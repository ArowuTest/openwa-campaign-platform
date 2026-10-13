package httpserver

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/campaignpreparation"
	"campaign-platform/internal/execution"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/sender"
	"campaign-platform/internal/shared/httpx"
	"campaign-platform/internal/shared/id"
	"campaign-platform/internal/testmessage"
)

var (
	errCapacityHoldUnavailable = errors.New("campaign capacity hold is unavailable")
	errCapacityHoldBlocked     = errors.New("campaign capacity hold is blocked")
	errCapacityHoldConflict    = errors.New("campaign capacity hold conflict")
)

type campaignCapacityHoldRequest struct {
	ExpectedCampaignVersion int64  `json:"expectedCampaignVersion"`
	Reason                  string `json:"reason"`
	ClientRequestID         string `json:"clientRequestId"`
}

type campaignCapacityHoldProposal struct {
	CampaignID                 string    `json:"campaignId"`
	CampaignVersion            int64     `json:"campaignVersion"`
	SenderPoolID               string    `json:"senderPoolId"`
	GatewayPoolID              string    `json:"gatewayPoolId"`
	Provider                   string    `json:"provider"`
	Engine                     string    `json:"engine"`
	ReservationStart           time.Time `json:"reservationStart"`
	ReservationEnd             time.Time `json:"reservationEnd"`
	BasisRecipientCount        int64     `json:"basisRecipientCount"`
	MeasurementAsOf            time.Time `json:"measurementAsOf"`
	HealthySessions            int       `json:"healthySessions"`
	HealthyNodes               int       `json:"healthyNodes"`
	MinimumHealthyNodes        int       `json:"minimumHealthyNodes"`
	AvailableMessagesPerMinute int       `json:"availableMessagesPerMinute"`
	AvailableHourlyUnits       int64     `json:"availableHourlyUnits"`
	AvailableDailyUnits        int64     `json:"availableDailyUnits"`
	SafetyMarginPercent        int       `json:"safetyMarginPercent"`
	RequiredMessagesPerMinute  float64   `json:"requiredMessagesPerMinute"`
	ReservedMessagesPerMinute  int       `json:"reservedMessagesPerMinute"`
	ReservedHourlyUnits        int64     `json:"reservedHourlyUnits"`
	ReservedDailyUnits         int64     `json:"reservedDailyUnits"`
	PacingPolicyID             string    `json:"pacingPolicyId"`
	PacingPolicyVersion        int64     `json:"pacingPolicyVersion"`
	PacingMessagesPerMinute    int       `json:"pacingMessagesPerMinute"`
	PacingHourlyAllowance      int64     `json:"pacingHourlyAllowance"`
	PacingDailyAllowance       int64     `json:"pacingDailyAllowance"`
	RoutingPolicyVersion       string    `json:"routingPolicyVersion"`
	CapacityEvidenceVersion    string    `json:"capacityEvidenceVersion"`
	Classification             string    `json:"classification"`
}

func capacityCheck(readiness campaignpreparation.Readiness) (campaignpreparation.ReadinessCheck, bool) {
	for _, check := range readiness.Checks {
		if check.Key == "capacity" {
			return check, true
		}
	}
	return campaignpreparation.ReadinessCheck{}, false
}

func pacingScopesForCampaign(entity campaign.Campaign) []sender.PacingScopeRef {
	scopes := []sender.PacingScopeRef{{Scope: sender.PacingPlatform}}
	if entity.Transport.Provider != "" {
		scopes = append(scopes, sender.PacingScopeRef{Scope: sender.PacingProvider, ID: string(entity.Transport.Provider)})
	}
	if entity.Transport.Engine != "" {
		scopes = append(scopes, sender.PacingScopeRef{Scope: sender.PacingEngine, ID: string(entity.Transport.Engine)})
	}
	if entity.Transport.GatewayPoolID != "" {
		scopes = append(scopes, sender.PacingScopeRef{Scope: sender.PacingGatewayPool, ID: entity.Transport.GatewayPoolID})
	}
	if entity.Transport.SenderPoolID != "" {
		scopes = append(scopes, sender.PacingScopeRef{Scope: sender.PacingSenderPool, ID: entity.Transport.SenderPoolID})
	}
	return append(scopes, sender.PacingScopeRef{Scope: sender.PacingCampaign, ID: entity.ID})
}

