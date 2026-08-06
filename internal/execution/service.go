package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/platformpolicy"
)

type CampaignService interface {
	Get(context.Context, string) (campaign.Campaign, error)
	Transition(context.Context, string, campaign.TransitionInput) (campaign.Campaign, error)
}
type RuntimeStore interface {
	RecordAdmission(context.Context, CapacityEvidence) error
	Metrics(context.Context, string) (Metrics, error)
	Capacity(context.Context, string, time.Time) (int, int64, error)
	RouteCapacity(context.Context, PoolRoute, time.Time) (PoolCapacity, error)
	RecordEvent(context.Context, string, string, string, string, map[string]any, time.Time) error
}
type Coordinator struct {
	Campaigns           CampaignService
	Store               RuntimeStore
	SafetyMarginPercent int
	RoutingPlans        *RoutingAdministration
	Maintenance         *platformpolicy.MaintenanceAdministration
	Clock               func() time.Time
}

func executionScope(entity campaign.Campaign) platformpolicy.OperationalScope {
	return platformpolicy.OperationalScope{
		Provider:      string(entity.Transport.Provider),
		GatewayPoolID: entity.Transport.GatewayPoolID,
		SenderPoolID:  entity.Transport.SenderPoolID,
	}
}

func (c *Coordinator) checkMaintenance(ctx context.Context, action platformpolicy.Operation, entity campaign.Campaign) error {
	if c == nil || c.Maintenance == nil {
		return nil
	}
	return c.Maintenance.Check(ctx, action, executionScope(entity), c.now())
}
func (c *Coordinator) now() time.Time {
	if c.Clock != nil {
		return c.Clock().UTC()
	}
	return time.Now().UTC()
}
func (c *Coordinator) assess(ctx context.Context, id string, record bool) (CapacityEvidence, Metrics, error) {
	if c == nil || c.Campaigns == nil || c.Store == nil {
		return CapacityEvidence{}, Metrics{}, errors.New("execution coordinator dependencies are required")
	}
	entity, err := c.Campaigns.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return CapacityEvidence{}, Metrics{}, err
	}
	if entity.Status != campaign.StatusScheduled && entity.Status != campaign.StatusDispatching && entity.Status != campaign.StatusPaused {
		return CapacityEvidence{}, Metrics{}, fmt.Errorf("campaign status %s cannot be assessed for execution", entity.Status)
	}
	if entity.CompletionDeadlineAt == nil {
		return CapacityEvidence{}, Metrics{}, errors.New("campaign completion deadline is required")
	}
	now := c.now()
	metrics, err := c.Store.Metrics(ctx, entity.ID)
	if err != nil {
		return CapacityEvidence{}, Metrics{}, fmt.Errorf("load campaign metrics: %w", err)
	}
	remaining := metrics.Authorised + metrics.Queued
	if remaining == 0 && entity.Status == campaign.StatusScheduled {
		remaining = entity.EligibleAudienceCount
	}
	start := now
	if entity.RequestedStartAt != nil && entity.RequestedStartAt.After(now) {
		start = *entity.RequestedStartAt
	}
	var evidence CapacityEvidence
	if c.RoutingPlans != nil {
		plan, listErr := c.RoutingPlans.LatestByCampaign(ctx, entity.ID)
		if listErr == nil {
			if err := c.RoutingPlans.ValidateForExecution(ctx, plan, entity, now); err != nil {
				return CapacityEvidence{}, Metrics{}, fmt.Errorf("validate campaign routing plan: %w", err)
			}
			capacities := make([]PoolCapacity, 0, len(plan.Routes))
			for _, route := range plan.Routes {
				capacity, capErr := c.Store.RouteCapacity(ctx, route, now)
				if capErr != nil {
					capacities = append(capacities, PoolCapacity{SenderPoolID: route.SenderPoolID, GatewayPoolID: route.GatewayPoolID})
					continue
				}
				capacities = append(capacities, capacity)
			}
			multi, multiErr := EvaluateMultiPoolAdmission(MultiPoolAdmissionInput{CampaignID: entity.ID, RemainingRecipients: remaining, EffectiveStart: start, Deadline: *entity.CompletionDeadlineAt, SafetyMarginPercent: c.SafetyMarginPercent, Plan: plan, Capacities: capacities, Now: now})
			if multiErr != nil {
				return CapacityEvidence{}, Metrics{}, multiErr
			}
			evidence = CapacityEvidence{CampaignID: entity.ID, PoolID: plan.ID, EvidenceVersion: plan.CapacityEvidenceVersion, RemainingRecipients: remaining, AvailableMessagesPerMinute: multi.EffectiveMessagesPerMinute, AvailableDailyCapacity: multi.EffectiveDailyUnits, SafetyMarginPercent: c.SafetyMarginPercent, RequiredMessagesPerMinute: multi.RequiredMessagesPerMinute, EffectiveMessagesPerMinute: float64(multi.EffectiveMessagesPerMinute), ForecastCompletionAt: multi.ForecastCompletionAt, DeadlineAt: *entity.CompletionDeadlineAt, Decision: multi.Decision, Reasons: multi.Reasons, EvaluatedAt: multi.EvaluatedAt}
		} else if errors.Is(listErr, ErrRoutingPlanNotFound) {
			evidence, err = c.singlePoolAdmission(ctx, entity, remaining, start, now)
		} else {
			return CapacityEvidence{}, Metrics{}, fmt.Errorf("load campaign routing plan: %w", listErr)
		}
	} else {
		evidence, err = c.singlePoolAdmission(ctx, entity, remaining, start, now)
	}
	if err != nil {
		return CapacityEvidence{}, Metrics{}, err
	}
	if record {
		if err = c.Store.RecordAdmission(ctx, evidence); err != nil {
			return CapacityEvidence{}, Metrics{}, fmt.Errorf("record capacity assessment: %w", err)
		}
	}
	return evidence, metrics, nil
}

