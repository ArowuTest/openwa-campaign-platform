package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/campaign"
	"campaign-platform/internal/jobs"
)

func resolveMaterialisationEstimateEvidence(
	record cohort.EstimateJobRecord,
	entity campaign.Campaign,
	definition audiencefilter.Group,
) (cohort.EligibilityContext, string, string, int64, error) {
	if record.Job.Status != jobs.StatusCompleted || record.Result == nil {
		return cohort.EligibilityContext{}, "", "", 0, errors.New("a completed durable cohort estimate is required")
	}
	if record.OrganisationID != entity.OrganisationID || record.PurposeID != entity.PurposeID ||
		strings.ToUpper(strings.TrimSpace(record.Channel)) != "WHATSAPP" {
		return cohort.EligibilityContext{}, "", "", 0, errors.New("cohort estimate does not match the campaign organisation, purpose and channel")
	}
	if record.AsOf.IsZero() || record.Result.CalculatedAt.IsZero() || record.Result.Breakdown == nil {
		return cohort.EligibilityContext{}, "", "", 0, errors.New("cohort estimate result evidence is incomplete")
	}
	if record.Result.EligibleCount <= 0 {
		return cohort.EligibilityContext{}, "", "", 0, errors.New("cohort estimate contains no eligible recipients")
	}
	if record.Result.EligibleCount > entity.MaximumUniqueRecipients {
		return cohort.EligibilityContext{}, "", "", 0, fmt.Errorf(
			"cohort estimate exceeds campaign entitlement: eligible=%d maximum=%d",
			record.Result.EligibleCount,
			entity.MaximumUniqueRecipients,
		)
	}
	if record.Result.Breakdown.Eligible != record.Result.EligibleCount {
		return cohort.EligibilityContext{}, "", "", 0, errors.New("cohort estimate count does not reconcile with its eligibility breakdown")
	}
	if !sameFilterGroup(record.Definition, definition) {
		return cohort.EligibilityContext{}, "", "", 0, errors.New("cohort estimate definition does not match the requested materialisation definition")
	}
	evidence := record.Evidence
	if strings.TrimSpace(evidence.ConsentReviewID) == "" || evidence.ConsentReviewVersion <= 0 ||
		strings.TrimSpace(evidence.ConsentWordingVersion) == "" ||
		strings.TrimSpace(evidence.OrganisationPolicyID) == "" || evidence.OrganisationPolicyVersion <= 0 {
		return cohort.EligibilityContext{}, "", "", 0, errors.New("cohort estimate governance evidence is incomplete")
	}
	if strings.TrimSpace(entity.ConsentReviewID) == "" || evidence.ConsentReviewID != entity.ConsentReviewID {
		return cohort.EligibilityContext{}, "", "", 0, errors.New("cohort estimate consent review does not match the campaign consent review")
	}

	eligibility := cohort.EligibilityContext{
		OrganisationID: entity.OrganisationID,
		PurposeID:      entity.PurposeID,
		Channel:        "WHATSAPP",
		AsOf:           record.AsOf.UTC(),
	}
	consentVersion := fmt.Sprintf(
		"consent-review/%s/v%d/wording/%s",
		evidence.ConsentReviewID,
		evidence.ConsentReviewVersion,
		strings.TrimSpace(evidence.ConsentWordingVersion),
	)
	configurationVersion := fmt.Sprintf(
		"organisation-policy/%s/v%d",
		evidence.OrganisationPolicyID,
		evidence.OrganisationPolicyVersion,
	)
	return eligibility, consentVersion, configurationVersion, record.Result.EligibleCount, nil
}

func sameFilterGroup(left, right audiencefilter.Group) bool {
	leftJSON, leftErr := json.Marshal(normaliseFilterGroupWireArrays(left))
	rightJSON, rightErr := json.Marshal(normaliseFilterGroupWireArrays(right))
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

// Only absent/empty wire arrays are equivalent. Keep values, types and order
// intact, and copy the slices so comparison never changes persisted definitions.
func normaliseFilterGroupWireArrays(group audiencefilter.Group) audiencefilter.Group {
	normalised := group
	normalised.Rules = make([]audiencefilter.Rule, len(group.Rules))
	for index, rule := range group.Rules {
		normalised.Rules[index] = rule
		normalised.Rules[index].Values = append([]any{}, rule.Values...)
	}
	normalised.Children = make([]audiencefilter.Group, len(group.Children))
	for index, child := range group.Children {
		normalised.Children[index] = normaliseFilterGroupWireArrays(child)
	}
	return normalised
}