func acceptedControlledTestForCapacityHold(ctx context.Context, service *testmessage.Service, entity campaign.Campaign) (bool, error) {
	if service == nil {
		return false, errCapacityHoldUnavailable
	}
	if entity.MessageVersionID == "" || entity.MessageContentHash == "" {
		return false, nil
	}
	cursor := ""
	const pageSize = 100
	const maxPages = 10
	for pageIndex := 0; pageIndex < maxPages; pageIndex++ {
		page, err := service.ListSendsPage(ctx, entity.ID, pageSize, cursor)
		if err != nil {
			return false, errCapacityHoldUnavailable
		}
		for _, send := range page.Items {
			if send.Status == testmessage.SendAccepted &&
				send.CampaignID == entity.ID &&
				send.MessageVersionID == entity.MessageVersionID &&
				send.MessageContentHash == entity.MessageContentHash &&
				send.GatewayPoolID == entity.Transport.GatewayPoolID &&
				send.GatewayPoolVersion == entity.Transport.GatewayPoolVersion &&
				send.SenderPoolID == entity.Transport.SenderPoolID &&
				send.Provider == string(entity.Transport.Provider) &&
				send.Engine == string(entity.Transport.Engine) &&
				send.ProviderAdapterVersion == entity.Transport.AdapterVersion &&
				send.ProviderDefinitionID == entity.Transport.ProviderDefinitionID &&
				send.ProviderDefinitionVersion == entity.Transport.ProviderDefinitionVersion {
				return true, nil
			}
		}
		if page.NextCursor == "" {
			return false, nil
		}
		cursor = page.NextCursor
	}
	return false, errCapacityHoldBlocked
}

func findCapacityHoldReplay(ctx context.Context, admin *execution.RoutingAdministration, campaignID, clientRequestID, actor, reason string, expectedCampaignVersion int64) (execution.RoutingPlan, bool, error) {
	if admin == nil {
		return execution.RoutingPlan{}, false, errCapacityHoldUnavailable
	}
	plan, err := admin.GetByIdempotencyKey(ctx, campaignID, clientRequestID)
	if errors.Is(err, execution.ErrRoutingPlanNotFound) {
		return execution.RoutingPlan{}, false, nil
	}
	if err != nil {
		return execution.RoutingPlan{}, false, errCapacityHoldUnavailable
	}
	if plan.ApprovedBy != actor || plan.ApprovalReason != reason ||
		plan.ApprovedCampaignVersion != expectedCampaignVersion ||
		plan.FallbackMode != "NONE" || len(plan.Routes) != 1 {
		return execution.RoutingPlan{}, false, errCapacityHoldConflict
	}
	route := plan.Routes[0]
	if route.Provider != string(campaign.ProviderOpenWA) ||
		(route.Engine != string(campaign.EngineBaileys) && route.Engine != string(campaign.EngineWhatsAppWebJS)) ||
		route.GatewayPoolID == "" || route.MetaSenderID != "" ||
		route.AllowReallocationIn || route.AllowReallocationOut {
		return execution.RoutingPlan{}, false, errCapacityHoldConflict
	}
	return plan, true, nil
}

