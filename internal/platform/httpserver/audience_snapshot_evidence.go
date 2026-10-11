package httpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/organisation"
)

func (s *Server) resolveAudienceSnapshotEvidence(ctx context.Context, entity campaign.Campaign, asOf time.Time) (string, string, error) {
	if s == nil || s.deps.ConsentReviews == nil {
		return "", "", errors.New("consent review service is required for audience snapshot evidence")
	}
	if s.deps.OrganisationPolicies == nil || s.deps.OrganisationPolicies.Store == nil {
		return "", "", errors.New("organisation policy service is required for audience snapshot evidence")
	}
	asOf = asOf.UTC()
	channel := strings.ToUpper(strings.TrimSpace(entity.Transport.Channel))
	if channel == "" {
		channel = "WHATSAPP"
	}
	if err := s.deps.ConsentReviews.ValidateCampaignReview(
		ctx,
		entity.ConsentReviewID,
		entity.ID,
		entity.OrganisationID,
		channel,
		asOf,
	); err != nil {
		return "", "", fmt.Errorf("campaign consent review is not authoritative for snapshot materialisation: %w", err)
	}
	review, err := s.deps.ConsentReviews.Get(ctx, entity.ConsentReviewID)
	if err != nil {
		return "", "", fmt.Errorf("load campaign consent review: %w", err)
	}
	if review.Status != consent.StatusApproved || review.OrganisationID != entity.OrganisationID {
		return "", "", errors.New("campaign consent review is not approved for the campaign organisation")
	}
	if strings.TrimSpace(review.WordingVersion) == "" || review.Version <= 0 {
		return "", "", errors.New("campaign consent review lacks version evidence")
	}

	policy, err := s.deps.OrganisationPolicies.Store.Active(ctx, entity.OrganisationID, asOf)
	if err != nil {
		if errors.Is(err, organisation.ErrPolicyNotFound) {
			return "", "", errors.New("active organisation policy is required for audience snapshot evidence")
		}
		return "", "", fmt.Errorf("load active organisation policy: %w", err)
	}
	if policy.Status != organisation.PolicyActive || policy.Version <= 0 {
		return "", "", errors.New("active organisation policy lacks version evidence")
	}

	consentVersion := fmt.Sprintf(
		"consent-review/%s/v%d/wording/%s",
		review.ID,
		review.Version,
		strings.TrimSpace(review.WordingVersion),
	)
	configurationVersion := fmt.Sprintf(
		"organisation-policy/%s/v%d",
		policy.ID,
		policy.Version,
	)
	return consentVersion, configurationVersion, nil
}

func (s *Server) revalidateAudienceSnapshotEvidence(ctx context.Context, entity campaign.Campaign, asOf time.Time, consentVersion, configurationVersion string) error {
	currentConsentVersion, currentConfigurationVersion, err := s.resolveAudienceSnapshotEvidence(ctx, entity, asOf)
	if err != nil {
		return err
	}
	if currentConsentVersion != consentVersion || currentConfigurationVersion != configurationVersion {
		return errors.New("authoritative consent or configuration evidence changed during member selection")
	}
	return nil
}
