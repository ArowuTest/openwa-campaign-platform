package orchestration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/campaign"
	"campaign-platform/internal/segment"
)

func TestReleaseServiceBatchesAndReplaysIdempotently(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)
	members := make([]segment.Member, 5)
	for i := range members {
		members[i] = segment.Member{ContactID: fmt.Sprintf("contact-%02d", i+1), EligibilityEvidenceHash: fmt.Sprintf("evidence-%02d", i+1)}
	}
	definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorEquals, Values: []any{"NG"}}}}
	builder, err := segment.NewBuilder("campaign-1", "", definition, 1, "consent-v1", "config-v1", "operator-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		if err := builder.Add(member.ContactID, member.EligibilityEvidenceHash); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := builder.Finalise(now)
	if err != nil {
		t.Fatal(err)
	}
	campaigns := campaign.NewMemoryRepository()
	entity := campaign.Campaign{
		ID: "campaign-1", OrganisationID: "org-1", PurposeID: "purpose-1", Status: campaign.StatusScheduled,
		MaximumUniqueRecipients: 5, AudienceSnapshotID: snapshot.ID, AudienceSnapshotHash: snapshot.SnapshotHash,
		EligibleAudienceCount: 5, MessageVersionID: "message-1", MessageContentHash: "message-hash", Version: 1,
		CommercialApprovalID: "commercial-current",
	}
	if err := campaigns.Create(ctx, entity); err != nil {
		t.Fatal(err)
	}
	snapshots := segment.NewMemoryRepository()
	if err := snapshots.CreateWithMembers(ctx, snapshot, members); err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	service := ReleaseService{Campaigns: campaign.NewService(campaigns), Snapshots: segment.NewService(snapshots), Store: store, Eligibility: EligibilityFunc(func(context.Context, string, string, string, string, time.Time) (EligibilityDecision, error) {
		return EligibilityDecision{Eligible: true}, nil
	}), BatchSize: 2, CommercialApprovals: commercialApprovalStub{id: "commercial-current"}, Clock: func() time.Time { return now }}

	first, err := service.Release(ctx, entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Authorised != 5 || first.OutboxCreated != 5 || first.BatchesProcessed != 3 || first.Existing != 0 {
		t.Fatalf("unexpected first release: %+v", first)
	}
	second, err := service.Release(ctx, entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Authorised != 0 || second.OutboxCreated != 0 || second.Existing != 5 {
		t.Fatalf("unexpected replay: %+v", second)
	}
	if got := len(mustRecipients(t, store, entity.ID)); got != 5 {
		t.Fatalf("recipients=%d", got)
	}
	if got := len(mustOutbox(t, store)); got != 5 {
		t.Fatalf("outbox=%d", got)
	}
}

func TestReleaseServiceRejectsSnapshotEvidenceMismatch(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)
	definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorEquals, Values: []any{"NG"}}}}
	builder, err := segment.NewBuilder("campaign-1", "", definition, 1, "consent-v1", "config-v1", "operator")
	if err != nil {
		t.Fatal(err)
	}
	members := []segment.Member{{ContactID: "contact-1", EligibilityEvidenceHash: "evidence"}}
	if err := builder.Add(members[0].ContactID, members[0].EligibilityEvidenceHash); err != nil {
		t.Fatal(err)
	}
	snapshot, err := builder.Finalise(now)
	if err != nil {
		t.Fatal(err)
	}
	campaigns := campaign.NewMemoryRepository()
	entity := campaign.Campaign{ID: "campaign-1", OrganisationID: "org-1", PurposeID: "purpose-1", Status: campaign.StatusScheduled, MaximumUniqueRecipients: 1, AudienceSnapshotID: snapshot.ID, AudienceSnapshotHash: "forged-expected-hash", EligibleAudienceCount: 1, MessageVersionID: "message-1", MessageContentHash: "hash", Version: 1, CommercialApprovalID: "commercial-current"}
	_ = campaigns.Create(ctx, entity)
	snapshots := segment.NewMemoryRepository()
	if err := snapshots.CreateWithMembers(ctx, snapshot, members); err != nil {
		t.Fatal(err)
	}
	service := ReleaseService{Campaigns: campaign.NewService(campaigns), Snapshots: segment.NewService(snapshots), Store: NewMemoryStore(), Eligibility: EligibilityFunc(func(context.Context, string, string, string, string, time.Time) (EligibilityDecision, error) {
		return EligibilityDecision{Eligible: true}, nil
	}), CommercialApprovals: commercialApprovalStub{id: "commercial-current"}}
	_, err = service.Release(ctx, entity.ID)
	if !errors.Is(err, ErrReleaseConflict) {
		t.Fatalf("expected release conflict, got %v", err)
	}
}

