package operations

import (
	"context"
	"sort"
	"sync"
	"time"

	"campaign-platform/internal/shared/id"
)

type MemoryAlertStore struct {
	mu             sync.Mutex
	policies       map[string]AlertPolicy
	policyEvents   map[string][]AlertPolicyEvent
	alerts         map[string]Alert
	alertEvents    map[string][]AlertEvent
	incidentEvents map[string][]IncidentEvent
	incidents      map[string]Incident
	notifications  map[string]Notification
	IncidentRepo   Repository
}

func NewMemoryAlertStore() *MemoryAlertStore {
	return &MemoryAlertStore{policies: map[string]AlertPolicy{}, policyEvents: map[string][]AlertPolicyEvent{}, alerts: map[string]Alert{}, alertEvents: map[string][]AlertEvent{}, incidentEvents: map[string][]IncidentEvent{}, incidents: map[string]Incident{}, notifications: map[string]Notification{}}
}
func (m *MemoryAlertStore) addPolicyEvent(v AlertPolicyEvent) error {
	if v.ID == "" {
		var err error
		v.ID, err = id.New()
		if err != nil {
			return err
		}
	}
	m.policyEvents[v.PolicyID] = append(m.policyEvents[v.PolicyID], v)
	return nil
}
func (m *MemoryAlertStore) addAlertEvent(v AlertEvent) error {
	if v.ID == "" {
		var err error
		v.ID, err = id.New()
		if err != nil {
			return err
		}
	}
	m.alertEvents[v.AlertID] = append(m.alertEvents[v.AlertID], v)
	return nil
}
func (m *MemoryAlertStore) ListAlertPolicies(_ context.Context, status AlertPolicyStatus, limit int) ([]AlertPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []AlertPolicy{}
	for _, v := range m.policies {
		if status == "" || v.Status == status {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (m *MemoryAlertStore) ListAlertPolicyPage(_ context.Context, status AlertPolicyStatus, limit int, before *time.Time, beforeID string) ([]AlertPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]AlertPolicy, 0, len(m.policies))
	for _, v := range m.policies {
		if status != "" && v.Status != status {
			continue
		}
		if before != nil && !(v.CreatedAt.Before(*before) || (v.CreatedAt.Equal(*before) && v.ID < beforeID)) {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
}

func (m *MemoryAlertStore) GetAlertPolicy(_ context.Context, key string) (AlertPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.policies[key]
	if !ok {
		return AlertPolicy{}, ErrNotFound
	}
	return v, nil
}
func (m *MemoryAlertStore) CreateAlertPolicy(_ context.Context, v AlertPolicy, e AlertPolicyEvent) (AlertPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.policies[v.ID]; ok {
		return AlertPolicy{}, ErrConflict
	}
	if err := m.addPolicyEvent(e); err != nil {
		return AlertPolicy{}, err
	}
	m.policies[v.ID] = v
	return v, nil
}
func (m *MemoryAlertStore) UpdateAlertPolicy(_ context.Context, v AlertPolicy, expected int64, e AlertPolicyEvent) (AlertPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.policies[v.ID]
	if !ok {
		return AlertPolicy{}, ErrNotFound
	}
	if cur.Version != expected {
		return AlertPolicy{}, ErrConflict
	}
	if err := m.addPolicyEvent(e); err != nil {
		return AlertPolicy{}, err
	}
	m.policies[v.ID] = v
	return v, nil
}
func (m *MemoryAlertStore) ActivateAlertPolicy(_ context.Context, v AlertPolicy, expected int64, e AlertPolicyEvent) (AlertPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.policies[v.ID]
	if !ok {
		return AlertPolicy{}, ErrNotFound
	}
	if cur.Version != expected {
		return AlertPolicy{}, ErrConflict
	}
	for key, p := range m.policies {
		if key == v.ID || p.Status != AlertPolicyActive || p.Metric != v.Metric ||
			!periodsOverlap(p.EffectiveFrom, p.EffectiveTo, v.EffectiveFrom, v.EffectiveTo) {
			continue
		}
		// A replacement may take effect only after the currently approved
		// definition began. Equal-start and backdated replacements fail closed.
		if !v.EffectiveFrom.After(p.EffectiveFrom) {
			return AlertPolicy{}, ErrConflict
		}
		x := v.EffectiveFrom
		p.EffectiveTo = &x
		// Keep the prior policy ACTIVE until its effective boundary. Marking it
		// RETIRED now would incorrectly disable it before the future replacement.
		p.Version++
		p.UpdatedAt = v.UpdatedAt
		m.policies[key] = p
		superseded := AlertPolicyEvent{PolicyID: p.ID, EventType: "SUPERSEDED", Version: p.Version, ActorID: e.ActorID, Reason: e.Reason, Evidence: map[string]any{"supersededById": v.ID, "effectiveTo": x}, OccurredAt: e.OccurredAt}
		if err := m.addPolicyEvent(superseded); err != nil {
			return AlertPolicy{}, err
		}
	}
	if err := m.addPolicyEvent(e); err != nil {
		return AlertPolicy{}, err
	}
	m.policies[v.ID] = v
	return v, nil
}
func (m *MemoryAlertStore) ListAlertPolicyEvents(ctx context.Context, key string, limit int) ([]AlertPolicyEvent, error) {
	return m.ListAlertPolicyEventPage(ctx, key, limit, nil, "")
}

func (m *MemoryAlertStore) ListAlertPolicyEventPage(_ context.Context, key string, limit int, before *time.Time, beforeID string) ([]AlertPolicyEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := append([]AlertPolicyEvent(nil), m.policyEvents[key]...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].OccurredAt.Equal(items[j].OccurredAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].OccurredAt.After(items[j].OccurredAt)
	})
	out := make([]AlertPolicyEvent, 0, limit)
	for _, item := range items {
		if before != nil && (item.OccurredAt.After(*before) || item.OccurredAt.Equal(*before) && item.ID >= beforeID) {
			continue
		}
		out = append(out, item)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
func (m *MemoryAlertStore) ListActiveAlertPolicies(_ context.Context, now time.Time) ([]AlertPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []AlertPolicy{}
	for _, v := range m.policies {
		if v.Status == AlertPolicyActive && !v.EffectiveFrom.After(now) && (v.EffectiveTo == nil || v.EffectiveTo.After(now)) {
			out = append(out, v)
		}
	}
	return out, nil
}
func (m *MemoryAlertStore) ObserveAlertPolicy(_ context.Context, p AlertPolicy, value float64, isBreach bool, now time.Time) (*Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.policies[p.ID]
	if !ok {
		return nil, ErrNotFound
	}
	cur.LastEvaluatedAt = &now
	if !isBreach {
		cur.CurrentBreachCount = 0
		for key, a := range m.alerts {
			if a.PolicyID == p.ID && (a.Status == AlertActive || a.Status == AlertAcknowledged) {
				a.Status = AlertResolved
				a.ResolvedAt = &now
				a.UpdatedAt = now
				a.Version++
				m.alerts[key] = a
				if err := m.addAlertEvent(AlertEvent{AlertID: a.ID, EventType: "RESOLVED", ObservedValue: &value, Detail: "metric recovered", OccurredAt: now}); err != nil {
					return nil, err
				}
				cool := now.Add(time.Duration(p.CooldownSeconds) * time.Second)
				cur.CooldownUntil = &cool
			}
		}
		m.policies[p.ID] = cur
		return nil, nil
	}
	cur.CurrentBreachCount++
	m.policies[p.ID] = cur
	if cur.CurrentBreachCount < cur.ConsecutiveEvaluations {
		return nil, nil
	}
	for key, a := range m.alerts {
		if a.PolicyID == p.ID && (a.Status == AlertActive || a.Status == AlertAcknowledged) {
			a.ObservedValue = value
			a.OccurrenceCount++
			a.LastObservedAt = now
			a.UpdatedAt = now
			a.Version++
			m.alerts[key] = a
			if err := m.addAlertEvent(AlertEvent{AlertID: a.ID, EventType: "OBSERVED", ObservedValue: &value, Detail: "threshold remains breached", OccurredAt: now}); err != nil {
				return nil, err
			}
			copy := a
			return &copy, nil
		}
	}
	if cur.CooldownUntil != nil && cur.CooldownUntil.After(now) {
		return nil, nil
	}
	ident, err := id.New()
	if err != nil {
		return nil, err
	}
	a := Alert{ID: ident, PolicyID: p.ID, Status: AlertActive, Severity: p.Severity, ObservedValue: value, Threshold: p.Threshold, OccurrenceCount: 1, FirstTriggeredAt: now, LastObservedAt: now, Version: 1, UpdatedAt: now}
	if len(p.EscalationSteps) > 0 {
		x := now.Add(time.Duration(p.EscalationSteps[0].AfterSeconds) * time.Second)
		a.NextEscalationAt = &x
	}
	if err := m.addAlertEvent(AlertEvent{AlertID: a.ID, EventType: "TRIGGERED", ObservedValue: &value, Detail: "governed threshold breached", OccurredAt: now}); err != nil {
		return nil, err
	}
	m.alerts[a.ID] = a
	return &a, nil
}
func (m *MemoryAlertStore) ListAlerts(_ context.Context, status AlertStatus, severity Severity, limit int) ([]Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Alert{}
	for _, v := range m.alerts {
		if (status == "" || v.Status == status) && (severity == "" || v.Severity == severity) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastObservedAt.After(out[j].LastObservedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (m *MemoryAlertStore) GetAlert(_ context.Context, key string) (Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.alerts[key]
	if !ok {
		return Alert{}, ErrNotFound
	}
	return v, nil
}
func (m *MemoryAlertStore) AcknowledgeAlert(_ context.Context, key string, expected int64, actor, reason string, now time.Time) (Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.alerts[key]
	if !ok {
		return Alert{}, ErrNotFound
	}
	if v.Version != expected || v.Status != AlertActive {
		return Alert{}, ErrConflict
	}
	v.Status = AlertAcknowledged
	v.AcknowledgedBy = actor
	v.AcknowledgedAt = &now
	v.NextEscalationAt = nil
	v.LeaseOwner = ""
	v.LeaseExpiresAt = nil
	v.Version++
	v.UpdatedAt = now
	if err := m.addAlertEvent(AlertEvent{AlertID: key, EventType: "ACKNOWLEDGED", ActorID: actor, Detail: reason, OccurredAt: now}); err != nil {
		return Alert{}, err
	}
	m.alerts[key] = v
	return v, nil
}
func (m *MemoryAlertStore) ListAlertEvents(_ context.Context, key string, limit int) ([]AlertEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]AlertEvent(nil), m.alertEvents[key]...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (m *MemoryAlertStore) ListAlertEventPage(_ context.Context, key string, limit int, after *time.Time, afterID string) ([]AlertEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := append([]AlertEvent(nil), m.alertEvents[key]...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].OccurredAt.Equal(items[j].OccurredAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].OccurredAt.Before(items[j].OccurredAt)
	})
	out := make([]AlertEvent, 0, limit)
	for _, item := range items {
		if after != nil && (item.OccurredAt.Before(*after) || (item.OccurredAt.Equal(*after) && item.ID <= afterID)) {
			continue
		}
		out = append(out, item)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (m *MemoryAlertStore) ClaimEscalations(_ context.Context, worker string, now time.Time, lease time.Duration, limit int) ([]Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Alert{}
	for key, v := range m.alerts {
		if len(out) >= limit {
			break
		}
		leaseAvailable := v.LeaseExpiresAt == nil || !v.LeaseExpiresAt.After(now)
		if v.Status == AlertActive && v.NextEscalationAt != nil && !v.NextEscalationAt.After(now) && leaseAvailable {
			expires := now.Add(lease)
			v.LeaseOwner = worker
			v.LeaseVersion++
			v.LeaseExpiresAt = &expires
			m.alerts[key] = v
			out = append(out, v)
		}
	}
	return out, nil
}
func (m *MemoryAlertStore) EscalateAlert(_ context.Context, v Alert, step EscalationStep, now time.Time) (Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.alerts[v.ID]
	if !ok {
		return Alert{}, ErrNotFound
	}
	if cur.Version != v.Version || cur.Status != AlertActive || cur.LeaseOwner != v.LeaseOwner || cur.LeaseVersion != v.LeaseVersion || cur.LeaseExpiresAt == nil || !cur.LeaseExpiresAt.After(now) {
		return Alert{}, ErrConflict
	}
	cur.EscalationIndex++
	cur.LeaseOwner = ""
	cur.LeaseExpiresAt = nil
	cur.Version++
	cur.UpdatedAt = now
	policy := m.policies[cur.PolicyID]
	if cur.EscalationIndex < len(policy.EscalationSteps) {
		x := cur.FirstTriggeredAt.Add(time.Duration(policy.EscalationSteps[cur.EscalationIndex].AfterSeconds) * time.Second)
		cur.NextEscalationAt = &x
	} else {
		cur.NextEscalationAt = nil
	}
	if err := m.addAlertEvent(AlertEvent{AlertID: cur.ID, EventType: "ESCALATED", Detail: "escalated to " + step.Target, Evidence: map[string]any{"target": step.Target, "index": cur.EscalationIndex}, OccurredAt: now}); err != nil {
		return Alert{}, err
	}
	nid, err := id.New()
	if err != nil {
		return Alert{}, err
	}
	m.alerts[cur.ID] = cur
	deliveredAt := now
	m.notifications[nid] = Notification{ID: nid, AlertID: cur.ID, IncidentID: cur.LinkedIncidentID, Channel: "PORTAL", Target: step.Target, Status: "DELIVERED", Payload: map[string]any{"alertId": cur.ID, "severity": cur.Severity}, AttemptCount: 1, DeliveredAt: &deliveredAt, CreatedAt: now, UpdatedAt: now}
	return cur, nil
}
func (m *MemoryAlertStore) CreateIncidentForAlert(ctx context.Context, a Alert, p AlertPolicy, now time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.alerts[a.ID]
	if cur.LinkedIncidentID != "" {
		return cur.LinkedIncidentID, nil
	}
	iid, err := id.New()
	if err != nil {
		return "", err
	}
	inc := Incident{ID: iid, Category: "AUTOMATED_ALERT", Severity: p.Severity, Status: IncidentOpen, Summary: "Operational alert: " + p.Name, Detail: "Automatically created from alert " + a.ID, CreatedAt: now, UpdatedAt: now, Version: 1}
	if m.IncidentRepo != nil {
		if _, err := m.IncidentRepo.CreateIncident(ctx, inc); err != nil {
			return "", err
		}
	}
	m.incidents[iid] = inc
	cur.LinkedIncidentID = iid
	cur.Version++
	cur.UpdatedAt = now
	if err := m.addAlertEvent(AlertEvent{AlertID: a.ID, EventType: "INCIDENT_CREATED", Detail: "incident created", Evidence: map[string]any{"incidentId": iid}, OccurredAt: now}); err != nil {
		return "", err
	}
	createdID, err := id.New()
	if err != nil {
		return "", err
	}
	linkedID, err := id.New()
	if err != nil {
		return "", err
	}
	m.alerts[a.ID] = cur
	m.incidentEvents[iid] = append(m.incidentEvents[iid], IncidentEvent{ID: createdID, IncidentID: iid, EventType: "CREATED", Detail: inc.Summary, OccurredAt: now}, IncidentEvent{ID: linkedID, IncidentID: iid, EventType: "ALERT_LINKED", Detail: "linked to alert " + a.ID, OccurredAt: now})
	return iid, nil
}
func (m *MemoryAlertStore) AddIncidentEvent(_ context.Context, v IncidentEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v.ID == "" {
		var err error
		v.ID, err = id.New()
		if err != nil {
			return err
		}
	}
	m.incidentEvents[v.IncidentID] = append(m.incidentEvents[v.IncidentID], v)
	return nil
}
func (m *MemoryAlertStore) ListIncidentEvents(_ context.Context, key string, limit int) ([]IncidentEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]IncidentEvent(nil), m.incidentEvents[key]...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (m *MemoryAlertStore) ListNotifications(_ context.Context, status string, limit int) ([]Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Notification{}
	for _, v := range m.notifications {
		if status == "" || v.Status == status {
			out = append(out, v)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryAlertStore) ListAlertPage(_ context.Context, status AlertStatus, severity Severity, limit int, before *time.Time, beforeID string) ([]Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]Alert, 0, len(m.alerts))
	for _, alert := range m.alerts {
		if status != "" && alert.Status != status {
			continue
		}
		if severity != "" && alert.Severity != severity {
			continue
		}
		if before != nil && !(alert.LastObservedAt.Before(*before) || (alert.LastObservedAt.Equal(*before) && alert.ID < beforeID)) {
			continue
		}
		items = append(items, alert)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].LastObservedAt.Equal(items[j].LastObservedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].LastObservedAt.After(items[j].LastObservedAt)
	})
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (m *MemoryAlertStore) ListNotificationPage(_ context.Context, status string, limit int, before *time.Time, beforeID string) ([]Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]Notification, 0, len(m.notifications))
	for _, notification := range m.notifications {
		if status != "" && notification.Status != status {
			continue
		}
		if before != nil && !(notification.CreatedAt.Before(*before) || (notification.CreatedAt.Equal(*before) && notification.ID < beforeID)) {
			continue
		}
		items = append(items, notification)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
