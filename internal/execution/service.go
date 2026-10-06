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
	PrepareTransition(context.Context, string, campaign.TransitionInput) (campaign.Campaign, error)
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
	Committer           LifecycleCommitter
	SafetyMarginPercent int
	RoutingPlans        *RoutingAdministration
	TestMessages        PilotTestMessageReader
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
	if entity.Transport.Provider == campaign.ProviderMeta && c.RoutingPlans == nil {
		return CapacityEvidence{}, Metrics{}, errors.New("Meta Cloud execution requires an approved routing plan")
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
			evidence = CapacityEvidence{CampaignID: entity.ID, PoolID: plan.ID, routingPlanID: plan.ID, EvidenceVersion: plan.CapacityEvidenceVersion, RemainingRecipients: remaining, AvailableMessagesPerMinute: multi.EffectiveMessagesPerMinute, AvailableDailyCapacity: multi.EffectiveDailyUnits, SafetyMarginPercent: c.SafetyMarginPercent, RequiredMessagesPerMinute: multi.RequiredMessagesPerMinute, EffectiveMessagesPerMinute: float64(multi.EffectiveMessagesPerMinute), ForecastCompletionAt: multi.ForecastCompletionAt, DeadlineAt: *entity.CompletionDeadlineAt, Decision: multi.Decision, Reasons: multi.Reasons, EvaluatedAt: multi.EvaluatedAt}
		} else if errors.Is(listErr, ErrRoutingPlanNotFound) {
			if entity.Transport.Provider == campaign.ProviderMeta {
				return CapacityEvidence{}, Metrics{}, errors.New("Meta Cloud execution requires an approved routing plan")
			}
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

func (c *Coordinator) commitLifecycle(
	ctx context.Context,
	id string,
	input campaign.TransitionInput,
	eventType string,
	details map[string]any,
	routingPlanID string,
	reservationOperation ReservationOperation,
	executionLease *ExecutionLeaseFence,
) (campaign.Campaign, error) {
	if c == nil || c.Committer == nil {
		return campaign.Campaign{}, ErrLifecycleCommitterRequired
	}
	entity, err := c.Campaigns.PrepareTransition(ctx, id, input)
	if err != nil {
		return campaign.Campaign{}, err
	}
	value := LifecycleCommit{
		Campaign: entity, ExpectedVersion: input.ExpectedVersion,
		Action: input.Action, EventType: eventType, ActorID: input.ActorID,
		Reason: input.Reason, Details: details, RoutingPlanID: routingPlanID,
		ReservationOperation: reservationOperation, OccurredAt: entity.UpdatedAt,
		ExecutionLease: executionLease,
	}
	if err := value.Validate(); err != nil {
		return campaign.Campaign{}, err
	}
	return c.Committer.Commit(ctx, value)
}

func (c *Coordinator) ApproveFinal(ctx context.Context, id string, input campaign.TransitionInput) (campaign.Campaign, error) {
	if c == nil || c.Committer == nil {
		return campaign.Campaign{}, ErrLifecycleCommitterRequired
	}
	if input.Action != campaign.ActionApproveFinal {
		return campaign.Campaign{}, errors.New("final approval requires APPROVE_FINAL action")
	}
	entity, err := c.Campaigns.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return campaign.Campaign{}, err
	}
	if input.ExpectedVersion != entity.Version {
		return campaign.Campaign{}, campaign.ErrConflict
	}
	pilot, err := c.ValidateDayOnePilot(ctx, entity)
	if err != nil {
		return campaign.Campaign{}, err
	}
	planID := ""
	reservationOperation := ReservationNone
	details := map[string]any{}
	if entity.Transport.Provider == campaign.ProviderOpenWA {
		planID = pilot.RoutingPlanID
		reservationOperation = ReservationValidateHeld
		details["acceptedTestMessageId"] = pilot.AcceptedTestMessageID
		details["routingPlanId"] = pilot.RoutingPlanID
		details["routingPlanVersion"] = pilot.RoutingPlanVersion
		details["reservationId"] = pilot.ReservationID
	}
	return c.commitLifecycle(
		ctx, id, input, "CAMPAIGN_FINAL_APPROVED", details,
		planID, reservationOperation, nil,
	)
}

