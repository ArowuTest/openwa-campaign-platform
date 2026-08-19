package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/metacloud"
	"campaign-platform/internal/provider"
	"campaign-platform/internal/sender"
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
	LatestByCampaign(context.Context, string) (RoutingPlan, error)
	Reservations(context.Context, string) ([]CapacityReservation, error)
	ActivateReservations(context.Context, string, time.Time) error
	ReleaseReservations(context.Context, string, string, time.Time) error
	PoolExecutionReport(context.Context, string) ([]PoolExecutionReport, error)
}

type CampaignReader interface {
	Get(context.Context, string) (campaign.Campaign, error)
}

type MetaSenderReader interface {
	Get(context.Context, string) (metacloud.Sender, error)
}

type MetaTemplateReader interface {
	GetBinding(context.Context, string) (metacloud.Binding, error)
	FindApproved(context.Context, string, string, string, string) (metacloud.Template, error)
}

type RoutingAdministration struct {
	Store                RoutingPlanStore
	Campaigns            CampaignReader
	ProviderCapabilities *provider.Service
	GatewayPools         *sender.GatewayPoolService
	MetaSenders          MetaSenderReader
	MetaTemplates        MetaTemplateReader
	MetaHealthStaleAfter time.Duration
	Clock                func() time.Time
}

func routingPlanCreationBlocked(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "DISPATCHING", "PAUSED", "COMPLETED", "COMPLETED_WITH_EXCEPTIONS", "CANCELLED":
		return true
	default:
		return false
	}
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
	if routingPlanCreationBlocked(string(entity.Status)) {
		return RoutingPlan{}, ErrRoutingPlanConflict
	}
	if entity.RequestedStartAt == nil || entity.CompletionDeadlineAt == nil || !entity.CompletionDeadlineAt.After(*entity.RequestedStartAt) {
		return RoutingPlan{}, ErrRoutingPlanInvalid
	}
	plan.ApprovedBy = strings.TrimSpace(actor)
	plan.ApprovedAt = s.now()
	if err := prepareDistribution(&plan); err != nil {
		return RoutingPlan{}, err
	}
	if err := plan.Validate(entity.MaximumUniqueRecipients); err != nil {
		return RoutingPlan{}, err
	}
	requestHash, err := plan.computeRequestHash()
	if err != nil {
		return RoutingPlan{}, err
	}
	plan.RequestHash = requestHash
	if err := s.freezeGovernedRoutes(ctx, &plan, entity); err != nil {
		return RoutingPlan{}, err
	}
	ident, err := id.New()
	if err != nil {
		return RoutingPlan{}, err
	}
	plan.ID = ident
	// The store assigns the next campaign-scoped plan version atomically.
	// Client-supplied versions are deliberately ignored to prevent duplicate
	// or out-of-order approvals under concurrent administration.
	plan.Version = 0
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

func (s *RoutingAdministration) freezeGovernedRoutes(ctx context.Context, plan *RoutingPlan, entity campaign.Campaign) error {
	if s.ProviderCapabilities == nil {
		return errors.New("provider capability governance is required")
	}
	start := s.now()
	if entity.RequestedStartAt != nil && entity.RequestedStartAt.After(start) {
		start = entity.RequestedStartAt.UTC()
	}
	end := start
	if entity.CompletionDeadlineAt != nil && entity.CompletionDeadlineAt.After(start) {
		end = entity.CompletionDeadlineAt.UTC().Add(-time.Nanosecond)
	}
	providerRequired := make([]provider.Capability, 0, len(entity.Transport.RequiredCapabilities))
	gatewayRequired := make([]sender.Capability, 0, len(entity.Transport.RequiredCapabilities))
	for _, value := range entity.Transport.RequiredCapabilities {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		providerRequired = append(providerRequired, provider.Capability(value))
		gatewayRequired = append(gatewayRequired, sender.Capability(value))
	}
	for i := range plan.Routes {
		route := &plan.Routes[i]
		required := append([]provider.Capability(nil), providerRequired...)
		if route.Provider == "META" {
			required = append(required, provider.CapabilitySendTemplate)
		}
		definition, err := s.ProviderCapabilities.Require(ctx, route.Provider, provider.ChannelWhatsApp, route.Engine, start, required)
		if err != nil {
			return fmt.Errorf("govern routing plan route %s: %w", route.SenderPoolID, err)
		}
		if end.After(start) {
			atEnd, endErr := s.ProviderCapabilities.Require(ctx, route.Provider, provider.ChannelWhatsApp, route.Engine, end, required)
			if endErr != nil || atEnd.ID != definition.ID || atEnd.Version != definition.Version {
				return fmt.Errorf("govern routing plan route %s: provider definition does not cover the complete campaign window", route.SenderPoolID)
			}
		}
		switch route.Provider {
		case "OPENWA":
			if s.GatewayPools == nil {
				return errors.New("gateway pool governance is required for OpenWA routes")
			}
			pool, poolErr := s.GatewayPools.RequireCapabilities(ctx, route.GatewayPoolID, sender.GatewayProvider(route.Provider), sender.GatewayEngine(route.Engine), gatewayRequired)
			if poolErr != nil {
				return fmt.Errorf("govern routing plan route %s: %w", route.SenderPoolID, poolErr)
			}
			if strings.TrimSpace(pool.AdapterVersion) != strings.TrimSpace(definition.AdapterVersion) {
				return fmt.Errorf("govern routing plan route %s: gateway adapter does not match provider definition", route.SenderPoolID)
			}
			if definition.MinimumGatewayVersion != "" {
				ok, versionErr := provider.VersionAtLeast(pool.AdapterVersion, definition.MinimumGatewayVersion)
				if versionErr != nil {
					return fmt.Errorf("govern routing plan route %s: invalid governed gateway version evidence: %w", route.SenderPoolID, versionErr)
				}
				if !ok {
					return fmt.Errorf("govern routing plan route %s: gateway version is below the provider minimum", route.SenderPoolID)
				}
			}
			route.GatewayPoolVersion = pool.Version
			route.MetaSenderVersion = 0
		case "META":
			if err := s.freezeMetaRoute(ctx, route, entity, start, end); err != nil {
				return fmt.Errorf("govern routing plan route %s: %w", route.SenderPoolID, err)
			}
		default:
			return fmt.Errorf("govern routing plan route %s: unsupported provider", route.SenderPoolID)
		}
		route.ProviderAdapterVersion = definition.AdapterVersion
		route.ProviderDefinitionID = definition.ID
		route.ProviderDefinitionVersion = definition.Version
	}
	return nil
}

