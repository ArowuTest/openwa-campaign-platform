package operations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/shared/id"
)

type AlertMetric string

const (
	AlertQueueDepth              AlertMetric = "QUEUE_DEPTH"
	AlertUnknownOutcomes         AlertMetric = "UNKNOWN_OUTCOMES"
	AlertStaleWorkerNodes        AlertMetric = "STALE_WORKER_NODES"
	AlertOpenIncidents           AlertMetric = "OPEN_INCIDENTS"
	AlertCriticalIncidents       AlertMetric = "CRITICAL_INCIDENTS"
	AlertUnavailableGatewayNodes AlertMetric = "UNAVAILABLE_GATEWAY_NODES"
	AlertUnhealthySenderSessions AlertMetric = "UNHEALTHY_SENDER_SESSIONS"
	AlertCampaignsAtRisk         AlertMetric = "CAMPAIGNS_AT_RISK"
	AlertCapacityShortfallPools  AlertMetric = "CAPACITY_SHORTFALL_POOLS"
	AlertReconciliationBacklog   AlertMetric = "RECONCILIATION_BACKLOG"
)

type Comparison string

const (
	ComparisonGT  Comparison = "GT"
	ComparisonGTE Comparison = "GTE"
	ComparisonLT  Comparison = "LT"
	ComparisonLTE Comparison = "LTE"
	ComparisonEQ  Comparison = "EQ"
)

type AlertPolicyStatus string

const (
	AlertPolicyDraft    AlertPolicyStatus = "DRAFT"
	AlertPolicyPending  AlertPolicyStatus = "PENDING_APPROVAL"
	AlertPolicyActive   AlertPolicyStatus = "ACTIVE"
	AlertPolicyRejected AlertPolicyStatus = "REJECTED"
	AlertPolicyRetired  AlertPolicyStatus = "RETIRED"
)

type EscalationStep struct {
	AfterSeconds int    `json:"afterSeconds"`
	Target       string `json:"target"`
}
type AlertPolicy struct {
	ID                     string            `json:"id"`
	Name                   string            `json:"name"`
	Metric                 AlertMetric       `json:"metric"`
	Comparison             Comparison        `json:"comparison"`
	Threshold              float64           `json:"threshold"`
	Severity               Severity          `json:"severity"`
	ConsecutiveEvaluations int               `json:"consecutiveEvaluations"`
	CooldownSeconds        int               `json:"cooldownSeconds"`
	AutoIncident           bool              `json:"autoIncident"`
	EscalationSteps        []EscalationStep  `json:"escalationSteps"`
	Status                 AlertPolicyStatus `json:"status"`
	EffectiveFrom          time.Time         `json:"effectiveFrom"`
	EffectiveTo            *time.Time        `json:"effectiveTo,omitempty"`
	CreatedBy              string            `json:"createdBy"`
	SubmittedBy            string            `json:"submittedBy,omitempty"`
	ApprovedBy             string            `json:"approvedBy,omitempty"`
	Reason                 string            `json:"reason"`
	Version                int64             `json:"version"`
	CurrentBreachCount     int               `json:"currentBreachCount"`
	LastEvaluatedAt        *time.Time        `json:"lastEvaluatedAt,omitempty"`
	CooldownUntil          *time.Time        `json:"cooldownUntil,omitempty"`
	CreatedAt              time.Time         `json:"createdAt"`
	UpdatedAt              time.Time         `json:"updatedAt"`
}
type AlertPolicyEvent struct {
	ID         string         `json:"id"`
	PolicyID   string         `json:"policyId"`
	EventType  string         `json:"eventType"`
	Version    int64          `json:"version"`
	ActorID    string         `json:"actorId"`
	Reason     string         `json:"reason"`
	Evidence   map[string]any `json:"evidence,omitempty"`
	OccurredAt time.Time      `json:"occurredAt"`
}
type AlertStatus string

