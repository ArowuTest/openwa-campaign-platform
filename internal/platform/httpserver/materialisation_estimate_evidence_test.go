package httpserver

import (
	"testing"
	"time"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/campaign"
	"campaign-platform/internal/jobs"
)

func materialisationEstimateFixture() (campaign.Campaign, cohort.EstimateJobRecord, audiencefilter.Group) {
	asOf := time.Date(2099, 4, 1, 12, 0, 0, 0, time.UTC)
	definition := audiencefilter.Group{
		Join:  audiencefilter.JoinAnd,
		Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"NG"}}},
	}
	entity := campaign.Campaign{
		ID:                      "44444444-4444-4444-8444-444444444444",
		OrganisationID:          "11111111-1111-4111-8111-111111111111",
		PurposeID:               "22222222-2222-4222-8222-222222222222",
		ConsentReviewID:         "33333333-3333-4333-8333-333333333333",
		MaximumUniqueRecipients: 100,
	}
	record := cohort.EstimateJobRecord{
		ID:             "55555555-5555-4555-8555-555555555555",
		OrganisationID: entity.OrganisationID,
		PurposeID:      entity.PurposeID,
		Channel:        "WHATSAPP",
		Definition:     definition,
		AsOf:           asOf,
		Job:            jobs.Job{Status: jobs.StatusCompleted},
		Result: &cohort.Estimate{
			EligibleCount: 42,
			Breakdown:     &cohort.EligibilityBreakdown{Eligible: 42},
			CalculatedAt:  asOf.Add(time.Minute),
		},
		Evidence: cohort.EstimateGovernanceEvidence{
			ConsentReviewID:           entity.ConsentReviewID,
			ConsentReviewVersion:      4,
			ConsentWordingVersion:     "wording-v7",
			OrganisationPolicyID:      "66666666-6666-4666-8666-666666666666",
			OrganisationPolicyVersion: 6,
		},
	}
	return entity, record, definition
}

func TestResolveMaterialisationEstimateEvidenceUsesCompletedDurableEstimate(t *testing.T) {
	entity, record, definition := materialisationEstimateFixture()
	eligibility, consentVersion, configurationVersion, count, err := resolveMaterialisationEstimateEvidence(record, entity, definition)
	if err != nil {
		t.Fatal(err)
	}
	if eligibility.OrganisationID != entity.OrganisationID || eligibility.PurposeID != entity.PurposeID || eligibility.Channel != "WHATSAPP" || !eligibility.AsOf.Equal(record.AsOf) {
		t.Fatalf("eligibility=%+v", eligibility)
	}
	if consentVersion != "consent-review/33333333-3333-4333-8333-333333333333/v4/wording/wording-v7" {
		t.Fatalf("consentVersion=%q", consentVersion)
	}
	if configurationVersion != "organisation-policy/66666666-6666-4666-8666-666666666666/v6" {
		t.Fatalf("configurationVersion=%q", configurationVersion)
	}
	if count != 42 {
		t.Fatalf("count=%d", count)
	}
}

func TestResolveMaterialisationEstimateEvidenceFailsClosedForStaleOrMismatchedEvidence(t *testing.T) {
	entity, record, definition := materialisationEstimateFixture()

	pending := record
	pending.Job.Status = jobs.StatusPending
	if _, _, _, _, err := resolveMaterialisationEstimateEvidence(pending, entity, definition); err == nil {
		t.Fatal("pending durable estimate was accepted")
	}

	wrongDefinition := definition
	wrongDefinition.Rules = []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"GH"}}}
	if _, _, _, _, err := resolveMaterialisationEstimateEvidence(record, entity, wrongDefinition); err == nil {
		t.Fatal("mismatched estimate definition was accepted")
	}

	wrongReview := record
	wrongReview.Evidence.ConsentReviewID = "77777777-7777-4777-8777-777777777777"
	if _, _, _, _, err := resolveMaterialisationEstimateEvidence(wrongReview, entity, definition); err == nil {
		t.Fatal("estimate from another consent review was accepted")
	}

	overEntitlement := record
	overEntitlement.Result = &cohort.Estimate{EligibleCount: 101, Breakdown: &cohort.EligibilityBreakdown{Eligible: 101}, CalculatedAt: record.AsOf.Add(time.Minute)}
	if _, _, _, _, err := resolveMaterialisationEstimateEvidence(overEntitlement, entity, definition); err == nil {
		t.Fatal("estimate above campaign entitlement was accepted")
	}
}