func (s *RoutingAdministration) ValidateForExecution(ctx context.Context, plan RoutingPlan, entity campaign.Campaign, at time.Time) error {
	if s == nil || s.ProviderCapabilities == nil {
		return errors.New("provider capability governance is required")
	}
	if err := plan.Validate(entity.MaximumUniqueRecipients); err != nil {
		return fmt.Errorf("routing plan structure is invalid: %w", err)
	}
	providerRequired := make([]provider.Capability, 0, len(entity.Transport.RequiredCapabilities))
	gatewayRequired := make([]sender.Capability, 0, len(entity.Transport.RequiredCapabilities))
	for _, value := range entity.Transport.RequiredCapabilities {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		providerRequired = append(providerRequired, provider.Capability(value))
		gatewayRequired = append(gatewayRequired, sender.Capability(value))
	}
	for _, route := range plan.Routes {
		if route.ProviderDefinitionID == "" || route.ProviderDefinitionVersion <= 0 || route.ProviderAdapterVersion == "" {
			return fmt.Errorf("routing plan route %s lacks frozen provider evidence", route.SenderPoolID)
		}
		definition, err := s.ProviderCapabilities.Get(ctx, route.ProviderDefinitionID)
		if err != nil {
			return fmt.Errorf("validate routing plan route %s: %w", route.SenderPoolID, err)
		}
		windowStart := at.UTC()
		if entity.RequestedStartAt != nil && entity.RequestedStartAt.After(windowStart) {
			windowStart = entity.RequestedStartAt.UTC()
		}
		windowEnd := windowStart
		if entity.CompletionDeadlineAt != nil && entity.CompletionDeadlineAt.After(windowStart) {
			windowEnd = entity.CompletionDeadlineAt.UTC().Add(-time.Nanosecond)
		}
		if definition.Version != route.ProviderDefinitionVersion || definition.Status != provider.StatusActive || definition.Provider != route.Provider || string(definition.Channel) != "WHATSAPP" || definition.Engine != route.Engine || definition.AdapterVersion != route.ProviderAdapterVersion || definition.EffectiveFrom.After(windowStart) || (definition.EffectiveTo != nil && !definition.EffectiveTo.After(windowEnd)) {
			return fmt.Errorf("routing plan route %s provider evidence is no longer valid", route.SenderPoolID)
		}
		required := append([]provider.Capability(nil), providerRequired...)
		if route.Provider == "META" {
			required = append(required, provider.CapabilitySendTemplate)
		}
		available := map[provider.Capability]bool{}
		for _, c := range definition.Capabilities {
			available[c] = true
		}
		for _, c := range required {
			if !available[c] {
				return fmt.Errorf("routing plan route %s lacks provider capability %s", route.SenderPoolID, c)
			}
		}
		switch route.Provider {
		case "OPENWA":
			if s.GatewayPools == nil || route.GatewayPoolVersion <= 0 || route.MetaSenderVersion != 0 {
				return fmt.Errorf("routing plan route %s lacks frozen gateway evidence", route.SenderPoolID)
			}
			pool, poolErr := s.GatewayPools.Get(ctx, route.GatewayPoolID)
			if poolErr != nil {
				return fmt.Errorf("validate routing plan route %s: %w", route.SenderPoolID, poolErr)
			}
			if pool.Version != route.GatewayPoolVersion || pool.Status != sender.GatewayPoolActive || string(pool.Provider) != route.Provider || string(pool.Engine) != route.Engine || pool.AdapterVersion != route.ProviderAdapterVersion {
				return fmt.Errorf("routing plan route %s gateway evidence is no longer valid", route.SenderPoolID)
			}
			caps := map[sender.Capability]bool{}
			for _, c := range pool.Capabilities {
				caps[c] = true
			}
			for _, c := range gatewayRequired {
				if !caps[c] {
					return fmt.Errorf("routing plan route %s lacks gateway capability %s", route.SenderPoolID, c)
				}
			}
			if definition.MinimumGatewayVersion != "" {
				ok, versionErr := provider.VersionAtLeast(pool.AdapterVersion, definition.MinimumGatewayVersion)
				if versionErr != nil {
					return fmt.Errorf("routing plan route %s has invalid governed gateway version evidence: %w", route.SenderPoolID, versionErr)
				}
				if !ok {
					return fmt.Errorf("routing plan route %s gateway version is below the provider minimum", route.SenderPoolID)
				}
			}
		case "META":
			if err := s.validateMetaRoute(ctx, route, entity, windowStart, windowEnd, at.UTC()); err != nil {
				return fmt.Errorf("validate routing plan route %s: %w", route.SenderPoolID, err)
			}
		default:
			return fmt.Errorf("routing plan route %s has unsupported provider", route.SenderPoolID)
		}
	}
	return nil
}

