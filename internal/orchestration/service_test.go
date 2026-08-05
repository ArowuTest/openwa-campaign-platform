package orchestration

import (
	"context"
	"errors"
	"fmt"
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
	}), BatchSize: 2, Clock: func() time.Time { return now }}

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
	if got := len(store.Recipients(ctx, entity.ID)); got != 5 {
		t.Fatalf("recipients=%d", got)
	}
	if got := len(store.Outbox(ctx)); got != 5 {
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
	entity := campaign.Campaign{ID: "campaign-1", OrganisationID: "org-1", PurposeID: "purpose-1", Status: campaign.StatusScheduled, MaximumUniqueRecipients: 1, AudienceSnapshotID: snapshot.ID, AudienceSnapshotHash: "forged-expected-hash", EligibleAudienceCount: 1, MessageVersionID: "message-1", MessageContentHash: "hash", Version: 1}
	_ = campaigns.Create(ctx, entity)
	snapshots := segment.NewMemoryRepository()
	if err := snapshots.CreateWithMembers(ctx, snapshot, members); err != nil {
		t.Fatal(err)
	}
	service := ReleaseService{Campaigns: campaign.NewService(campaigns), Snapshots: segment.NewService(snapshots), Store: NewMemoryStore(), Eligibility: EligibilityFunc(func(context.Context, string, string, string, string, time.Time) (EligibilityDecision, error) {
		return EligibilityDecision{Eligible: true}, nil
	})}
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