func (c *Coordinator) singlePoolAdmission(ctx context.Context, entity campaign.Campaign, remaining int64, start, now time.Time) (CapacityEvidence, error) {
	poolID := entity.Transport.SenderPoolID
	if entity.Transport.RoutingMode == campaign.RoutingSpecificSession {
		poolID = entity.Transport.GatewayPoolID
	}
	if poolID == "" {
		return CapacityEvidence{}, errors.New("campaign transport lacks a capacity pool reference")
	}
	rate, daily, err := c.Store.Capacity(ctx, poolID, now)
	if err != nil {
		return CapacityEvidence{}, fmt.Errorf("load measured capacity: %w", err)
	}
	return EvaluateAdmission(AdmissionInput{CampaignID: entity.ID, PoolID: poolID, EvidenceVersion: entity.Transport.CapacityEvidenceVersion, RemainingRecipients: remaining, AvailableMessagesPerMinute: rate, AvailableDailyCapacity: daily, SafetyMarginPercent: c.SafetyMarginPercent, StartAt: start, DeadlineAt: *entity.CompletionDeadlineAt, Now: now})
}

func (c *Coordinator) Plan(ctx context.Context, id string) (CapacityEvidence, error) {
	evidence, _, err := c.assess(ctx, id, true)
	return evidence, err
}

