package execution

import (
	"errors"
	"sort"
	"strings"
	"time"
)

var ErrRoutingPlanInvalid = errors.New("campaign routing plan is invalid")

type PoolRoute struct {
	SenderPoolID              string `json:"senderPoolId"`
	GatewayPoolID             string `json:"gatewayPoolId"`
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

type RoutingPlan struct {
	ID                      string      `json:"id"`
	CampaignID              string      `json:"campaignId"`
	Version                 int64       `json:"version"`
	Routes                  []PoolRoute `json:"routes"`
	RoutingPolicyVersion    string      `json:"routingPolicyVersion"`
	CapacityEvidenceVersion string      `json:"capacityEvidenceVersion"`
	PacingPolicyVersion     string      `json:"pacingPolicyVersion"`
	FallbackMode            string      `json:"fallbackMode"`
	ApprovedAt              time.Time   `json:"approvedAt"`
	ApprovedBy              string      `json:"approvedBy"`
}

func (p *RoutingPlan) Validate(maximumRecipients int64) error {
	p.CampaignID = strings.TrimSpace(p.CampaignID)
	p.RoutingPolicyVersion = strings.TrimSpace(p.RoutingPolicyVersion)
	p.CapacityEvidenceVersion = strings.TrimSpace(p.CapacityEvidenceVersion)
	p.PacingPolicyVersion = strings.TrimSpace(p.PacingPolicyVersion)
	p.ApprovedBy = strings.TrimSpace(p.ApprovedBy)
	if p.CampaignID == "" || p.RoutingPolicyVersion == "" || p.CapacityEvidenceVersion == "" || p.PacingPolicyVersion == "" || p.ApprovedBy == "" || p.FallbackMode != "NONE" || len(p.Routes) == 0 || maximumRecipients < 1 {
		return ErrRoutingPlanInvalid
	}
	seen := map[string]struct{}{}
	var maxTotal int64
	var daily int64
	for i := range p.Routes {
		r := &p.Routes[i]
		r.SenderPoolID = strings.TrimSpace(r.SenderPoolID)
		r.GatewayPoolID = strings.TrimSpace(r.GatewayPoolID)
		r.Provider = strings.ToUpper(strings.TrimSpace(r.Provider))
		r.Engine = strings.ToUpper(strings.TrimSpace(r.Engine))
		if r.SenderPoolID == "" || r.GatewayPoolID == "" || r.Provider != "OPENWA" || (r.Engine != "WHATSAPP_WEB_JS" && r.Engine != "BAILEYS") || r.AllocationWeight < 1 || r.AllocationWeight > 10000 || r.MaximumRecipients < 1 || r.ReservedMessagesPerMinute < 1 || r.ReservedHourlyUnits < 1 || r.ReservedDailyUnits < r.ReservedHourlyUnits {
			return ErrRoutingPlanInvalid
		}
		if _, ok := seen[r.SenderPoolID]; ok {
			return ErrRoutingPlanInvalid
		}
		seen[r.SenderPoolID] = struct{}{}
		maxTotal += r.MaximumRecipients
		daily += r.ReservedDailyUnits
	}
	if maxTotal < maximumRecipients || daily < maximumRecipients {
		return ErrRoutingPlanInvalid
	}
	sort.Slice(p.Routes, func(i, j int) bool { return p.Routes[i].SenderPoolID < p.Routes[j].SenderPoolID })
	return nil
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
	AvailableMessagesPerMinute int
	AvailableHourlyUnits       int64
	AvailableDailyUnits        int64
	HealthySessions            int
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
		caps[c.SenderPoolID] = c
	}
	rawMPM := 0
	var rawHour, rawDay int64
	reasons := []string{}
	for _, r := range in.Plan.Routes {
		c, ok := caps[r.SenderPoolID]
		if !ok || c.HealthySessions < 1 {
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
	if effMPM < 1 || effHour < 1 || effDay < in.RemainingRecipients {
		out.Decision = DecisionReject
		if effMPM < 1 {
			out.Reasons = append(out.Reasons, "NO_EFFECTIVE_THROUGHPUT")
		}
		if effDay < in.RemainingRecipients {
			out.Reasons = append(out.Reasons, "INSUFFICIENT_DAILY_CAPACITY")
		}
		return out, nil
	}
	if float64(effMPM) < required {
		out.Decision = DecisionHold
		out.Reasons = append(out.Reasons, "DEADLINE_CAPACITY_SHORTFALL")
	}
	forecast := start.Add(time.Duration(float64(in.RemainingRecipients) / float64(effMPM) * float64(time.Minute)))
	out.ForecastCompletionAt = &forecast
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
