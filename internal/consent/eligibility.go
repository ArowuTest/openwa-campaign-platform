package consent

import (
	"sort"
	"strings"
	"time"
)

// EligibilityInput is a deterministic representation of the evidence used by
// cohort estimation, release and final dispatch checks. Suppression always wins;
// otherwise the latest effective grant governs, allowing a later valid re-consent
// to supersede an earlier withdrawal without erasing the withdrawal evidence.
type EligibilityInput struct {
	ContactActive  bool
	OrganisationID string
	PurposeID      string
	Channel        string
	AsOf           time.Time
	Grants         []Grant
	Suppressions   []Suppression
}

type EligibilityResult struct {
	Eligible      bool
	Reason        string
	GrantID       string
	SuppressionID string
}

func EvaluateEligibility(in EligibilityInput) EligibilityResult {
	asOf := in.AsOf.UTC()
	if !in.ContactActive {
		return EligibilityResult{Reason: "CONTACT_INACTIVE"}
	}
	for _, suppression := range in.Suppressions {
		if suppressionApplies(suppression, in.OrganisationID, in.PurposeID, in.Channel, asOf) {
			return EligibilityResult{Reason: "SUPPRESSED", SuppressionID: suppression.ID}
		}
	}
	grants := append([]Grant(nil), in.Grants...)
	sort.SliceStable(grants, func(i, j int) bool {
		if grants[i].EffectiveFrom.Equal(grants[j].EffectiveFrom) {
			if grants[i].CreatedAt.Equal(grants[j].CreatedAt) {
				return grants[i].ID > grants[j].ID
			}
			return grants[i].CreatedAt.After(grants[j].CreatedAt)
		}
		return grants[i].EffectiveFrom.After(grants[j].EffectiveFrom)
	})
	for _, grant := range grants {
		if grant.OrganisationID != strings.TrimSpace(in.OrganisationID) || grant.PurposeID != strings.TrimSpace(in.PurposeID) || !strings.EqualFold(grant.Channel, in.Channel) || grant.EffectiveFrom.After(asOf) {
			continue
		}
		result := EligibilityResult{GrantID: grant.ID}
		switch grant.Status {
		case GrantWithdrawn:
			result.Reason = "CONSENT_WITHDRAWN"
		case GrantRevoked:
			result.Reason = "CONSENT_REVOKED"
		case GrantExpired:
			result.Reason = "CONSENT_EXPIRED"
		case GrantActive:
			if grant.ExpiresAt != nil && !grant.ExpiresAt.After(asOf) {
				result.Reason = "CONSENT_EXPIRED"
			} else {
				result.Eligible = true
			}
		default:
			result.Reason = "NO_ACTIVE_CONSENT"
		}
		return result
	}
	return EligibilityResult{Reason: "NO_ACTIVE_CONSENT"}
}

func suppressionApplies(s Suppression, organisationID, purposeID, channel string, asOf time.Time) bool {
	if !s.Active || s.EffectiveAt.After(asOf) || (s.ExpiresAt != nil && !s.ExpiresAt.After(asOf)) {
		return false
	}
	switch s.Scope {
	case SuppressionGlobal:
		return true
	case SuppressionOrganisation:
		return s.OrganisationID == strings.TrimSpace(organisationID)
	case SuppressionPurpose:
		return s.PurposeID == strings.TrimSpace(purposeID)
	case SuppressionChannel:
		return strings.EqualFold(s.Channel, channel)
	case SuppressionTemporary:
		return (s.OrganisationID == "" || s.OrganisationID == strings.TrimSpace(organisationID)) &&
			(s.PurposeID == "" || s.PurposeID == strings.TrimSpace(purposeID)) &&
			(s.Channel == "" || strings.EqualFold(s.Channel, channel))
	default:
		return false
	}
}