func (c *Coordinator) Forecast(ctx context.Context, id string) (ExecutionForecast, error) {
	evidence, metrics, err := c.assess(ctx, id, false)
	if err != nil {
		return ExecutionForecast{}, err
	}
	return BuildExecutionForecast(evidence, metrics), nil
}
func (c *Coordinator) Start(ctx context.Context, id, actor, reason string, expected int64) (campaign.Campaign, CapacityEvidence, error) {
	entity, err := c.Campaigns.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return campaign.Campaign{}, CapacityEvidence{}, err
	}
	if err := c.checkMaintenance(ctx, platformpolicy.OperationCampaignStart, entity); err != nil {
		return campaign.Campaign{}, CapacityEvidence{}, err
	}
	window, err := entity.EvaluateDispatchWindow(c.now())
	if err != nil {
		return campaign.Campaign{}, CapacityEvidence{}, err
	}
	if !window.Allowed {
		return campaign.Campaign{}, CapacityEvidence{}, fmt.Errorf("campaign dispatch window blocked: %s", window.Reason)
	}
	ev, err := c.Plan(ctx, id)
	if err != nil {
		return campaign.Campaign{}, CapacityEvidence{}, err
	}
	if ev.Decision != DecisionAdmit {
		return campaign.Campaign{}, ev, fmt.Errorf("campaign admission decision is %s: %v", ev.Decision, ev.Reasons)
	}
	if c.RoutingPlans != nil {
		plan, planErr := c.RoutingPlans.LatestByCampaign(ctx, id)
		if planErr == nil {
			if err := c.RoutingPlans.Activate(ctx, plan.ID); err != nil {
				return campaign.Campaign{}, ev, fmt.Errorf("activate routing-plan reservations: %w", err)
			}
		} else if !errors.Is(planErr, ErrRoutingPlanNotFound) {
			return campaign.Campaign{}, ev, fmt.Errorf("load routing plan for activation: %w", planErr)
		}
	}
	entity, err = c.Campaigns.Transition(ctx, id, campaign.TransitionInput{Action: campaign.ActionStartDispatch, ActorID: actor, Reason: reason, ExpectedVersion: expected})
	if err != nil {
		return campaign.Campaign{}, ev, err
	}
	if err := c.Store.RecordEvent(ctx, id, "DISPATCH_STARTED", actor, reason, map[string]any{"capacityEvidenceVersion": ev.EvidenceVersion, "forecastCompletionAt": ev.ForecastCompletionAt}, c.now()); err != nil {
		return entity, ev, fmt.Errorf("campaign dispatch started but execution event could not be recorded: %w", err)
	}
	return entity, ev, nil
}
func (c *Coordinator) Pause(ctx context.Context, id, actor, reason string, expected int64) (campaign.Campaign, error) {
	entity, err := c.Campaigns.Transition(ctx, id, campaign.TransitionInput{Action: campaign.ActionPause, ActorID: actor, Reason: reason, ExpectedVersion: expected})
	if err != nil {
		return entity, err
	}
	if err := c.Store.RecordEvent(ctx, id, "CAMPAIGN_PAUSED", actor, reason, nil, c.now()); err != nil {
		return entity, fmt.Errorf("campaign paused but execution event could not be recorded: %w", err)
	}
	return entity, nil
}
func (c *Coordinator) Resume(ctx context.Context, id, actor, reason string, expected int64) (campaign.Campaign, CapacityEvidence, error) {
	entity, err := c.Campaigns.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return campaign.Campaign{}, CapacityEvidence{}, err
	}
	if err := c.checkMaintenance(ctx, platformpolicy.OperationCampaignResume, entity); err != nil {
		return campaign.Campaign{}, CapacityEvidence{}, err
	}
	window, err := entity.EvaluateDispatchWindow(c.now())
	if err != nil {
		return campaign.Campaign{}, CapacityEvidence{}, err
	}
	if !window.Allowed {
		return campaign.Campaign{}, CapacityEvidence{}, fmt.Errorf("campaign dispatch window blocked: %s", window.Reason)
	}
	ev, err := c.Plan(ctx, id)
	if err != nil {
		return campaign.Campaign{}, CapacityEvidence{}, err
	}
	if ev.Decision != DecisionAdmit {
		return campaign.Campaign{}, ev, fmt.Errorf("campaign resume admission decision is %s: %v", ev.Decision, ev.Reasons)
	}
	entity, err = c.Campaigns.Transition(ctx, id, campaign.TransitionInput{Action: campaign.ActionResume, ActorID: actor, Reason: reason, ExpectedVersion: expected})
	if err != nil {
		return entity, ev, err
	}
	if err := c.Store.RecordEvent(ctx, id, "CAMPAIGN_RESUMED", actor, reason, map[string]any{"forecastCompletionAt": ev.ForecastCompletionAt}, c.now()); err != nil {
		return entity, ev, fmt.Errorf("campaign resumed but execution event could not be recorded: %w", err)
	}
	return entity, ev, nil
}
func (c *Coordinator) Cancel(ctx context.Context, id, actor, reason string, expected int64) (campaign.Campaign, error) {
	if strings.TrimSpace(reason) == "" {
		return campaign.Campaign{}, errors.New("cancellation reason is required")
	}
	entity, err := c.Campaigns.Transition(ctx, id, campaign.TransitionInput{Action: campaign.ActionCancel, ActorID: actor, Reason: reason, ExpectedVersion: expected})
	if err == nil {
		if c.RoutingPlans != nil {
			plan, planErr := c.RoutingPlans.LatestByCampaign(ctx, id)
			if planErr == nil {
				if releaseErr := c.RoutingPlans.Release(ctx, plan.ID, actor); releaseErr != nil {
					return entity, fmt.Errorf("campaign cancelled but routing reservations could not be released: %w", releaseErr)
				}
			} else if !errors.Is(planErr, ErrRoutingPlanNotFound) {
				return entity, fmt.Errorf("campaign cancelled but routing plan could not be loaded: %w", planErr)
			}
		}
		if eventErr := c.Store.RecordEvent(ctx, id, "CAMPAIGN_CANCELLED", actor, reason, nil, c.now()); eventErr != nil {
			return entity, fmt.Errorf("campaign cancelled but execution event could not be recorded: %w", eventErr)
		}
	}
	return entity, err
}
func (c *Coordinator) AssessAndComplete(ctx context.Context, id, actor string, expected int64) (campaign.Campaign, CompletionAssessment, error) {
	m, err := c.Store.Metrics(ctx, id)
	if err != nil {
		return campaign.Campaign{}, CompletionAssessment{}, err
	}
	a, err := AssessCompletion(id, m, c.now())
	if err != nil {
		return campaign.Campaign{}, a, err
	}
	if a.State == CompletionInProgress {
		return campaign.Campaign{}, a, nil
	}
	entity, err := c.Campaigns.Transition(ctx, id, campaign.TransitionInput{Action: campaign.ActionComplete, ActorID: actor, ExpectedVersion: expected, FailedCount: m.Failed, UnknownCount: m.Unknown})
	if err != nil {
		return campaign.Campaign{}, a, err
	}
	if c.RoutingPlans != nil {
		plan, planErr := c.RoutingPlans.LatestByCampaign(ctx, id)
		if planErr == nil {
			if releaseErr := c.RoutingPlans.Release(ctx, plan.ID, actor); releaseErr != nil {
				return entity, a, fmt.Errorf("campaign completed but routing reservations could not be released: %w", releaseErr)
			}
		} else if !errors.Is(planErr, ErrRoutingPlanNotFound) {
			return entity, a, fmt.Errorf("campaign completed but routing plan could not be loaded: %w", planErr)
		}
	}
	if err := c.Store.RecordEvent(ctx, id, string(a.State), actor, "automatic completion assessment", map[string]any{"failed": a.Failed, "unknown": a.Unknown}, c.now()); err != nil {
		return entity, a, fmt.Errorf("campaign completed but execution event could not be recorded: %w", err)
	}
	return entity, a, nil
}