func startRoutingPlanActivation(
	entity campaign.Campaign,
	pilot PilotAdmissionEvidence,
	evidence CapacityEvidence,
) (string, ReservationOperation, error) {
	if entity.Transport.Provider == campaign.ProviderOpenWA {
		planID := strings.TrimSpace(pilot.RoutingPlanID)
		if planID == "" {
			return "", ReservationNone, fmt.Errorf("%w: OpenWA start requires a pilot-bound routing plan", ErrPilotAdmission)
		}
		return planID, ReservationActivate, nil
	}
	if planID := strings.TrimSpace(evidence.routingPlanID); planID != "" {
		// Bind activation to the exact plan that Plan() validated and assessed.
		// Never re-read "latest" here: a scheduled operator may release that
		// plan and approve a replacement before lifecycle commit. Activating the
		// assessed plan makes that race fail closed because released reservations
		// are not activatable.
		return planID, ReservationActivate, nil
	}
	if entity.Transport.Provider == campaign.ProviderMeta {
		return "", ReservationNone, errors.New("Meta Cloud start requires the assessed routing plan identity")
	}
	// Preserve the existing execution behavior for legacy/noncanonical
	// transports that truly used the single-pool admission path.
	return "", ReservationNone, nil
}

func (c *Coordinator) Start(ctx context.Context, id, actor, reason string, expected int64) (campaign.Campaign, CapacityEvidence, error) {
	return c.start(ctx, id, actor, reason, expected, nil)
}

func (c *Coordinator) StartWithExecutionLease(
	ctx context.Context, id, actor, reason string, expected int64,
	lease ExecutionLeaseFence,
) (campaign.Campaign, CapacityEvidence, error) {
	return c.start(ctx, id, actor, reason, expected, &lease)
}

