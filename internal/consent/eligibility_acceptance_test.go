package consent

import (
	"testing"
	"time"
)

func TestEligibilityRequiresExactOrganisationPurposeAndChannelScope(t *testing.T) {
	now := time.Now().UTC()
	grant := Grant{ID: "g1", OrganisationID: "org-a", PurposeID: "promo", Channel: "WHATSAPP", Status: GrantActive, EffectiveFrom: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour)}
	tests := []struct {
		name, org, purpose, channel string
		eligible                    bool
	}{
		{"exact", "org-a", "promo", "WHATSAPP", true},
		{"wrong organisation", "org-b", "promo", "WHATSAPP", false},
		{"wrong purpose", "org-a", "service", "WHATSAPP", false},
		{"wrong channel", "org-a", "promo", "SMS", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateEligibility(EligibilityInput{ContactActive: true, OrganisationID: tc.org, PurposeID: tc.purpose, Channel: tc.channel, AsOf: now, Grants: []Grant{grant}})
			if got.Eligible != tc.eligible {
				t.Fatalf("result=%+v", got)
			}
			if !tc.eligible && got.Reason != "NO_ACTIVE_CONSENT" {
				t.Fatalf("reason=%s", got.Reason)
			}
		})
	}
}

func TestEligibilityAppliesEachSuppressionScopePrecisely(t *testing.T) {
	now := time.Now().UTC()
	grant := Grant{ID: "g1", OrganisationID: "org-a", PurposeID: "promo", Channel: "WHATSAPP", Status: GrantActive, EffectiveFrom: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour)}
	tests := []Suppression{
		{ID: "global", Scope: SuppressionGlobal, Active: true, EffectiveAt: now.Add(-time.Minute)},
		{ID: "org", Scope: SuppressionOrganisation, OrganisationID: "org-a", Active: true, EffectiveAt: now.Add(-time.Minute)},
		{ID: "purpose", Scope: SuppressionPurpose, PurposeID: "promo", Active: true, EffectiveAt: now.Add(-time.Minute)},
		{ID: "channel", Scope: SuppressionChannel, Channel: "WHATSAPP", Active: true, EffectiveAt: now.Add(-time.Minute)},
	}
	for _, suppression := range tests {
		t.Run(string(suppression.Scope), func(t *testing.T) {
			got := EvaluateEligibility(EligibilityInput{ContactActive: true, OrganisationID: "org-a", PurposeID: "promo", Channel: "WHATSAPP", AsOf: now, Grants: []Grant{grant}, Suppressions: []Suppression{suppression}})
			if got.Eligible || got.Reason != "SUPPRESSED" || got.SuppressionID != suppression.ID {
				t.Fatalf("result=%+v", got)
			}
		})
	}
	mismatched := Suppression{ID: "other-org", Scope: SuppressionOrganisation, OrganisationID: "org-b", Active: true, EffectiveAt: now.Add(-time.Minute)}
	got := EvaluateEligibility(EligibilityInput{ContactActive: true, OrganisationID: "org-a", PurposeID: "promo", Channel: "WHATSAPP", AsOf: now, Grants: []Grant{grant}, Suppressions: []Suppression{mismatched}})
	if !got.Eligible || got.GrantID != "g1" {
		t.Fatalf("mismatched suppression result=%+v", got)
	}
}