func buildCampaignCapacityHoldProposal(entity campaign.Campaign, readiness campaignpreparation.Readiness, pacing sender.ResolvedPacing) (campaignCapacityHoldProposal, error) {
	switch entity.Status {
	case campaign.StatusMessageApproved, campaign.StatusCommercialApproved, campaign.StatusFinalApprovalPending:
	default:
		return campaignCapacityHoldProposal{}, errCapacityHoldBlocked
	}
	if entity.Transport.Provider != campaign.ProviderOpenWA ||
		entity.Transport.RoutingMode != campaign.RoutingSenderPool ||
		entity.Transport.FallbackMode != campaign.FallbackNone ||
		entity.Transport.SenderPoolID == "" || entity.Transport.GatewayPoolID == "" {
		return campaignCapacityHoldProposal{}, errCapacityHoldBlocked
	}
	if entity.RequestedStartAt == nil || entity.CompletionDeadlineAt == nil || !entity.CompletionDeadlineAt.After(*entity.RequestedStartAt) {
		return campaignCapacityHoldProposal{}, errCapacityHoldBlocked
	}
	if readiness.CampaignID != entity.ID || readiness.CampaignVersion != entity.Version || readiness.Capacity == nil {
		return campaignCapacityHoldProposal{}, errCapacityHoldConflict
	}
	check, ok := capacityCheck(readiness)
	if !ok || check.Status != "PASS" || readiness.Capacity.Classification != "ADVISORY_ONLY" {
		return campaignCapacityHoldProposal{}, errCapacityHoldBlocked
	}
	preview := readiness.Capacity
	if preview.SenderPoolID != entity.Transport.SenderPoolID || preview.GatewayPoolID != entity.Transport.GatewayPoolID ||
		preview.RequiredMessagesPerMinute == nil || *preview.RequiredMessagesPerMinute <= 0 ||
		preview.BasisRecipientCount < 1 || preview.AvailableMessagesPerMinute < 1 ||
		preview.AvailableHourlyUnits < 1 || preview.AvailableDailyUnits < 1 {
		return campaignCapacityHoldProposal{}, errCapacityHoldBlocked
	}
	if pacing.Policy.ID == "" || pacing.Policy.Version < 1 || pacing.Policy.Status != sender.PacingActive ||
		pacing.Policy.MessagesPerMinute < 1 || pacing.Policy.HourlyAllowance < 1 || pacing.Policy.DailyAllowance < 1 {
		return campaignCapacityHoldProposal{}, errCapacityHoldBlocked
	}
	margin := 1 - float64(preview.SafetyMarginPercent)/100
	if margin <= 0 || margin > 1 {
		return campaignCapacityHoldProposal{}, errCapacityHoldBlocked
	}
	reservedMPMFloat := math.Ceil(*preview.RequiredMessagesPerMinute / margin)
	if reservedMPMFloat < 1 || reservedMPMFloat > float64(campaignpreparation.MaxSafeInteger) {
		return campaignCapacityHoldProposal{}, errCapacityHoldBlocked
	}
	reservedMPM := int(reservedMPMFloat)
	reservedHourly := int64(reservedMPM) * 60
	if reservedHourly > preview.BasisRecipientCount {
		reservedHourly = preview.BasisRecipientCount
	}
	window := entity.CompletionDeadlineAt.Sub(*entity.RequestedStartAt)
	reservedDaily, dailyErr := execution.MinimumDailyReservationForWindow(
		preview.BasisRecipientCount, reservedMPM, reservedHourly, window,
	)
	if dailyErr != nil {
		return campaignCapacityHoldProposal{}, errCapacityHoldBlocked
	}
	if reservedMPM > preview.AvailableMessagesPerMinute || reservedMPM > pacing.Policy.MessagesPerMinute ||
		reservedHourly > preview.AvailableHourlyUnits || reservedHourly > pacing.Policy.HourlyAllowance ||
		reservedDaily > preview.AvailableDailyUnits || reservedDaily > pacing.Policy.DailyAllowance {
		return campaignCapacityHoldProposal{}, errCapacityHoldBlocked
	}
	return campaignCapacityHoldProposal{
		CampaignID: entity.ID, CampaignVersion: entity.Version,
		SenderPoolID: entity.Transport.SenderPoolID, GatewayPoolID: entity.Transport.GatewayPoolID,
		Provider: string(entity.Transport.Provider), Engine: string(entity.Transport.Engine),
		ReservationStart: entity.RequestedStartAt.UTC(), ReservationEnd: entity.CompletionDeadlineAt.UTC(),
		BasisRecipientCount: preview.BasisRecipientCount, MeasurementAsOf: preview.MeasurementAsOf,
		HealthySessions: preview.HealthySessions, HealthyNodes: preview.HealthyNodes, MinimumHealthyNodes: preview.MinimumHealthyNodes,
		AvailableMessagesPerMinute: preview.AvailableMessagesPerMinute, AvailableHourlyUnits: preview.AvailableHourlyUnits,
		AvailableDailyUnits: preview.AvailableDailyUnits, SafetyMarginPercent: preview.SafetyMarginPercent,
		RequiredMessagesPerMinute: *preview.RequiredMessagesPerMinute, ReservedMessagesPerMinute: reservedMPM,
		ReservedHourlyUnits: reservedHourly, ReservedDailyUnits: reservedDaily,
		PacingPolicyID: pacing.Policy.ID, PacingPolicyVersion: pacing.Policy.Version,
		PacingMessagesPerMinute: pacing.Policy.MessagesPerMinute, PacingHourlyAllowance: pacing.Policy.HourlyAllowance,
		PacingDailyAllowance:    pacing.Policy.DailyAllowance,
		RoutingPolicyVersion:    entity.Transport.RoutingPolicyVersion,
		CapacityEvidenceVersion: entity.Transport.CapacityEvidenceVersion,
		Classification:          "READY_TO_HOLD",
	}, nil
}