func TestMemoryStoreEnforcesEntitlementAcrossBatches(t *testing.T) {
	store := NewMemoryStore()
	checker := EligibilityFunc(func(context.Context, string, string, string, string, time.Time) (EligibilityDecision, error) {
		return EligibilityDecision{Eligible: true}, nil
	})
	base := Command{CampaignID: "campaign-1", SnapshotID: "snapshot-1", MessageVersionID: "message-1", OrganisationID: "org-1", PurposeID: "purpose-1", Channel: "WHATSAPP", MaximumUniqueRecipients: 2, AsOf: time.Now().UTC()}
	first := base
	first.Members = []Member{{ContactID: "contact-1", EligibilityEvidenceHash: "e1"}, {ContactID: "contact-2", EligibilityEvidenceHash: "e2"}}
	if _, err := store.Authorise(context.Background(), first, checker); err != nil {
		t.Fatal(err)
	}
	second := base
	second.Members = []Member{{ContactID: "contact-3", EligibilityEvidenceHash: "e3"}}
	if _, err := store.Authorise(context.Background(), second, checker); !errors.Is(err, ErrEntitlementExceeded) {
		t.Fatalf("expected entitlement error, got %v", err)
	}
}

func TestReleaseCompatibilityWrapperUsesGuardedValidation(t *testing.T) {
	ctx := context.Background()
	campaigns := campaign.NewMemoryRepository()
	entity := campaign.Campaign{
		ID: "campaign-unguarded", OrganisationID: "org-1", Status: campaign.StatusScheduled,
		Version: 1, Transport: campaign.TransportSelection{Provider: campaign.ProviderOpenWA},
	}
	if err := campaigns.Create(ctx, entity); err != nil {
		t.Fatal(err)
	}
	service := ReleaseService{
		Campaigns: campaign.NewService(campaigns),
		Snapshots: segment.NewService(segment.NewMemoryRepository()),
		Store:     NewMemoryStore(),
		Eligibility: EligibilityFunc(func(context.Context, string, string, string, string, time.Time) (EligibilityDecision, error) {
			return EligibilityDecision{Eligible: true}, nil
		}),
	}
	_, err := service.Release(ctx, entity.ID)
	if err == nil || !strings.Contains(err.Error(), "day-one OpenWA pilot admission is required") {
		t.Fatalf("unguarded release bypassed guarded admission: %v", err)
	}
}

func TestReleaseServiceGuardedRequiresExpectedVersionAndPilotAdmission(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorEquals, Values: []any{"NG"}}}}
	builder, err := segment.NewBuilder("campaign-guarded", "", definition, 1, "consent-v1", "config-v1", "operator")
	if err != nil {
		t.Fatal(err)
	}
	member := segment.Member{ContactID: "contact-1", EligibilityEvidenceHash: "evidence"}
	if err := builder.Add(member.ContactID, member.EligibilityEvidenceHash); err != nil {
		t.Fatal(err)
	}
	snapshot, err := builder.Finalise(now)
	if err != nil {
		t.Fatal(err)
	}
	campaigns := campaign.NewMemoryRepository()
	entity := campaign.Campaign{
		ID: "campaign-guarded", OrganisationID: "org-1", PurposeID: "purpose-1",
		Status: campaign.StatusScheduled, MaximumUniqueRecipients: 1,
		AudienceSnapshotID: snapshot.ID, AudienceSnapshotHash: snapshot.SnapshotHash,
		EligibleAudienceCount: 1, MessageVersionID: "message-1", MessageContentHash: "hash", Version: 7,
		CommercialApprovalID: "commercial-current",
		Transport:            campaign.TransportSelection{Provider: campaign.ProviderOpenWA},
	}
	if err := campaigns.Create(ctx, entity); err != nil {
		t.Fatal(err)
	}
	snapshots := segment.NewMemoryRepository()
	if err := snapshots.CreateWithMembers(ctx, snapshot, []segment.Member{member}); err != nil {
		t.Fatal(err)
	}
	gateCalls := 0
	service := ReleaseService{
		Campaigns: campaign.NewService(campaigns), Snapshots: segment.NewService(snapshots),
		Store: NewMemoryStore(), Eligibility: EligibilityFunc(func(context.Context, string, string, string, string, time.Time) (EligibilityDecision, error) {
			return EligibilityDecision{Eligible: true}, nil
		}),
		PilotAdmission: func(_ context.Context, got campaign.Campaign) error {
			gateCalls++
			if got.ID != entity.ID {
				t.Fatalf("gate campaign=%s", got.ID)
			}
			return nil
		},
		CommercialApprovals: commercialApprovalStub{id: "commercial-current"},
		Clock:               func() time.Time { return now },
	}

	if _, err := service.ReleaseGuarded(ctx, entity.ID, entity.Version-1); !errors.Is(err, ErrReleaseConflict) {
		t.Fatalf("stale version should fail closed, got %v", err)
	}
	if gateCalls != 0 {
		t.Fatalf("pilot gate called for stale version: %d", gateCalls)
	}
	if _, err := service.ReleaseGuarded(ctx, entity.ID, entity.Version); err != nil {
		t.Fatalf("guarded release failed: %v", err)
	}
	if gateCalls != 1 {
		t.Fatalf("pilot gate calls=%d", gateCalls)
	}
}

