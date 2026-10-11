package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/testmessage"
)

var ErrPilotAdmission = errors.New("day-one pilot admission evidence is invalid")

type PilotAdmissionEvidence struct {
	AcceptedTestMessageID string
	RoutingPlanID         string
	RoutingPlanVersion    int64
	ReservationID         string
	ReservationStatus     string
	SenderPoolID          string
	GatewayPoolID         string
	Provider              string
	Engine                string
}

func EvaluateDayOnePilotAdmission(
	entity campaign.Campaign,
	sends []testmessage.Send,
	plan RoutingPlan,
	reservations []CapacityReservation,
) (PilotAdmissionEvidence, error) {
	if entity.Transport.Provider != campaign.ProviderOpenWA {
		return PilotAdmissionEvidence{}, nil
	}
	if entity.Transport.RoutingMode != campaign.RoutingSenderPool || entity.Transport.FallbackMode != campaign.FallbackNone {
		return PilotAdmissionEvidence{}, fmt.Errorf("%w: day-one OpenWA execution requires one sender pool and fallback NONE", ErrPilotAdmission)
	}
	if strings.TrimSpace(entity.Transport.SenderPoolID) == "" || strings.TrimSpace(entity.Transport.GatewayPoolID) == "" {
		return PilotAdmissionEvidence{}, fmt.Errorf("%w: campaign route is incomplete", ErrPilotAdmission)
	}
	if plan.CampaignID != entity.ID || plan.ID == "" || plan.Version <= 0 || len(plan.Routes) != 1 || plan.FallbackMode != "NONE" {
		return PilotAdmissionEvidence{}, fmt.Errorf("%w: exactly one approved routing-plan route is required", ErrPilotAdmission)
	}
	route := plan.Routes[0]
	if route.AllowReallocationIn || route.AllowReallocationOut {
		return PilotAdmissionEvidence{}, fmt.Errorf("%w: automatic route reallocation is not enabled for day one", ErrPilotAdmission)
	}
	if plan.RoutingPolicyVersion != entity.Transport.RoutingPolicyVersion ||
		plan.CapacityEvidenceVersion != entity.Transport.CapacityEvidenceVersion ||
		route.SenderPoolID != entity.Transport.SenderPoolID ||
		route.GatewayPoolID != entity.Transport.GatewayPoolID ||
		route.Provider != string(entity.Transport.Provider) ||
		route.Engine != string(entity.Transport.Engine) ||
		route.ProviderAdapterVersion != entity.Transport.AdapterVersion ||
		route.ProviderDefinitionID != entity.Transport.ProviderDefinitionID ||
		route.ProviderDefinitionVersion != entity.Transport.ProviderDefinitionVersion ||
		route.GatewayPoolVersion != entity.Transport.GatewayPoolVersion ||
		route.MaximumRecipients < entity.MaximumUniqueRecipients {
		return PilotAdmissionEvidence{}, fmt.Errorf("%w: routing plan does not exactly match the approved campaign transport", ErrPilotAdmission)
	}
	if len(reservations) != 1 {
		return PilotAdmissionEvidence{}, fmt.Errorf("%w: exactly one capacity reservation is required", ErrPilotAdmission)
	}
	reservation := reservations[0]
	requiredStatus := "HELD"
	if entity.Status == campaign.StatusDispatching || entity.Status == campaign.StatusPaused {
		requiredStatus = "ACTIVE"
	}
	if reservation.ID == "" || reservation.CampaignID != entity.ID || reservation.RoutingPlanID != plan.ID ||
		reservation.SenderPoolID != route.SenderPoolID || reservation.Status != requiredStatus ||
		reservation.FencingVersion <= 0 || entity.RequestedStartAt == nil || entity.CompletionDeadlineAt == nil ||
		!reservation.ReservationStart.Equal(entity.RequestedStartAt.UTC()) ||
		!reservation.ReservationEnd.Equal(entity.CompletionDeadlineAt.UTC()) ||
		reservation.ReservedMessagesPerMinute != route.ReservedMessagesPerMinute ||
		reservation.ReservedHourlyUnits != route.ReservedHourlyUnits ||
		reservation.ReservedDailyUnits != route.ReservedDailyUnits {
		return PilotAdmissionEvidence{}, fmt.Errorf("%w: the authoritative capacity reservation is not valid for the current campaign window", ErrPilotAdmission)
	}

	for _, send := range sends {
		if send.Status != testmessage.SendAccepted ||
			send.CampaignID != entity.ID ||
			send.MessageVersionID != entity.MessageVersionID ||
			send.MessageContentHash != entity.MessageContentHash ||
			send.GatewayPoolID != route.GatewayPoolID ||
			send.GatewayPoolVersion != route.GatewayPoolVersion ||
			send.SenderPoolID != route.SenderPoolID ||
			send.Provider != route.Provider ||
			send.Engine != route.Engine ||
			send.ProviderAdapterVersion != route.ProviderAdapterVersion ||
			send.ProviderDefinitionID != route.ProviderDefinitionID ||
			send.ProviderDefinitionVersion != route.ProviderDefinitionVersion ||
			strings.TrimSpace(send.SenderSessionID) == "" ||
			strings.TrimSpace(send.RouteReference) == "" {
			continue
		}
		return PilotAdmissionEvidence{
			AcceptedTestMessageID: send.ID,
			RoutingPlanID:         plan.ID,
			RoutingPlanVersion:    plan.Version,
			ReservationID:         reservation.ID,
			ReservationStatus:     reservation.Status,
			SenderPoolID:          route.SenderPoolID,
			GatewayPoolID:         route.GatewayPoolID,
			Provider:              route.Provider,
			Engine:                route.Engine,
		}, nil
	}
	return PilotAdmissionEvidence{}, fmt.Errorf("%w: no accepted controlled test matches the current message and exact OpenWA route", ErrPilotAdmission)
}

type PilotTestMessageReader interface {
	ListSends(context.Context, string) ([]testmessage.Send, error)
}

func (c *Coordinator) ValidateDayOnePilot(
	ctx context.Context,
	entity campaign.Campaign,
) (PilotAdmissionEvidence, error) {
	if entity.Transport.Provider != campaign.ProviderOpenWA {
		return PilotAdmissionEvidence{}, nil
	}
	if c == nil || c.RoutingPlans == nil || c.TestMessages == nil {
		return PilotAdmissionEvidence{}, fmt.Errorf("%w: pilot admission services are unavailable", ErrPilotAdmission)
	}
	plan, err := c.RoutingPlans.LatestByCampaign(ctx, entity.ID)
	if err != nil {
		return PilotAdmissionEvidence{}, fmt.Errorf("%w: load approved routing plan: %v", ErrPilotAdmission, err)
	}
	reservations, err := c.RoutingPlans.Reservations(ctx, plan.ID)
	if err != nil {
		return PilotAdmissionEvidence{}, fmt.Errorf("%w: load capacity reservation: %v", ErrPilotAdmission, err)
	}
	sends, err := c.TestMessages.ListSends(ctx, entity.ID)
	if err != nil {
		return PilotAdmissionEvidence{}, fmt.Errorf("%w: load controlled-test evidence: %v", ErrPilotAdmission, err)
	}
	return EvaluateDayOnePilotAdmission(entity, sends, plan, reservations)
}