func (c *Coordinator) start(
	ctx context.Context, id, actor, reason string, expected int64,
	lease *ExecutionLeaseFence,
) (campaign.Campaign, CapacityEvidence, error) {
	if c == nil || c.Committer == nil {
		return campaign.Campaign{}, CapacityEvidence{}, ErrLifecycleCommitterRequired
	}
	entity, err := c.Campaigns.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return campaign.Campaign{}, CapacityEvidence{}, err
	}
	if expected != entity.Version {
		return campaign.Campaign{}, CapacityEvidence{}, campaign.ErrConflict
	}
	pilot, err := c.ValidateDayOnePilot(ctx, entity)
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
	planID, reservationOperation, err := startRoutingPlanActivation(entity, pilot, ev)
	if err != nil {
		return campaign.Campaign{}, ev, err
	}
	entity, err = c.commitLifecycle(
		ctx, id,
		campaign.TransitionInput{
			Action: campaign.ActionStartDispatch, ActorID: actor,
			Reason: reason, ExpectedVersion: expected,
		},
		"DISPATCH_STARTED",
		map[string]any{
			"capacityEvidenceVersion": ev.EvidenceVersion,
			"forecastCompletionAt":    ev.ForecastCompletionAt,
			"acceptedTestMessageId":   pilot.AcceptedTestMessageID,
			"routingPlanId":           pilot.RoutingPlanID,
			"routingPlanVersion":      pilot.RoutingPlanVersion,
			"reservationId":           pilot.ReservationID,
		},
		planID, reservationOperation, lease,
	)
	return entity, ev, err
}
func (c *Coordinator) Pause(ctx context.Context, id, actor, reason string, expected int64) (campaign.Campaign, error) {
	return c.commitLifecycle(
		ctx, id,
		campaign.TransitionInput{
			Action: campaign.ActionPause, ActorID: actor,
			Reason: reason, ExpectedVersion: expected,
		},
		"CAMPAIGN_PAUSED", nil, "", ReservationNone, nil,
	)
}
func (c *Coordinator) Resume(ctx context.Context, id, actor, reason string, expected int64) (campaign.Campaign, CapacityEvidence, error) {
	if c == nil || c.Committer == nil {
		return campaign.Campaign{}, CapacityEvidence{}, ErrLifecycleCommitterRequired
	}
	entity, err := c.Campaigns.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return campaign.Campaign{}, CapacityEvidence{}, err
	}
	if expected != entity.Version {
		return campaign.Campaign{}, CapacityEvidence{}, campaign.ErrConflict
	}
	pilot, err := c.ValidateDayOnePilot(ctx, entity)
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
	entity, err = c.commitLifecycle(
		ctx, id,
		campaign.TransitionInput{
			Action: campaign.ActionResume, ActorID: actor,
			Reason: reason, ExpectedVersion: expected,
		},
		"CAMPAIGN_RESUMED",
		map[string]any{
			"forecastCompletionAt":  ev.ForecastCompletionAt,
			"acceptedTestMessageId": pilot.AcceptedTestMessageID,
			"routingPlanId":         pilot.RoutingPlanID,
			"routingPlanVersion":    pilot.RoutingPlanVersion,
			"reservationId":         pilot.ReservationID,
		},
		"", ReservationNone, nil,
	)
	return entity, ev, err
}
func (c *Coordinator) Cancel(ctx context.Context, id, actor, reason string, expected int64) (campaign.Campaign, error) {
	if strings.TrimSpace(reason) == "" {
		return campaign.Campaign{}, errors.New("cancellation reason is required")
	}
	if c == nil || c.Committer == nil {
		return campaign.Campaign{}, ErrLifecycleCommitterRequired
	}
	planID := ""
	reservationOperation := ReservationNone
	if c.RoutingPlans != nil {
		plan, planErr := c.RoutingPlans.LatestByCampaign(ctx, id)
		if planErr == nil {
			planID = plan.ID
			reservationOperation = ReservationRelease
		} else if !errors.Is(planErr, ErrRoutingPlanNotFound) {
			return campaign.Campaign{}, fmt.Errorf("load routing plan for cancellation: %w", planErr)
		}
	}
	return c.commitLifecycle(
		ctx, id,
		campaign.TransitionInput{
			Action: campaign.ActionCancel, ActorID: actor,
			Reason: reason, ExpectedVersion: expected,
		},
		"CAMPAIGN_CANCELLED", nil, planID, reservationOperation, nil,
	)
}
func (c *Coordinator) AssessAndComplete(ctx context.Context, id, actor string, expected int64) (campaign.Campaign, CompletionAssessment, error) {
	return c.assessAndComplete(ctx, id, actor, expected, nil)
}

func (c *Coordinator) AssessAndCompleteWithExecutionLease(
	ctx context.Context, id, actor string, expected int64,
	lease ExecutionLeaseFence,
) (campaign.Campaign, CompletionAssessment, error) {
	return c.assessAndComplete(ctx, id, actor, expected, &lease)
}

func (c *Coordinator) assessAndComplete(
	ctx context.Context, id, actor string, expected int64,
	lease *ExecutionLeaseFence,
) (campaign.Campaign, CompletionAssessment, error) {
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
	if c == nil || c.Committer == nil {
		return campaign.Campaign{}, a, ErrLifecycleCommitterRequired
	}
	planID := ""
	reservationOperation := ReservationNone
	if c.RoutingPlans != nil {
		plan, planErr := c.RoutingPlans.LatestByCampaign(ctx, id)
		if planErr == nil {
			planID = plan.ID
			reservationOperation = ReservationRelease
		} else if !errors.Is(planErr, ErrRoutingPlanNotFound) {
			return campaign.Campaign{}, a, fmt.Errorf("load routing plan for completion: %w", planErr)
		}
	}
	entity, err := c.commitLifecycle(
		ctx, id,
		campaign.TransitionInput{
			Action: campaign.ActionComplete, ActorID: actor,
			Reason:          "automatic completion assessment",
			ExpectedVersion: expected, FailedCount: m.Failed,
			UnknownCount: m.Unknown,
		},
		string(a.State), map[string]any{
			"failed": a.Failed, "unknown": a.Unknown,
		},
		planID, reservationOperation, lease,
	)
	return entity, a, err
}