func (s *Server) campaignCapacityHoldProposal(r *http.Request) (campaignCapacityHoldProposal, campaign.Campaign, sender.ResolvedPacing, error) {
	if s.deps.CampaignPreparation == nil || s.deps.Campaigns == nil || s.deps.PacingPolicies == nil || s.deps.TestMessages == nil {
		return campaignCapacityHoldProposal{}, campaign.Campaign{}, sender.ResolvedPacing{}, errCapacityHoldUnavailable
	}
	identifier := id.CanonicalUUID(r.PathValue("id"))
	if !id.IsUUID(identifier) {
		return campaignCapacityHoldProposal{}, campaign.Campaign{}, sender.ResolvedPacing{}, campaign.ErrNotFound
	}
	entity, err := s.deps.Campaigns.Get(r.Context(), identifier)
	if err != nil {
		return campaignCapacityHoldProposal{}, campaign.Campaign{}, sender.ResolvedPacing{}, err
	}
	acceptedTest, err := acceptedControlledTestForCapacityHold(r.Context(), s.deps.TestMessages, entity)
	if err != nil {
		return campaignCapacityHoldProposal{}, campaign.Campaign{}, sender.ResolvedPacing{}, err
	}
	if !acceptedTest {
		return campaignCapacityHoldProposal{}, campaign.Campaign{}, sender.ResolvedPacing{}, errCapacityHoldBlocked
	}
	readiness, err := s.deps.CampaignPreparation.Get(r.Context(), identifier)
	if err != nil {
		return campaignCapacityHoldProposal{}, campaign.Campaign{}, sender.ResolvedPacing{}, err
	}
	if entity.RequestedStartAt == nil || entity.CompletionDeadlineAt == nil || !entity.CompletionDeadlineAt.After(*entity.RequestedStartAt) {
		return campaignCapacityHoldProposal{}, campaign.Campaign{}, sender.ResolvedPacing{}, errCapacityHoldBlocked
	}
	scopes := pacingScopesForCampaign(entity)
	pacing, err := s.deps.PacingPolicies.ResolveStableWindow(
		r.Context(), scopes, entity.RequestedStartAt.UTC(), entity.CompletionDeadlineAt.UTC(),
	)
	if err != nil {
		return campaignCapacityHoldProposal{}, campaign.Campaign{}, sender.ResolvedPacing{}, errCapacityHoldBlocked
	}
	proposal, err := buildCampaignCapacityHoldProposal(entity, readiness, pacing)
	return proposal, entity, pacing, err
}

func (s *Server) getCampaignCapacityHoldProposal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	proposal, _, _, err := s.campaignCapacityHoldProposal(r)
	switch {
	case errors.Is(err, campaign.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
	case errors.Is(err, errCapacityHoldUnavailable), errors.Is(err, campaignpreparation.ErrUnavailable):
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CAPACITY_HOLD_UNAVAILABLE", "Authoritative capacity hold evidence is unavailable.", nil)
	case errors.Is(err, errCapacityHoldConflict), errors.Is(err, campaign.ErrConflict):
		httpx.WriteError(w, r, http.StatusConflict, "CAPACITY_HOLD_STALE", "The campaign changed while capacity was measured. Reload before continuing.", nil)
	case err != nil:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CAPACITY_HOLD_BLOCKED", "Current route, schedule, pacing or measured capacity cannot support a governed hold.", nil)
	default:
		httpx.WriteJSON(w, http.StatusOK, proposal)
	}
}

