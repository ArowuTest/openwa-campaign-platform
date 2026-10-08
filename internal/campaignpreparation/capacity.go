package campaignpreparation

import (
	"campaign-platform/internal/campaign"
	"campaign-platform/internal/execution"
	"context"
	"errors"
	"math"
	"time"
)

// MemoryCapacityReader deliberately does not invent healthy sessions or rate.
type MemoryCapacityReader struct{}

func (MemoryCapacityReader) RouteCapacity(context.Context, execution.PoolRoute, time.Time) (execution.PoolCapacity, error) {
	return execution.PoolCapacity{}, ErrUnavailable
}

func (s *Service) capacityCheck(ctx context.Context, c campaign.Campaign, basis int64, basisKind string, now, start, end time.Time, scheduleValid bool) (ReadinessCheck, *CapacityPreview) {
	out := newCheck("capacity", "CAPACITY_GROSS_PROJECTION_VALID", "Current healthy route measurements support a gross advisory projection.", "Review live session and node health. Competing reservation-window feasibility requires independent review.")
	route := routeFor(c)
	measured, err := s.Capacity.RouteCapacity(ctx, route, now)
	if err != nil {
		code, msg := "CAPACITY_UNAVAILABLE", "Current measured route capacity is unavailable."
		if errors.Is(err, ErrRouteIdentityUnavailable) {
			code = "CAPACITY_ROUTE_IDENTITY_UNAVAILABLE"
			msg = "Canonical route identity could not be measured."
		}
		if _, ok := s.Capacity.(MemoryCapacityReader); ok {
			msg = "Measured session and node capacity is unavailable in this development runtime."
		}
		out.fail("UNAVAILABLE", code, msg)
		return out, nil
	}
	if measured.SenderPoolID != route.SenderPoolID || measured.GatewayPoolID != route.GatewayPoolID {
		out.fail("UNAVAILABLE", "CAPACITY_ROUTE_IDENTITY_UNAVAILABLE", "Capacity does not match the exact saved route.")
		return out, nil
	}
	if measured.HealthySessions < 0 || measured.HealthyNodes < 0 || measured.MinimumHealthyNodes < 0 || measured.AvailableMessagesPerMinute < 0 || int64(measured.AvailableMessagesPerMinute) > MaxSafeInteger || measured.AvailableHourlyUnits < 0 || measured.AvailableHourlyUnits > MaxSafeInteger || measured.AvailableDailyUnits < 0 || measured.AvailableDailyUnits > MaxSafeInteger {
		out.fail("UNAVAILABLE", "CAPACITY_UNAVAILABLE", "Current capacity measurements are invalid.")
		return out, nil
	}
	p := &CapacityPreview{SenderPoolID: route.SenderPoolID, GatewayPoolID: route.GatewayPoolID, HealthySessions: measured.HealthySessions, HealthyNodes: measured.HealthyNodes, MinimumHealthyNodes: measured.MinimumHealthyNodes, AvailableMessagesPerMinute: measured.AvailableMessagesPerMinute, AvailableHourlyUnits: measured.AvailableHourlyUnits, AvailableDailyUnits: measured.AvailableDailyUnits, SafetyMarginPercent: s.SafetyMarginPercent, BasisRecipientCount: basis, Basis: basisKind, MeasurementAsOf: now, Classification: "ADVISORY_ONLY", Reasons: []string{ReservationReason}}
	fail := func(status, code, msg string) (ReadinessCheck, *CapacityPreview) {
		out.fail(status, code, msg)
		p.Classification = "BLOCKED"
		p.Reasons = append(p.Reasons, code)
		return out, p
	}
	if measured.HealthySessions < 1 || measured.MinimumHealthyNodes < 1 || measured.HealthyNodes < measured.MinimumHealthyNodes || measured.AvailableMessagesPerMinute < 1 || measured.AvailableHourlyUnits < 1 || measured.AvailableDailyUnits < 1 {
		return fail("UNAVAILABLE", "CAPACITY_UNAVAILABLE", "Fresh route health and positive capacity measurements are required.")
	}
	// Check arithmetic before any float-to-int or duration conversion.
	margin := 1 - float64(s.SafetyMarginPercent)/100
	effectiveRate := float64(measured.AvailableMessagesPerMinute) * margin
	if basis < 1 || basis > MaxSafeInteger || effectiveRate <= 0 || math.IsNaN(effectiveRate) || math.IsInf(effectiveRate, 0) {
		return fail("BLOCKED", "CAPACITY_PROJECTION_UNREPRESENTABLE", "The advisory projection cannot be represented safely.")
	}
	minutes := math.Ceil(float64(basis) / effectiveRate)
	const maxMinutes = int64(math.MaxInt64) / int64(time.Minute)
	if math.IsNaN(minutes) || math.IsInf(minutes, 0) || minutes < 1 || minutes > float64(maxMinutes) {
		return fail("BLOCKED", "CAPACITY_PROJECTION_UNREPRESENTABLE", "The advisory projection exceeds the supported time range.")
	}
	if !scheduleValid || !end.After(start) {
		return fail("BLOCKED", "CAPACITY_WINDOW_INVALID", "A valid future saved window is required for projection.")
	}
	if c.QuietHoursStart != "" || c.QuietHoursEnd != "" {
		return fail("BLOCKED", "CAPACITY_WINDOW_NOT_MODELED", "Quiet hours are not modeled by this gross projection.")
	}
	duration := time.Duration(int64(minutes)) * time.Minute
	completion := start.Add(duration).UTC()
	if completion.Before(start) || completion.Year() > 9999 {
		return fail("BLOCKED", "CAPACITY_PROJECTION_UNREPRESENTABLE", "The advisory projection exceeds the supported date range.")
	}
	required := float64(basis) / end.Sub(start).Minutes()
	if math.IsNaN(required) || math.IsInf(required, 0) || required <= 0 {
		return fail("BLOCKED", "CAPACITY_PROJECTION_UNREPRESENTABLE", "The advisory rate cannot be represented safely.")
	}
	p.RequiredMessagesPerMinute = &required
	// Gross rates retain the existing static reserve deduction. No dynamic hold
	// is added to or subtracted from these measurements.
	days := math.Ceil(end.Sub(start).Hours() / 24)
	available := math.Floor(float64(measured.AvailableDailyUnits)*margin) * days
	if completion.After(end) || float64(basis) > available {
		return fail("BLOCKED", "CAPACITY_INSUFFICIENT", "Current gross measurements do not fit the saved window.")
	}
	p.ProjectedCompletionAt = &completion
	out.Evidence = []EvidenceReference{{Kind: "senderPool", ID: route.SenderPoolID, ObservedAt: &now}, {Kind: "gatewayPool", ID: route.GatewayPoolID, ObservedAt: &now}}
	return out, p
}
