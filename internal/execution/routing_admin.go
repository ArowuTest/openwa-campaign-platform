package execution

import (
	"context"
	"errors"
	"strings"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/shared/id"
)

var (
	ErrRoutingPlanNotFound = errors.New("campaign routing plan not found")
	ErrRoutingPlanConflict = errors.New("campaign routing plan conflict")
	ErrCapacityOverbooked  = errors.New("sender pool capacity reservation would be overbooked")
)

type CapacityReservation struct {
	ID                        string    `json:"id"`
	CampaignID                string    `json:"campaignId"`
	RoutingPlanID             string    `json:"routingPlanId"`
	SenderPoolID              string    `json:"senderPoolId"`
	ReservationStart          time.Time `json:"reservationStart"`
	ReservationEnd            time.Time `json:"reservationEnd"`
	ReservedMessagesPerMinute int       `json:"reservedMessagesPerMinute"`
	ReservedHourlyUnits       int64     `json:"reservedHourlyUnits"`
	ReservedDailyUnits        int64     `json:"reservedDailyUnits"`
	Status                    string    `json:"status"`
	FencingVersion            int64     `json:"fencingVersion"`
	CreatedAt                 time.Time `json:"createdAt"`
	UpdatedAt                 time.Time `json:"updatedAt"`
}

type RoutingPlanStore interface {
	Create(context.Context, RoutingPlan, []CapacityReservation) (RoutingPlan, error)
	Get(context.Context, string) (RoutingPlan, error)
	ListByCampaign(context.Context, string) ([]RoutingPlan, error)
	Reservations(context.Context, string) ([]CapacityReservation, error)
	ActivateReservations(context.Context, string, time.Time) error
	ReleaseReservations(context.Context, string, string, time.Time) error
}

type CampaignReader interface {
	Get(context.Context, string) (campaign.Campaign, error)
}

type RoutingAdministration struct {
	Store     RoutingPlanStore
	Campaigns CampaignReader
	Clock     func() time.Time
}

func (s *RoutingAdministration) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func (s *RoutingAdministration) CreateApproved(ctx context.Context, plan RoutingPlan, actor string) (RoutingPlan, error) {
	if s == nil || s.Store == nil || s.Campaigns == nil || strings.TrimSpace(actor) == "" {
		return RoutingPlan{}, ErrRoutingPlanInvalid
	}
	entity, err := s.Campaigns.Get(ctx, plan.CampaignID)
	if err != nil {
		return RoutingPlan{}, err
	}
	if entity.RequestedStartAt == nil || entity.CompletionDeadlineAt == nil || !entity.CompletionDeadlineAt.After(*entity.RequestedStartAt) {
		return RoutingPlan{}, ErrRoutingPlanInvalid
	}
	plan.ApprovedBy = strings.TrimSpace(actor)
	plan.ApprovedAt = s.now()
	if err := plan.Validate(entity.MaximumUniqueRecipients); err != nil {
		return RoutingPlan{}, err
	}
	ident, err := id.New()
	if err != nil {
		return RoutingPlan{}, err
	}
	plan.ID = ident
	if plan.Version < 1 {
		plan.Version = 1
	}
	reservations := make([]CapacityReservation, 0, len(plan.Routes))
	for _, route := range plan.Routes {
		rid, err := id.New()
		if err != nil {
			return RoutingPlan{}, err
		}
		reservations = append(reservations, CapacityReservation{ID: rid, CampaignID: plan.CampaignID, RoutingPlanID: plan.ID, SenderPoolID: route.SenderPoolID, ReservationStart: entity.RequestedStartAt.UTC(), ReservationEnd: entity.CompletionDeadlineAt.UTC(), ReservedMessagesPerMinute: route.ReservedMessagesPerMinute, ReservedHourlyUnits: route.ReservedHourlyUnits, ReservedDailyUnits: route.ReservedDailyUnits, Status: "HELD", FencingVersion: 1, CreatedAt: plan.ApprovedAt, UpdatedAt: plan.ApprovedAt})
	}
	return s.Store.Create(ctx, plan, reservations)
}
func (s *RoutingAdministration) Get(ctx context.Context, id string) (RoutingPlan, error) {
	return s.Store.Get(ctx, id)
}
func (s *RoutingAdministration) ListByCampaign(ctx context.Context, campaignID string) ([]RoutingPlan, error) {
	return s.Store.ListByCampaign(ctx, campaignID)
}
func (s *RoutingAdministration) Reservations(ctx context.Context, planID string) ([]CapacityReservation, error) {
	return s.Store.Reservations(ctx, planID)
}
func (s *RoutingAdministration) Activate(ctx context.Context, planID string) error {
	return s.Store.ActivateReservations(ctx, planID, s.now())
}
func (s *RoutingAdministration) Release(ctx context.Context, planID, actor string) error {
	if strings.TrimSpace(actor) == "" {
		return ErrRoutingPlanInvalid
	}
	return s.Store.ReleaseReservations(ctx, planID, actor, s.now())
}