type commercialApprovalStub struct {
	id  string
	err error
}

func (s commercialApprovalStub) ValidateCampaignApproval(context.Context, string, string, int64) (string, error) {
	return s.id, s.err
}

func TestGuardedReleaseRejectsCommercialAuthorityThatWasRevokedAfterFinalApproval(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 11, 13, 30, 0, 0, time.UTC)
	definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{
		DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorEquals, Values: []any{"NG"},
	}}}
	builder, err := segment.NewBuilder("campaign-commercial-revoked", "", definition, 1, "consent-v1", "config-v1", "operator")
	if err != nil {
		t.Fatal(err)
	}
	member := segment.Member{ContactID: "contact-1", EligibilityEvidenceHash: "evidence-1"}
	if err := builder.Add(member.ContactID, member.EligibilityEvidenceHash); err != nil {
		t.Fatal(err)
	}
	snapshot, err := builder.Finalise(now)
	if err != nil {
		t.Fatal(err)
	}
	campaigns := campaign.NewMemoryRepository()
	entity := campaign.Campaign{
		ID: "campaign-commercial-revoked", OrganisationID: "org-1", PurposeID: "purpose-1",
		Status: campaign.StatusScheduled, Version: 11, MaximumUniqueRecipients: 1,
		AudienceSnapshotID: snapshot.ID, AudienceSnapshotHash: snapshot.SnapshotHash,
		EligibleAudienceCount: 1, MessageVersionID: "message-1", MessageContentHash: "message-hash",
		CommercialApprovalID: "commercial-1",
		Transport:            campaign.TransportSelection{Provider: campaign.ProviderOpenWA},
	}
	if err := campaigns.Create(ctx, entity); err != nil {
		t.Fatal(err)
	}
	snapshots := segment.NewMemoryRepository()
	if err := snapshots.CreateWithMembers(ctx, snapshot, []segment.Member{member}); err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	service := ReleaseService{
		Campaigns: campaign.NewService(campaigns), Snapshots: segment.NewService(snapshots),
		Store: store, Eligibility: EligibilityFunc(func(context.Context, string, string, string, string, time.Time) (EligibilityDecision, error) {
			return EligibilityDecision{Eligible: true}, nil
		}),
		PilotAdmission:      func(context.Context, campaign.Campaign) error { return nil },
		CommercialApprovals: commercialApprovalStub{err: errors.New("commercial approval revoked")},
		Clock:               func() time.Time { return now },
	}
	if _, err := service.ReleaseGuarded(ctx, entity.ID, entity.Version); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("guarded release accepted revoked commercial authority: %v", err)
	}
	if got := len(mustRecipients(t, store, entity.ID)); got != 0 {
		t.Fatalf("revoked commercial authority created %d recipient obligations", got)
	}

	service.CommercialApprovals = commercialApprovalStub{id: "different-commercial"}
	if _, err := service.ReleaseGuarded(ctx, entity.ID, entity.Version); err == nil || !strings.Contains(err.Error(), "no longer current") {
		t.Fatalf("guarded release accepted different commercial authority: %v", err)
	}
}
