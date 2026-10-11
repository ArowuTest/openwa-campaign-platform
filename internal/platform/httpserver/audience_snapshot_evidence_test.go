package httpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/organisation"
)

func snapshotEvidenceFixture(t *testing.T, now time.Time) (*Server, campaign.Campaign) {
	t.Helper()
	reviewRepo := consent.NewMemoryRepository()
	expiry := now.Add(24 * time.Hour)
	review := consent.Review{
		ID:             "22222222-2222-4222-8222-222222222222",
		OrganisationID: "11111111-1111-4111-8111-111111111111",
		Scope:          consent.ReviewScopeOrganisation,
		Name:           "Approved campaign consent",
		Channel:        "WHATSAPP",
		WordingVersion: "wording-v7",
		Status:         consent.StatusApproved,
		Outcome:        consent.OutcomeApproved,
		Version:        4,
		ExpiresAt:      &expiry,
		CreatedAt:      now.Add(-48 * time.Hour),
		UpdatedAt:      now.Add(-time.Hour),
	}
	if err := reviewRepo.Create(context.Background(), review); err != nil {
		t.Fatal(err)
	}
	policyStore := organisation.NewMemoryPolicyStore()
	policy := organisation.Policy{
		ID:                    "33333333-3333-4333-8333-333333333333",
		OrganisationID:        review.OrganisationID,
		Status:                organisation.PolicyActive,
		Version:               6,
		EffectiveFrom:         now.Add(-24 * time.Hour),
		ContactRetentionDays:  365,
		CampaignRetentionDays: 365,
		CreatedAt:             now.Add(-48 * time.Hour),
		UpdatedAt:             now.Add(-2 * time.Hour),
	}
	if _, err := policyStore.Create(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	server := &Server{deps: Dependencies{
		ConsentReviews:       consent.NewService(reviewRepo),
		OrganisationPolicies: &organisation.PolicyAdministration{Store: policyStore, Clock: func() time.Time { return now }},
	}}
	entity := campaign.Campaign{
		ID:              "44444444-4444-4444-8444-444444444444",
		OrganisationID:  review.OrganisationID,
		ConsentReviewID: review.ID,
		Transport:       campaign.TransportSelection{Channel: "WHATSAPP"},
	}
	return server, entity
}

func TestResolveAudienceSnapshotEvidenceUsesAuthoritativeGovernanceRecords(t *testing.T) {
	now := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	server, entity := snapshotEvidenceFixture(t, now)
	consentVersion, configVersion, err := server.resolveAudienceSnapshotEvidence(context.Background(), entity, now)
	if err != nil {
		t.Fatal(err)
	}
	if consentVersion != "consent-review/22222222-2222-4222-8222-222222222222/v4/wording/wording-v7" {
		t.Fatalf("consent version=%q", consentVersion)
	}
	if configVersion != "organisation-policy/33333333-3333-4333-8333-333333333333/v6" {
		t.Fatalf("configuration version=%q", configVersion)
	}
}

func TestResolveAudienceSnapshotEvidenceFailsClosedForMissingOrInvalidGovernance(t *testing.T) {
	now := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	server, entity := snapshotEvidenceFixture(t, now)

	noReview := *server
	noReview.deps.ConsentReviews = nil
	if _, _, err := noReview.resolveAudienceSnapshotEvidence(context.Background(), entity, now); err == nil {
		t.Fatal("missing consent review service was accepted")
	}

	noPolicy := *server
	noPolicy.deps.OrganisationPolicies = nil
	if _, _, err := noPolicy.resolveAudienceSnapshotEvidence(context.Background(), entity, now); err == nil {
		t.Fatal("missing organisation policy service was accepted")
	}

	wrongOrg := entity
	wrongOrg.OrganisationID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, _, err := server.resolveAudienceSnapshotEvidence(context.Background(), wrongOrg, now); err == nil || !strings.Contains(strings.ToLower(err.Error()), "organisation") {
		t.Fatalf("wrong organisation err=%v", err)
	}

	expiredServer, expiredEntity := snapshotEvidenceFixture(t, now)
	expiredReview, err := expiredServer.deps.ConsentReviews.Get(context.Background(), expiredEntity.ConsentReviewID)
	if err != nil {
		t.Fatal(err)
	}
	expiredReview.ExpiresAt = func() *time.Time { v := now.Add(-time.Minute); return &v }()
	repo := consent.NewMemoryRepository()
	if err := repo.Create(context.Background(), expiredReview); err != nil {
		t.Fatal(err)
	}
	expiredServer.deps.ConsentReviews = consent.NewService(repo)
	if _, _, err := expiredServer.resolveAudienceSnapshotEvidence(context.Background(), expiredEntity, now); err == nil || !strings.Contains(strings.ToLower(err.Error()), "expired") {
		t.Fatalf("expired review err=%v", err)
	}
}