const (
	AlertActive       AlertStatus = "ACTIVE"
	AlertAcknowledged AlertStatus = "ACKNOWLEDGED"
	AlertResolved     AlertStatus = "RESOLVED"
)

type Alert struct {
	ID               string      `json:"id"`
	PolicyID         string      `json:"policyId"`
	Status           AlertStatus `json:"status"`
	Severity         Severity    `json:"severity"`
	ObservedValue    float64     `json:"observedValue"`
	Threshold        float64     `json:"threshold"`
	OccurrenceCount  int         `json:"occurrenceCount"`
	FirstTriggeredAt time.Time   `json:"firstTriggeredAt"`
	LastObservedAt   time.Time   `json:"lastObservedAt"`
	AcknowledgedBy   string      `json:"acknowledgedBy,omitempty"`
	AcknowledgedAt   *time.Time  `json:"acknowledgedAt,omitempty"`
	ResolvedAt       *time.Time  `json:"resolvedAt,omitempty"`
	LinkedIncidentID string      `json:"linkedIncidentId,omitempty"`
	EscalationIndex  int         `json:"escalationIndex"`
	NextEscalationAt *time.Time  `json:"nextEscalationAt,omitempty"`
	LeaseOwner       string      `json:"leaseOwner,omitempty"`
	LeaseVersion     int64       `json:"leaseVersion"`
	LeaseExpiresAt   *time.Time  `json:"leaseExpiresAt,omitempty"`
	Version          int64       `json:"version"`
	UpdatedAt        time.Time   `json:"updatedAt"`
}
type AlertEvent struct {
	ID            string         `json:"id"`
	AlertID       string         `json:"alertId"`
	EventType     string         `json:"eventType"`
	ActorID       string         `json:"actorId,omitempty"`
	ObservedValue *float64       `json:"observedValue,omitempty"`
	Detail        string         `json:"detail"`
	Evidence      map[string]any `json:"evidence,omitempty"`
	OccurredAt    time.Time      `json:"occurredAt"`
}
type IncidentEvent struct {
	ID         string         `json:"id"`
	IncidentID string         `json:"incidentId"`
	EventType  string         `json:"eventType"`
	ActorID    string         `json:"actorId,omitempty"`
	Detail     string         `json:"detail"`
	Evidence   map[string]any `json:"evidence,omitempty"`
	OccurredAt time.Time      `json:"occurredAt"`
}
type Notification struct {
	ID            string         `json:"id"`
	AlertID       string         `json:"alertId,omitempty"`
	IncidentID    string         `json:"incidentId,omitempty"`
	Channel       string         `json:"channel"`
	Target        string         `json:"target"`
	Status        string         `json:"status"`
	Payload       map[string]any `json:"payload"`
	AttemptCount  int            `json:"attemptCount"`
	DeliveredAt   *time.Time     `json:"deliveredAt,omitempty"`
	LastErrorCode string         `json:"lastErrorCode,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type AlertStore interface {
	ListAlertPolicies(context.Context, AlertPolicyStatus, int) ([]AlertPolicy, error)
	GetAlertPolicy(context.Context, string) (AlertPolicy, error)
	CreateAlertPolicy(context.Context, AlertPolicy, AlertPolicyEvent) (AlertPolicy, error)
	UpdateAlertPolicy(context.Context, AlertPolicy, int64, AlertPolicyEvent) (AlertPolicy, error)
	ActivateAlertPolicy(context.Context, AlertPolicy, int64, AlertPolicyEvent) (AlertPolicy, error)
	ListAlertPolicyEvents(context.Context, string, int) ([]AlertPolicyEvent, error)
	ListActiveAlertPolicies(context.Context, time.Time) ([]AlertPolicy, error)
	ObserveAlertPolicy(context.Context, AlertPolicy, float64, bool, time.Time) (*Alert, error)
	ListAlerts(context.Context, AlertStatus, Severity, int) ([]Alert, error)
	GetAlert(context.Context, string) (Alert, error)
	AcknowledgeAlert(context.Context, string, int64, string, string, time.Time) (Alert, error)
	ListAlertEvents(context.Context, string, int) ([]AlertEvent, error)
	ClaimEscalations(context.Context, string, time.Time, time.Duration, int) ([]Alert, error)
	EscalateAlert(context.Context, Alert, EscalationStep, time.Time) (Alert, error)
	CreateIncidentForAlert(context.Context, Alert, AlertPolicy, time.Time) (string, error)
	AddIncidentEvent(context.Context, IncidentEvent) error
	ListIncidentEvents(context.Context, string, int) ([]IncidentEvent, error)
	ListNotifications(context.Context, string, int) ([]Notification, error)
}

type AlertAdministration struct {
	Store AlertStore
	Clock func() time.Time
}

func (a *AlertAdministration) now() time.Time {
	if a != nil && a.Clock != nil {
		return a.Clock().UTC()
	}
	return time.Now().UTC()
}
func validMetric(v AlertMetric) bool {
	switch v {
	case AlertQueueDepth, AlertUnknownOutcomes, AlertStaleWorkerNodes, AlertOpenIncidents, AlertCriticalIncidents,
		AlertUnavailableGatewayNodes, AlertUnhealthySenderSessions, AlertCampaignsAtRisk, AlertCapacityShortfallPools, AlertReconciliationBacklog:
		return true
	}
	return false
}
func validComparison(v Comparison) bool {
	switch v {
	case ComparisonGT, ComparisonGTE, ComparisonLT, ComparisonLTE, ComparisonEQ:
		return true
	}
	return false
}
func validateSteps(v []EscalationStep) error {
	last := 0
	for _, x := range v {
		if x.AfterSeconds <= last || x.AfterSeconds > 7*24*3600 || strings.TrimSpace(x.Target) == "" {
			return ErrInvalid
		}
		last = x.AfterSeconds
	}
	return nil
}
func (a *AlertAdministration) CreatePolicy(ctx context.Context, v AlertPolicy, actor, reason string) (AlertPolicy, error) {
	if a == nil || a.Store == nil {
		return AlertPolicy{}, errors.New("alert store is required")
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" || actor == "" || len(reason) < 8 || !validMetric(v.Metric) || !validComparison(v.Comparison) || validateSteps(v.EscalationSteps) != nil {
		return AlertPolicy{}, ErrInvalid
	}
	switch v.Severity {
	case SeverityInfo, SeverityWarning, SeverityCritical:
	default:
		return AlertPolicy{}, ErrInvalid
	}
	if v.ConsecutiveEvaluations <= 0 || v.ConsecutiveEvaluations > 1000 || v.CooldownSeconds < 0 || v.CooldownSeconds > 86400 {
		return AlertPolicy{}, ErrInvalid
	}
	now := a.now()
	if v.EffectiveFrom.IsZero() {
		v.EffectiveFrom = now
	} else {
		v.EffectiveFrom = v.EffectiveFrom.UTC()
	}
	if v.EffectiveTo != nil {
		x := v.EffectiveTo.UTC()
		v.EffectiveTo = &x
		if !x.After(v.EffectiveFrom) {
			return AlertPolicy{}, ErrInvalid
		}
	}
	ident, err := id.New()
	if err != nil {
		return AlertPolicy{}, err
	}
	v.ID = ident
	v.Status = AlertPolicyDraft
	v.CreatedBy = actor
	v.Reason = reason
	v.Version = 1
	v.CreatedAt = now
	v.UpdatedAt = now
	ev := AlertPolicyEvent{PolicyID: v.ID, EventType: "CREATED", Version: 1, ActorID: actor, Reason: reason, Evidence: map[string]any{"metric": v.Metric, "threshold": v.Threshold}, OccurredAt: now}
	return a.Store.CreateAlertPolicy(ctx, v, ev)
}
func (a *AlertAdministration) SubmitPolicy(ctx context.Context, key string, expected int64, actor, reason string) (AlertPolicy, error) {
	v, err := a.Store.GetAlertPolicy(ctx, key)
	if err != nil {
		return AlertPolicy{}, err
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if v.Version != expected || v.Status != AlertPolicyDraft || actor == "" || len(reason) < 8 {
		return AlertPolicy{}, ErrConflict
	}
	v.Status = AlertPolicyPending
	v.SubmittedBy = actor
	v.Reason = reason
	v.Version++
	v.UpdatedAt = a.now()
	return a.Store.UpdateAlertPolicy(ctx, v, expected, AlertPolicyEvent{PolicyID: v.ID, EventType: "SUBMITTED", Version: v.Version, ActorID: actor, Reason: reason, OccurredAt: v.UpdatedAt})
}
func (a *AlertAdministration) DecidePolicy(ctx context.Context, key string, expected int64, approve bool, actor, reason string) (AlertPolicy, error) {
	v, err := a.Store.GetAlertPolicy(ctx, key)
	if err != nil {
		return AlertPolicy{}, err
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if v.Version != expected || v.Status != AlertPolicyPending || actor == "" || len(reason) < 8 {
		return AlertPolicy{}, ErrConflict
	}
	if actor == v.CreatedBy || actor == v.SubmittedBy {
		return AlertPolicy{}, errors.New("alert policy approver must be independent")
	}
	v.ApprovedBy = actor
	v.Reason = reason
	v.Version++
	v.UpdatedAt = a.now()
	eventType := "REJECTED"
	v.Status = AlertPolicyRejected
	if approve {
		v.Status = AlertPolicyActive
		eventType = "ACTIVATED"
	}
	ev := AlertPolicyEvent{PolicyID: v.ID, EventType: eventType, Version: v.Version, ActorID: actor, Reason: reason, Evidence: map[string]any{"effectiveFrom": v.EffectiveFrom, "effectiveTo": v.EffectiveTo}, OccurredAt: v.UpdatedAt}
	if approve {
		return a.Store.ActivateAlertPolicy(ctx, v, expected, ev)
	}
	return a.Store.UpdateAlertPolicy(ctx, v, expected, ev)
}
func (a *AlertAdministration) RetirePolicy(ctx context.Context, key string, expected int64, actor, reason string) (AlertPolicy, error) {
	v, err := a.Store.GetAlertPolicy(ctx, key)
	if err != nil {
		return AlertPolicy{}, err
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if v.Version != expected || v.Status != AlertPolicyActive || actor == "" || len(reason) < 8 {
		return AlertPolicy{}, ErrConflict
	}
	now := a.now()
	v.Status = AlertPolicyRetired
	if v.EffectiveTo == nil || v.EffectiveTo.After(now) {
		v.EffectiveTo = &now
	}
	v.Reason = reason
	v.Version++
	v.UpdatedAt = now
	return a.Store.UpdateAlertPolicy(ctx, v, expected, AlertPolicyEvent{PolicyID: v.ID, EventType: "RETIRED", Version: v.Version, ActorID: actor, Reason: reason, OccurredAt: now})
}
func (a *AlertAdministration) ListPolicies(ctx context.Context, status AlertPolicyStatus, limit int) ([]AlertPolicy, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return a.Store.ListAlertPolicies(ctx, status, limit)
}
func (a *AlertAdministration) PolicyEvents(ctx context.Context, key string, limit int) ([]AlertPolicyEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	return a.Store.ListAlertPolicyEvents(ctx, key, limit)
}
func (a *AlertAdministration) ListAlerts(ctx context.Context, status AlertStatus, severity Severity, limit int) ([]Alert, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return a.Store.ListAlerts(ctx, status, severity, limit)
}
func (a *AlertAdministration) Acknowledge(ctx context.Context, key string, expected int64, actor, reason string) (Alert, error) {
	if strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 8 {
		return Alert{}, ErrInvalid
	}
	return a.Store.AcknowledgeAlert(ctx, key, expected, actor, reason, a.now())
}

type DashboardReader interface {
	Dashboard(context.Context) (Dashboard, error)
}
type AlertEvaluator struct {
	Store     AlertStore
	Dashboard DashboardReader
	WorkerID  string
	Lease     time.Duration
	Clock     func() time.Time
}

func (e *AlertEvaluator) now() time.Time {
	if e != nil && e.Clock != nil {
		return e.Clock().UTC()
	}
	return time.Now().UTC()
}
func metricValue(d Dashboard, m AlertMetric) float64 {
	switch m {
	case AlertQueueDepth:
		return float64(d.QueueDepth)
	case AlertUnknownOutcomes:
		return float64(d.UnknownOutcomes)
	case AlertStaleWorkerNodes:
		return float64(d.StaleWorkerNodes)
	case AlertOpenIncidents:
		return float64(d.OpenIncidents)
	case AlertCriticalIncidents:
		return float64(d.CriticalIncidents)
	case AlertUnavailableGatewayNodes:
		return float64(d.UnavailableGatewayNodes)
	case AlertUnhealthySenderSessions:
		return float64(d.UnhealthySenderSessions)
	case AlertCampaignsAtRisk:
		return float64(d.CampaignsAtRisk)
	case AlertCapacityShortfallPools:
		return float64(d.CapacityShortfallPools)
	case AlertReconciliationBacklog:
		return float64(d.ReconciliationBacklog)
	}
	return 0
}
func breached(v, t float64, c Comparison) bool {
	switch c {
	case ComparisonGT:
		return v > t
	case ComparisonGTE:
		return v >= t
	case ComparisonLT:
		return v < t
	case ComparisonLTE:
		return v <= t
	case ComparisonEQ:
		return v == t
	}
	return false
}
func (e *AlertEvaluator) Evaluate(ctx context.Context) (int, error) {
	if e == nil || e.Store == nil || e.Dashboard == nil {
		return 0, errors.New("alert evaluator dependencies are required")
	}
	now := e.now()
	d, err := e.Dashboard.Dashboard(ctx)
	if err != nil {
		return 0, err
	}
	policies, err := e.Store.ListActiveAlertPolicies(ctx, now)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, p := range policies {
		v := metricValue(d, p.Metric)
		alert, observeErr := e.Store.ObserveAlertPolicy(ctx, p, v, breached(v, p.Threshold, p.Comparison), now)
		if observeErr != nil {
			return changed, observeErr
		}
		if alert != nil {
			changed++
			if p.AutoIncident && alert.LinkedIncidentID == "" {
				if _, createErr := e.Store.CreateIncidentForAlert(ctx, *alert, p, now); createErr != nil {
					return changed, createErr
				}
			}
		}
	}
	return changed, nil
}
func (e *AlertEvaluator) Escalate(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	now := e.now()
	workerID := strings.TrimSpace(e.WorkerID)
	if workerID == "" {
		workerID = "interactive-alert-evaluator"
	}
	lease := e.Lease
	if lease <= 0 {
		lease = time.Minute
	}
	alerts, err := e.Store.ClaimEscalations(ctx, workerID, now, lease, limit)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, alert := range alerts {
		p, er := e.Store.GetAlertPolicy(ctx, alert.PolicyID)
		if er != nil {
			return count, er
		}
		if alert.EscalationIndex >= len(p.EscalationSteps) {
			continue
		}
		if _, er = e.Store.EscalateAlert(ctx, alert, p.EscalationSteps[alert.EscalationIndex], now); er != nil {
			return count, er
		}
		count++
	}
	return count, nil
}
func (e *AlertEvaluator) Run(ctx context.Context, poll time.Duration) error {
	if poll <= 0 {
		poll = time.Minute
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		if _, err := e.Evaluate(ctx); err != nil && ctx.Err() == nil {
			return err
		}
		if _, err := e.Escalate(ctx, 100); err != nil && ctx.Err() == nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (a Alert) String() string { return fmt.Sprintf("%s:%s", a.PolicyID, a.Status) }
