package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

var ErrRoutingPlanInvalid = errors.New("campaign routing plan is invalid")

type DistributionMode string

const (
	DistributionAuto     DistributionMode = "AUTO"
	DistributionWeighted DistributionMode = "WEIGHTED"
)

type PoolRoute struct {
	SenderPoolID              string `json:"senderPoolId"`
	GatewayPoolID             string `json:"gatewayPoolId,omitempty"`
	MetaSenderID              string `json:"metaSenderId,omitempty"`
	Provider                  string `json:"provider"`
	Engine                    string `json:"engine"`
	ProviderAdapterVersion    string `json:"providerAdapterVersion,omitempty"`
	ProviderDefinitionID      string `json:"providerDefinitionId,omitempty"`
	ProviderDefinitionVersion int64  `json:"providerDefinitionVersion,omitempty"`
	GatewayPoolVersion        int64  `json:"gatewayPoolVersion,omitempty"`
	MetaSenderVersion         int64  `json:"metaSenderVersion,omitempty"`
	AllocationWeight          int    `json:"allocationWeight"`
	MaximumRecipients         int64  `json:"maximumRecipients"`
	ReservedMessagesPerMinute int    `json:"reservedMessagesPerMinute"`
	ReservedHourlyUnits       int64  `json:"reservedHourlyUnits"`
	ReservedDailyUnits        int64  `json:"reservedDailyUnits"`
	AllowReallocationIn       bool   `json:"allowReallocationIn"`
	AllowReallocationOut      bool   `json:"allowReallocationOut"`
}

type RoutingPlan struct {
	ID                      string           `json:"id"`
	CampaignID              string           `json:"campaignId"`
	Version                 int64            `json:"version"`
	Routes                  []PoolRoute      `json:"routes"`
	DistributionMode        DistributionMode `json:"distributionMode"`
	RoutingPolicyVersion    string           `json:"routingPolicyVersion"`
	CapacityEvidenceVersion string           `json:"capacityEvidenceVersion"`
	PacingPolicyVersion     string           `json:"pacingPolicyVersion"`
	FallbackMode            string           `json:"fallbackMode"`
	ApprovedAt              time.Time        `json:"approvedAt"`
	ApprovedBy              string           `json:"approvedBy"`
	IdempotencyKey          string           `json:"idempotencyKey"`
	RequestHash             string           `json:"-"`
}

func (p *RoutingPlan) Validate(maximumRecipients int64) error {
	p.CampaignID = strings.TrimSpace(p.CampaignID)
	p.RoutingPolicyVersion = strings.TrimSpace(p.RoutingPolicyVersion)
	p.CapacityEvidenceVersion = strings.TrimSpace(p.CapacityEvidenceVersion)
	p.PacingPolicyVersion = strings.TrimSpace(p.PacingPolicyVersion)
	p.ApprovedBy = strings.TrimSpace(p.ApprovedBy)
	p.IdempotencyKey = strings.TrimSpace(p.IdempotencyKey)
	if p.DistributionMode == "" {
		p.DistributionMode = DistributionWeighted
	}
	if p.DistributionMode != DistributionAuto && p.DistributionMode != DistributionWeighted {
		return ErrRoutingPlanInvalid
	}
	if p.CampaignID == "" || p.RoutingPolicyVersion == "" || p.CapacityEvidenceVersion == "" || p.PacingPolicyVersion == "" || p.ApprovedBy == "" || p.IdempotencyKey == "" || len(p.IdempotencyKey) > 200 || p.FallbackMode != "NONE" || len(p.Routes) == 0 || maximumRecipients < 1 {
		return ErrRoutingPlanInvalid
	}
	seen := map[string]struct{}{}
	var maxTotal int64
	for i := range p.Routes {
		r := &p.Routes[i]
		r.SenderPoolID = strings.TrimSpace(r.SenderPoolID)
		r.GatewayPoolID = strings.TrimSpace(r.GatewayPoolID)
		r.MetaSenderID = strings.TrimSpace(r.MetaSenderID)
		r.Provider = strings.ToUpper(strings.TrimSpace(r.Provider))
		r.Engine = strings.ToUpper(strings.TrimSpace(r.Engine))
		endpointValid := false
		switch r.Provider {
		case "OPENWA":
			endpointValid = r.GatewayPoolID != "" && r.MetaSenderID == "" && (r.Engine == "WHATSAPP_WEB_JS" || r.Engine == "BAILEYS")
		case "META":
			endpointValid = r.GatewayPoolID == "" && r.MetaSenderID != "" && r.Engine == "CLOUD_API"
		}
		if r.SenderPoolID == "" || !endpointValid || r.AllocationWeight < 1 || r.AllocationWeight > 10000 || r.MaximumRecipients < 1 || r.ReservedMessagesPerMinute < 1 || r.ReservedHourlyUnits < 1 || r.ReservedDailyUnits < r.ReservedHourlyUnits {
			return ErrRoutingPlanInvalid
		}
		if _, ok := seen[r.SenderPoolID]; ok {
			return ErrRoutingPlanInvalid
		}
		seen[r.SenderPoolID] = struct{}{}
		maxTotal += r.MaximumRecipients
	}
	if maxTotal < maximumRecipients {
		return ErrRoutingPlanInvalid
	}
	sort.Slice(p.Routes, func(i, j int) bool { return p.Routes[i].SenderPoolID < p.Routes[j].SenderPoolID })
	return nil
}

