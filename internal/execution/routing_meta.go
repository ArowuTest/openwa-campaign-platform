package execution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/metacloud"
)

func (s *RoutingAdministration) metaHealthWindow() time.Duration {
	if s != nil && s.MetaHealthStaleAfter > 0 {
		return s.MetaHealthStaleAfter
	}
	return 5 * time.Minute
}

func (s *RoutingAdministration) requireMetaSender(ctx context.Context, route PoolRoute, entity campaign.Campaign, windowStart, windowEnd, healthAt time.Time) (metacloud.Sender, error) {
	if s == nil || s.MetaSenders == nil || s.MetaTemplates == nil {
		return metacloud.Sender{}, errors.New("Meta sender and template governance are required")
	}
	value, err := s.MetaSenders.Get(ctx, route.MetaSenderID)
	if err != nil {
		return metacloud.Sender{}, err
	}
	if value.Status != metacloud.StatusActive || value.OrganisationID != entity.OrganisationID || value.SenderPoolID != route.SenderPoolID {
		return metacloud.Sender{}, errors.New("Meta sender is not active for the campaign organisation and pool")
	}
	if value.EffectiveFrom == nil || value.EffectiveFrom.After(windowStart) || (value.EffectiveTo != nil && !value.EffectiveTo.After(windowEnd)) {
		return metacloud.Sender{}, errors.New("Meta sender does not cover the campaign execution window")
	}
	if value.Health != metacloud.HealthHealthy && value.Health != metacloud.HealthDegraded {
		return metacloud.Sender{}, errors.New("Meta sender is unavailable")
	}
	if value.HealthObservedAt == nil || value.HealthObservedAt.Before(healthAt.Add(-s.metaHealthWindow())) || value.HealthObservedAt.After(healthAt.Add(time.Minute)) {
		return metacloud.Sender{}, errors.New("Meta sender health evidence is stale or invalid")
	}
	if entity.MessageVersionID == "" {
		return metacloud.Sender{}, errors.New("Meta routing requires an approved message version binding")
	}
	binding, err := s.MetaTemplates.GetBinding(ctx, entity.MessageVersionID)
	if err != nil {
		return metacloud.Sender{}, fmt.Errorf("Meta message binding: %w", err)
	}
	template, err := s.MetaTemplates.FindApproved(ctx, entity.OrganisationID, value.WABAID, binding.TemplateName, binding.Language)
	if err != nil {
		return metacloud.Sender{}, fmt.Errorf("Meta approved template: %w", err)
	}
	if template.ComponentHash != binding.TemplateComponentHash {
		return metacloud.Sender{}, errors.New("Meta template components no longer match approved message binding")
	}
	return value, nil
}

func (s *RoutingAdministration) freezeMetaRoute(ctx context.Context, route *PoolRoute, entity campaign.Campaign, start, end time.Time) error {
	value, err := s.requireMetaSender(ctx, *route, entity, start, end, s.now())
	if err != nil {
		return err
	}
	route.MetaSenderVersion = value.Version
	route.GatewayPoolVersion = 0
	return nil
}

func (s *RoutingAdministration) validateMetaRoute(ctx context.Context, route PoolRoute, entity campaign.Campaign, start, end, at time.Time) error {
	if route.MetaSenderVersion <= 0 || route.GatewayPoolVersion != 0 {
		return errors.New("Meta route lacks frozen sender evidence")
	}
	value, err := s.requireMetaSender(ctx, route, entity, start, end, at)
	if err != nil {
		return err
	}
	if value.Version < route.MetaSenderVersion {
		return errors.New("Meta sender version regressed below frozen evidence")
	}
	return nil
}
