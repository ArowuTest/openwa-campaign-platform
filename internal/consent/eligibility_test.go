package consent

import (
	"testing"
	"time"
)

func TestEligibilitySuppressionOverridesLatestGrant(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	result := EvaluateEligibility(EligibilityInput{ContactActive: true, OrganisationID: "org", PurposeID: "music", Channel: "WHATSAPP", AsOf: now,
		Grants:       []Grant{{ID: "g1", OrganisationID: "org", PurposeID: "music", Channel: "WHATSAPP", Status: GrantActive, EffectiveFrom: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour)}},
		Suppressions: []Suppression{{ID: "s1", Scope: SuppressionGlobal, Active: true, EffectiveAt: now.Add(-time.Minute)}},
	})
	if result.Eligible || result.Reason != "SUPPRESSED" || result.SuppressionID != "s1" {
		t.Fatalf("result=%+v", result)
	}
}

func TestEligibilityLaterReconsentSupersedesEarlierWithdrawal(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	result := EvaluateEligibility(EligibilityInput{ContactActive: true, OrganisationID: "org", PurposeID: "music", Channel: "WHATSAPP", AsOf: now,
		Grants: []Grant{
			{ID: "withdrawn", OrganisationID: "org", PurposeID: "music", Channel: "WHATSAPP", Status: GrantWithdrawn, EffectiveFrom: now.Add(-48 * time.Hour), CreatedAt: now.Add(-48 * time.Hour)},
			{ID: "reconsent", OrganisationID: "org", PurposeID: "music", Channel: "WHATSAPP", Status: GrantActive, EffectiveFrom: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour)},
		},
	})
	if !result.Eligible || result.GrantID != "reconsent" {
		t.Fatalf("result=%+v", result)
	}
}

func TestEligibilityLatestWithdrawalBlocksEarlierActiveGrant(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	result := EvaluateEligibility(EligibilityInput{ContactActive: true, OrganisationID: "org", PurposeID: "music", Channel: "WHATSAPP", AsOf: now,
		Grants: []Grant{
			{ID: "active", OrganisationID: "org", PurposeID: "music", Channel: "WHATSAPP", Status: GrantActive, EffectiveFrom: now.Add(-48 * time.Hour), CreatedAt: now.Add(-48 * time.Hour)},
			{ID: "withdrawn", OrganisationID: "org", PurposeID: "music", Channel: "WHATSAPP", Status: GrantWithdrawn, EffectiveFrom: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour)},
		},
	})
	if result.Eligible || result.Reason != "CONSENT_WITHDRAWN" {
		t.Fatalf("result=%+v", result)
	}
}

func TestEligibilityExpiredAndFutureEvidence(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Minute)
	result := EvaluateEligibility(EligibilityInput{ContactActive: true, OrganisationID: "org", PurposeID: "music", Channel: "WHATSAPP", AsOf: now,
		Grants: []Grant{
			{ID: "future", OrganisationID: "org", PurposeID: "music", Channel: "WHATSAPP", Status: GrantActive, EffectiveFrom: now.Add(time.Hour), CreatedAt: now},
			{ID: "expired", OrganisationID: "org", PurposeID: "music", Channel: "WHATSAPP", Status: GrantActive, EffectiveFrom: now.Add(-time.Hour), ExpiresAt: &expired, CreatedAt: now.Add(-time.Hour)},
		},
	})
	if result.Eligible || result.Reason != "CONSENT_EXPIRED" {
		t.Fatalf("result=%+v", result)
	}
}