func (s *Server) createCampaignCapacityHold(w http.ResponseWriter, r *http.Request) {
	if s.deps.RoutingPlans == nil || s.deps.Campaigns == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CAPACITY_HOLD_UNAVAILABLE", "Routing-plan or campaign authority is unavailable.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "MFA_STEP_UP_REQUIRED", "Recent MFA verification is required to reserve campaign capacity.", nil)
		return
	}
	var input campaignCapacityHoldRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The capacity-hold confirmation is invalid.", nil)
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	input.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	if input.ExpectedCampaignVersion < 1 || len(input.Reason) < 8 || len(input.Reason) > 1000 ||
		len(input.ClientRequestID) < 16 || len(input.ClientRequestID) > 128 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CAPACITY_HOLD_CONFIRMATION_INVALID", "Expected version, meaningful reason and client request ID are required.", nil)
		return
	}
	campaignID := id.CanonicalUUID(r.PathValue("id"))
	if !id.IsUUID(campaignID) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	existing, replayed, replayErr := findCapacityHoldReplay(
		r.Context(), s.deps.RoutingPlans, campaignID, input.ClientRequestID, principal.User.ID, input.Reason,
		input.ExpectedCampaignVersion,
	)
	switch {
	case errors.Is(replayErr, errCapacityHoldConflict):
		httpx.WriteError(w, r, http.StatusConflict, "CAPACITY_HOLD_REPLAY_CONFLICT", "The client request ID was already used with different approval evidence.", nil)
		return
	case errors.Is(replayErr, errCapacityHoldUnavailable):
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CAPACITY_HOLD_UNAVAILABLE", "Routing-plan replay evidence is unavailable.", nil)
		return
	case replayErr != nil:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CAPACITY_HOLD_BLOCKED", "Capacity-hold replay evidence could not be proven safely.", nil)
		return
	case replayed:
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"plan": existing, "replayed": true})
		return
	}
	currentCampaign, currentErr := s.deps.Campaigns.Get(r.Context(), campaignID)
	if errors.Is(currentErr, campaign.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	if currentErr != nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CAPACITY_HOLD_UNAVAILABLE", "Current campaign authority is unavailable.", nil)
		return
	}
	if currentCampaign.Version != input.ExpectedCampaignVersion {
		httpx.WriteError(w, r, http.StatusConflict, "CAPACITY_HOLD_STALE", "The campaign changed; reload measured capacity before confirming.", nil)
		return
	}
	proposal, entity, pacing, err := s.campaignCapacityHoldProposal(r)
	if err != nil {
		getCampaignCapacityHoldError(w, r, err)
		return
	}
	if entity.Version != input.ExpectedCampaignVersion {
		httpx.WriteError(w, r, http.StatusConflict, "CAPACITY_HOLD_STALE", "The campaign changed; reload measured capacity before confirming.", nil)
		return
	}
	plan := execution.RoutingPlan{
		CampaignID:              entity.ID,
		DistributionMode:        execution.DistributionWeighted,
		RoutingPolicyVersion:    proposal.RoutingPolicyVersion,
		CapacityEvidenceVersion: proposal.CapacityEvidenceVersion,
		PacingPolicyVersion:     pacing.Policy.ID + "/v" + strconv.FormatInt(pacing.Policy.Version, 10),
		FallbackMode:            "NONE",
		ApprovalReason:          input.Reason,
		IdempotencyKey:          input.ClientRequestID,
		Routes: []execution.PoolRoute{{
			SenderPoolID: proposal.SenderPoolID, GatewayPoolID: proposal.GatewayPoolID,
			Provider: proposal.Provider, Engine: proposal.Engine, AllocationWeight: 100,
			MaximumRecipients:         entity.MaximumUniqueRecipients,
			ReservedMessagesPerMinute: proposal.ReservedMessagesPerMinute,
			ReservedHourlyUnits:       proposal.ReservedHourlyUnits, ReservedDailyUnits: proposal.ReservedDailyUnits,
			AllowReallocationIn: false, AllowReallocationOut: false,
		}},
	}
	value, err := s.deps.RoutingPlans.CreateApproved(r.Context(), plan, principal.User.ID)
	switch {
	case errors.Is(err, execution.ErrCapacityOverbooked):
		httpx.WriteError(w, r, http.StatusConflict, "ROUTING_CAPACITY_OVERBOOKED", "Measured capacity changed or is already reserved. Reload before continuing.", nil)
	case errors.Is(err, execution.ErrRoutingPlanConflict):
		httpx.WriteError(w, r, http.StatusConflict, "CAPACITY_HOLD_CONFLICT", "A live or conflicting capacity hold already exists.", nil)
	case err != nil:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CAPACITY_HOLD_REJECTED", "The governed capacity hold could not be created.", nil)
	default:
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{"plan": value, "proposal": proposal})
	}
}

func getCampaignCapacityHoldError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, campaign.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
	case errors.Is(err, errCapacityHoldUnavailable), errors.Is(err, campaignpreparation.ErrUnavailable):
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CAPACITY_HOLD_UNAVAILABLE", "Authoritative capacity hold evidence is unavailable.", nil)
	case errors.Is(err, errCapacityHoldConflict), errors.Is(err, campaign.ErrConflict):
		httpx.WriteError(w, r, http.StatusConflict, "CAPACITY_HOLD_STALE", "The campaign changed while capacity was measured.", nil)
	default:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CAPACITY_HOLD_BLOCKED", "Current route, schedule, controlled-test, pacing or measured capacity evidence cannot support a governed hold.", nil)
	}
}