func (s *RoutingAdministration) Get(ctx context.Context, id string) (RoutingPlan, error) {
	return s.Store.Get(ctx, id)
}
func (s *RoutingAdministration) ListByCampaign(ctx context.Context, campaignID string) ([]RoutingPlan, error) {
	return s.Store.ListByCampaign(ctx, campaignID)
}
func (s *RoutingAdministration) LatestByCampaign(ctx context.Context, campaignID string) (RoutingPlan, error) {
	return s.Store.LatestByCampaign(ctx, campaignID)
}
func (s *RoutingAdministration) Reservations(ctx context.Context, planID string) ([]CapacityReservation, error) {
	return s.Store.Reservations(ctx, planID)
}
func (s *RoutingAdministration) Activate(ctx context.Context, planID string) error {
	return s.Store.ActivateReservations(ctx, planID, s.now())
}
func (s *RoutingAdministration) PoolReport(ctx context.Context, planID string) ([]PoolExecutionReport, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(planID) == "" {
		return nil, ErrRoutingPlanInvalid
	}
	return s.Store.PoolExecutionReport(ctx, strings.TrimSpace(planID))
}
func (s *RoutingAdministration) Release(ctx context.Context, planID, actor string) error {
	planID = strings.TrimSpace(planID)
	actor = strings.TrimSpace(actor)
	if s == nil || s.Store == nil || s.Campaigns == nil || planID == "" || actor == "" {
		return ErrRoutingPlanInvalid
	}
	plan, err := s.Store.Get(ctx, planID)
	if err != nil {
		return err
	}
	entity, err := s.Campaigns.Get(ctx, plan.CampaignID)
	if err != nil {
		return err
	}
	if entity.Status == campaign.StatusDispatching || entity.Status == campaign.StatusPaused {
		return ErrRoutingPlanConflict
	}
	return s.Store.ReleaseReservations(ctx, plan.ID, actor, s.now())
}

type PoolExecutionReport struct {
	RoutingPlanID             string `json:"routingPlanId"`
	SenderPoolID              string `json:"senderPoolId"`
	GatewayPoolID             string `json:"gatewayPoolId"`
	Provider                  string `json:"provider"`
	Engine                    string `json:"engine"`
	ProviderAdapterVersion    string `json:"providerAdapterVersion"`
	ProviderDefinitionID      string `json:"providerDefinitionId"`
	ProviderDefinitionVersion int64  `json:"providerDefinitionVersion"`
	GatewayPoolVersion        int64  `json:"gatewayPoolVersion"`
	MaximumRecipients         int64  `json:"maximumRecipients"`
	ReservedMessagesPerMinute int    `json:"reservedMessagesPerMinute"`
	ReservedHourlyUnits       int64  `json:"reservedHourlyUnits"`
	ReservedDailyUnits        int64  `json:"reservedDailyUnits"`
	ShardCount                int64  `json:"shardCount"`
	RecipientCount            int64  `json:"recipientCount"`
	QueuedCount               int64  `json:"queuedCount"`
	SubmittedCount            int64  `json:"submittedCount"`
	SentCount                 int64  `json:"sentCount"`
	DeliveredCount            int64  `json:"deliveredCount"`
	ReadCount                 int64  `json:"readCount"`
	FailedCount               int64  `json:"failedCount"`
	UnknownCount              int64  `json:"unknownCount"`
	TerminalCount             int64  `json:"terminalCount"`
}