func (p RoutingPlan) computeRequestHash() (string, error) {
	type routeIntent struct {
		SenderPoolID              string `json:"senderPoolId"`
		GatewayPoolID             string `json:"gatewayPoolId,omitempty"`
		MetaSenderID              string `json:"metaSenderId,omitempty"`
		Provider                  string `json:"provider"`
		Engine                    string `json:"engine"`
		AllocationWeight          int    `json:"allocationWeight"`
		MaximumRecipients         int64  `json:"maximumRecipients"`
		ReservedMessagesPerMinute int    `json:"reservedMessagesPerMinute"`
		ReservedHourlyUnits       int64  `json:"reservedHourlyUnits"`
		ReservedDailyUnits        int64  `json:"reservedDailyUnits"`
		AllowReallocationIn       bool   `json:"allowReallocationIn"`
		AllowReallocationOut      bool   `json:"allowReallocationOut"`
	}
	routes := make([]routeIntent, 0, len(p.Routes))
	for _, route := range p.Routes {
		routes = append(routes, routeIntent{
			SenderPoolID: route.SenderPoolID, GatewayPoolID: route.GatewayPoolID, MetaSenderID: route.MetaSenderID,
			Provider: route.Provider, Engine: route.Engine, AllocationWeight: route.AllocationWeight,
			MaximumRecipients: route.MaximumRecipients, ReservedMessagesPerMinute: route.ReservedMessagesPerMinute,
			ReservedHourlyUnits: route.ReservedHourlyUnits, ReservedDailyUnits: route.ReservedDailyUnits,
			AllowReallocationIn: route.AllowReallocationIn, AllowReallocationOut: route.AllowReallocationOut,
		})
	}
	payload := struct {
		CampaignID              string           `json:"campaignId"`
		Routes                  []routeIntent    `json:"routes"`
		DistributionMode        DistributionMode `json:"distributionMode"`
		RoutingPolicyVersion    string           `json:"routingPolicyVersion"`
		CapacityEvidenceVersion string           `json:"capacityEvidenceVersion"`
		PacingPolicyVersion     string           `json:"pacingPolicyVersion"`
		FallbackMode            string           `json:"fallbackMode"`
		ApprovedBy              string           `json:"approvedBy"`
	}{CampaignID: p.CampaignID, Routes: routes, DistributionMode: p.DistributionMode, RoutingPolicyVersion: p.RoutingPolicyVersion, CapacityEvidenceVersion: p.CapacityEvidenceVersion, PacingPolicyVersion: p.PacingPolicyVersion, FallbackMode: p.FallbackMode, ApprovedBy: p.ApprovedBy}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// AssignShard deterministically maps a shard to an approved route using weighted rendezvous-style slots.
func (p RoutingPlan) AssignShard(shardOrdinal int64) (PoolRoute, error) {
	if shardOrdinal < 0 || len(p.Routes) == 0 {
		return PoolRoute{}, ErrRoutingPlanInvalid
	}
	total := 0
	for _, r := range p.Routes {
		total += r.AllocationWeight
	}
	if total < 1 {
		return PoolRoute{}, ErrRoutingPlanInvalid
	}
	slot := int(shardOrdinal % int64(total))
	running := 0
	for _, r := range p.Routes {
		running += r.AllocationWeight
		if slot < running {
			return r, nil
		}
	}
	return PoolRoute{}, ErrRoutingPlanInvalid
}

type PoolCapacity struct {
	SenderPoolID               string
	GatewayPoolID              string
	AvailableMessagesPerMinute int
	AvailableHourlyUnits       int64
	AvailableDailyUnits        int64
	HealthySessions            int
	HealthyNodes               int
	MinimumHealthyNodes        int
}
type MultiPoolAdmissionInput struct {
	CampaignID          string
	RemainingRecipients int64
	EffectiveStart      time.Time
	Deadline            time.Time
	SafetyMarginPercent int
	Plan                RoutingPlan
	Capacities          []PoolCapacity
	Now                 time.Time
}
type MultiPoolAdmission struct {
	Decision                   AdmissionDecision `json:"decision"`
	RequiredMessagesPerMinute  float64           `json:"requiredMessagesPerMinute"`
	EffectiveMessagesPerMinute int               `json:"effectiveMessagesPerMinute"`
	EffectiveHourlyUnits       int64             `json:"effectiveHourlyUnits"`
	EffectiveDailyUnits        int64             `json:"effectiveDailyUnits"`
	ForecastCompletionAt       *time.Time        `json:"forecastCompletionAt,omitempty"`
	Reasons                    []string          `json:"reasons"`
	EvaluatedAt                time.Time         `json:"evaluatedAt"`
}

func EvaluateMultiPoolAdmission(in MultiPoolAdmissionInput) (MultiPoolAdmission, error) {
	if in.Now.IsZero() {
		in.Now = time.Now().UTC()
	}
	if err := in.Plan.Validate(in.RemainingRecipients); err != nil {
		return MultiPoolAdmission{}, err
	}
	if in.RemainingRecipients < 0 || in.SafetyMarginPercent < 0 || in.SafetyMarginPercent > 90 {
		return MultiPoolAdmission{}, ErrRoutingPlanInvalid
	}
	start := in.EffectiveStart
	if start.Before(in.Now) {
		start = in.Now
	}
	if !in.Deadline.After(start) {
		return MultiPoolAdmission{}, ErrRoutingPlanInvalid
	}
	caps := map[string]PoolCapacity{}
	for _, c := range in.Capacities {
		caps[c.SenderPoolID+"\x00"+c.GatewayPoolID] = c
	}
	rawMPM := 0
	var rawHour, rawDay int64
	reasons := []string{}
	unavailableFrozenRoute := false
	for _, r := range in.Plan.Routes {
		c, ok := caps[r.SenderPoolID+"\x00"+r.GatewayPoolID]
		if !ok || c.HealthySessions < 1 || c.HealthyNodes < c.MinimumHealthyNodes ||
			c.AvailableMessagesPerMinute < r.ReservedMessagesPerMinute ||
			c.AvailableHourlyUnits < r.ReservedHourlyUnits ||
			c.AvailableDailyUnits < r.ReservedDailyUnits {
			unavailableFrozenRoute = true
			reasons = append(reasons, "POOL_UNAVAILABLE:"+r.SenderPoolID)
			continue
		}
		rawMPM += min(r.ReservedMessagesPerMinute, c.AvailableMessagesPerMinute)
		rawHour += min64(r.ReservedHourlyUnits, c.AvailableHourlyUnits)
		rawDay += min64(r.ReservedDailyUnits, c.AvailableDailyUnits)
	}
	factor := float64(100-in.SafetyMarginPercent) / 100
	effMPM := int(float64(rawMPM) * factor)
	effHour := int64(float64(rawHour) * factor)
	effDay := int64(float64(rawDay) * factor)
	mins := in.Deadline.Sub(start).Minutes()
	required := 0.0
	if mins > 0 {
		required = float64(in.RemainingRecipients) / mins
	}
	out := MultiPoolAdmission{Decision: DecisionAdmit, RequiredMessagesPerMinute: required, EffectiveMessagesPerMinute: effMPM, EffectiveHourlyUnits: effHour, EffectiveDailyUnits: effDay, Reasons: reasons, EvaluatedAt: in.Now.UTC()}
	if effMPM < 1 || effHour < 1 || effDay < 1 {
		out.Decision = DecisionReject
		out.Reasons = append(out.Reasons, "NO_EFFECTIVE_THROUGHPUT")
		return out, nil
	}
	if unavailableFrozenRoute {
		out.Decision = DecisionHold
		out.Reasons = append(out.Reasons, "FROZEN_ROUTE_UNAVAILABLE")
		return out, nil
	}

	// A campaign window can span multiple hourly and daily allowance periods.
	// Evaluate the total work possible inside the window under every governed
	// limit rather than requiring one daily allowance to cover the whole
	// campaign or considering only the nominal messages-per-minute rate.
	minuteCapacity := int64(float64(effMPM) * mins)
	hourWindows := int64(math.Ceil(in.Deadline.Sub(start).Hours()))
	if hourWindows < 1 {
		hourWindows = 1
	}
	dayWindows := int64(math.Ceil(in.Deadline.Sub(start).Hours() / 24))
	if dayWindows < 1 {
		dayWindows = 1
	}
	hourCapacity := saturatingMultiply(effHour, hourWindows)
	dayCapacity := saturatingMultiply(effDay, dayWindows)
	windowCapacity := min64(minuteCapacity, min64(hourCapacity, dayCapacity))

	forecastDuration := governedCompletionDuration(in.RemainingRecipients, effMPM, effHour, effDay)
	if forecastDuration <= 0 {
		out.Decision = DecisionReject
		out.Reasons = append(out.Reasons, "NO_EFFECTIVE_THROUGHPUT")
		return out, nil
	}
	forecast := start.Add(forecastDuration)
	out.ForecastCompletionAt = &forecast

	if minuteCapacity < in.RemainingRecipients {
		out.Reasons = append(out.Reasons, "MINUTE_THROUGHPUT_SHORTFALL")
	}
	if hourCapacity < in.RemainingRecipients {
		out.Reasons = append(out.Reasons, "HOURLY_ALLOWANCE_SHORTFALL")
	}
	if dayCapacity < in.RemainingRecipients {
		out.Reasons = append(out.Reasons, "DAILY_ALLOWANCE_SHORTFALL")
	}
	if windowCapacity < in.RemainingRecipients || float64(effMPM) < required || forecast.After(in.Deadline) {
		out.Decision = DecisionHold
		out.Reasons = append(out.Reasons, "DEADLINE_CAPACITY_SHORTFALL")
	}
	return out, nil
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func saturatingMultiply(value, multiplier int64) int64 {
	if value <= 0 || multiplier <= 0 {
		return 0
	}
	if value > math.MaxInt64/multiplier {
		return math.MaxInt64
	}
	return value * multiplier
}

// governedCompletionDuration returns a conservative earliest completion time
// under the pacing rate plus resettable hourly and daily allowances. It assumes
// allowance windows begin at the campaign start; production scheduling may use
// stricter calendar-boundary calculations when the deployment policy requires it.
func governedCompletionDuration(recipients int64, messagesPerMinute int, hourlyAllowance, dailyAllowance int64) time.Duration {
	if recipients <= 0 {
		return time.Nanosecond
	}
	if messagesPerMinute <= 0 || hourlyAllowance <= 0 || dailyAllowance <= 0 {
		return 0
	}
	minuteBound := durationForUnits(recipients, int64(messagesPerMinute), time.Minute)
	hourBound := durationAcrossAllowanceWindows(recipients, hourlyAllowance, int64(messagesPerMinute), time.Hour)
	dayBound := durationAcrossAllowanceWindows(recipients, dailyAllowance, int64(messagesPerMinute), 24*time.Hour)
	if hourBound > minuteBound {
		minuteBound = hourBound
	}
	if dayBound > minuteBound {
		minuteBound = dayBound
	}
	return minuteBound
}

func durationForUnits(units, rate int64, interval time.Duration) time.Duration {
	if units <= 0 {
		return time.Nanosecond
	}
	if rate <= 0 {
		return 0
	}
	return time.Duration(math.Ceil(float64(units) / float64(rate) * float64(interval)))
}

func durationAcrossAllowanceWindows(units, allowance, messagesPerMinute int64, window time.Duration) time.Duration {
	if units <= allowance {
		return durationForUnits(units, messagesPerMinute, time.Minute)
	}
	fullWaits := (units - 1) / allowance
	remaining := units - fullWaits*allowance
	if fullWaits > int64(math.MaxInt64/int64(window)) {
		return time.Duration(math.MaxInt64)
	}
	wait := time.Duration(fullWaits) * window
	last := durationForUnits(remaining, messagesPerMinute, time.Minute)
	if last > window {
		last = window
	}
	if wait > time.Duration(math.MaxInt64)-last {
		return time.Duration(math.MaxInt64)
	}
	return wait + last
}
