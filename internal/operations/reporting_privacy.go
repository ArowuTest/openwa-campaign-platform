package operations

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/audit"
	"campaign-platform/internal/shared/id"
)

type ReportingPrivacyPolicyStatus string

const (
	ReportingPrivacyDraft    ReportingPrivacyPolicyStatus = "DRAFT"
	ReportingPrivacyPending  ReportingPrivacyPolicyStatus = "PENDING_APPROVAL"
	ReportingPrivacyActive   ReportingPrivacyPolicyStatus = "ACTIVE"
	ReportingPrivacyRejected ReportingPrivacyPolicyStatus = "REJECTED"
	ReportingPrivacyRetired  ReportingPrivacyPolicyStatus = "RETIRED"
)

type ReportingPrivacyPolicy struct {
	ID                string                       `json:"id"`
	OrganisationID    string                       `json:"organisationId,omitempty"`
	Status            ReportingPrivacyPolicyStatus `json:"status"`
	MinimumCohortSize int                          `json:"minimumCohortSize"`
	SuppressionLabel  string                       `json:"suppressionLabel"`
	ApplyGeography    bool                         `json:"applyGeography"`
	ApplyDemographics bool                         `json:"applyDemographics"`
	ApplyAttributes   bool                         `json:"applyAttributes"`
	EffectiveFrom     time.Time                    `json:"effectiveFrom"`
	EffectiveTo       *time.Time                   `json:"effectiveTo,omitempty"`
	CreatedBy         string                       `json:"createdBy"`
	SubmittedBy       string                       `json:"submittedBy,omitempty"`
	ApprovedBy        string                       `json:"approvedBy,omitempty"`
	Reason            string                       `json:"reason"`
	Version           int64                        `json:"version"`
	CreatedAt         time.Time                    `json:"createdAt"`
	UpdatedAt         time.Time                    `json:"updatedAt"`
}

type ReportingPrivacyEvent struct {
	ID            string    `json:"id"`
	PolicyID      string    `json:"policyId"`
	EventType     string    `json:"eventType"`
	ActorID       string    `json:"actorId"`
	Reason        string    `json:"reason,omitempty"`
	PolicyVersion int64     `json:"policyVersion"`
	OccurredAt    time.Time `json:"occurredAt"`
}

type ReportingPrivacyStore interface {
	Create(context.Context, ReportingPrivacyPolicy, ReportingPrivacyEvent) (ReportingPrivacyPolicy, error)
	Get(context.Context, string) (ReportingPrivacyPolicy, error)
	List(context.Context, string, int) ([]ReportingPrivacyPolicy, error)
	CompareAndSwap(context.Context, ReportingPrivacyPolicy, int64, ReportingPrivacyEvent) (ReportingPrivacyPolicy, error)
	Resolve(context.Context, string, time.Time) (ReportingPrivacyPolicy, error)
	Events(context.Context, string, int) ([]ReportingPrivacyEvent, error)
}

type ReportingPrivacyAdministration struct {
	Store ReportingPrivacyStore
	Audit *audit.Recorder
	Clock func() time.Time
}

func (s *ReportingPrivacyAdministration) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func (s *ReportingPrivacyAdministration) Create(ctx context.Context, policy ReportingPrivacyPolicy, actor, reason, correlation string) (ReportingPrivacyPolicy, error) {
	if s == nil || s.Store == nil {
		return ReportingPrivacyPolicy{}, errors.New("reporting privacy service is not configured")
	}
	if policy.MinimumCohortSize < 2 || policy.MinimumCohortSize > 10000 || strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 8 {
		return ReportingPrivacyPolicy{}, ErrInvalid
	}
	identifier, err := id.New()
	if err != nil {
		return ReportingPrivacyPolicy{}, err
	}
	now := s.now()
	if policy.EffectiveFrom.IsZero() {
		policy.EffectiveFrom = now
	}
	if policy.EffectiveTo != nil && !policy.EffectiveTo.After(policy.EffectiveFrom) {
		return ReportingPrivacyPolicy{}, ErrInvalid
	}
	if strings.TrimSpace(policy.SuppressionLabel) == "" {
		policy.SuppressionLabel = "SUPPRESSED_SMALL_COHORT"
	}
	policy.ID, policy.Status, policy.CreatedBy, policy.Reason, policy.Version, policy.CreatedAt, policy.UpdatedAt = identifier, ReportingPrivacyDraft, actor, strings.TrimSpace(reason), 1, now, now
	event, err := newReportingPrivacyEvent(policy, "CREATED", actor, reason, now)
	if err != nil {
		return ReportingPrivacyPolicy{}, err
	}
	out, err := s.Store.Create(ctx, policy, event)
	if err == nil {
		err = recordReportingPrivacyAudit(ctx, s.Audit, actor, "REPORTING_PRIVACY_POLICY_CREATED", out.ID, out, reason, correlation, now)
	}
	return out, err
}

func (s *ReportingPrivacyAdministration) Submit(ctx context.Context, identifier string, expected int64, actor, reason, correlation string) (ReportingPrivacyPolicy, error) {
	current, err := s.Store.Get(ctx, identifier)
	if err != nil {
		return ReportingPrivacyPolicy{}, err
	}
	if current.Status != ReportingPrivacyDraft || len(strings.TrimSpace(reason)) < 8 {
		return ReportingPrivacyPolicy{}, ErrConflict
	}
	current.Status, current.SubmittedBy, current.Reason, current.UpdatedAt, current.Version = ReportingPrivacyPending, actor, strings.TrimSpace(reason), s.now(), current.Version+1
	event, err := newReportingPrivacyEvent(current, "SUBMITTED", actor, reason, current.UpdatedAt)
	if err != nil {
		return ReportingPrivacyPolicy{}, err
	}
	out, err := s.Store.CompareAndSwap(ctx, current, expected, event)
	if err == nil {
		err = recordReportingPrivacyAudit(ctx, s.Audit, actor, "REPORTING_PRIVACY_POLICY_SUBMITTED", out.ID, out, reason, correlation, current.UpdatedAt)
	}
	return out, err
}

func (s *ReportingPrivacyAdministration) Decide(ctx context.Context, identifier string, expected int64, approve bool, actor, reason, correlation string) (ReportingPrivacyPolicy, error) {
	current, err := s.Store.Get(ctx, identifier)
	if err != nil {
		return ReportingPrivacyPolicy{}, err
	}
	if current.Status != ReportingPrivacyPending || current.CreatedBy == actor || current.SubmittedBy == actor || len(strings.TrimSpace(reason)) < 8 {
		return ReportingPrivacyPolicy{}, ErrConflict
	}
	now := s.now()
	eventType := "REJECTED"
	current.ApprovedBy, current.Reason, current.UpdatedAt, current.Version = actor, strings.TrimSpace(reason), now, current.Version+1
	if approve {
		current.Status, eventType = ReportingPrivacyActive, "ACTIVATED"
	} else {
		current.Status = ReportingPrivacyRejected
	}
	event, err := newReportingPrivacyEvent(current, eventType, actor, reason, now)
	if err != nil {
		return ReportingPrivacyPolicy{}, err
	}
	out, err := s.Store.CompareAndSwap(ctx, current, expected, event)
	if err == nil {
		err = recordReportingPrivacyAudit(ctx, s.Audit, actor, "REPORTING_PRIVACY_POLICY_"+eventType, out.ID, out, reason, correlation, now)
	}
	return out, err
}

func (s *ReportingPrivacyAdministration) Retire(ctx context.Context, identifier string, expected int64, actor, reason, correlation string) (ReportingPrivacyPolicy, error) {
	current, err := s.Store.Get(ctx, identifier)
	if err != nil {
		return ReportingPrivacyPolicy{}, err
	}
	if current.Status != ReportingPrivacyActive || len(strings.TrimSpace(reason)) < 8 {
		return ReportingPrivacyPolicy{}, ErrConflict
	}
	now := s.now()
	current.Status, current.ApprovedBy, current.Reason, current.EffectiveTo, current.UpdatedAt, current.Version = ReportingPrivacyRetired, actor, strings.TrimSpace(reason), &now, now, current.Version+1
	event, err := newReportingPrivacyEvent(current, "RETIRED", actor, reason, now)
	if err != nil {
		return ReportingPrivacyPolicy{}, err
	}
	out, err := s.Store.CompareAndSwap(ctx, current, expected, event)
	if err == nil {
		err = recordReportingPrivacyAudit(ctx, s.Audit, actor, "REPORTING_PRIVACY_POLICY_RETIRED", out.ID, out, reason, correlation, now)
	}
	return out, err
}

func (s *ReportingPrivacyAdministration) Resolve(ctx context.Context, organisationID string, at time.Time) (ReportingPrivacyPolicy, error) {
	if s == nil || s.Store == nil {
		return conservativeReportingPrivacyPolicy(at), nil
	}
	policy, err := s.Store.Resolve(ctx, strings.TrimSpace(organisationID), at.UTC())
	if errors.Is(err, ErrNotFound) {
		return conservativeReportingPrivacyPolicy(at), nil
	}
	return policy, err
}
func (s *ReportingPrivacyAdministration) List(ctx context.Context, organisationID string, limit int) ([]ReportingPrivacyPolicy, error) {
	return s.Store.List(ctx, organisationID, limit)
}
func (s *ReportingPrivacyAdministration) Get(ctx context.Context, id string) (ReportingPrivacyPolicy, error) {
	return s.Store.Get(ctx, id)
}
func (s *ReportingPrivacyAdministration) Events(ctx context.Context, id string, limit int) ([]ReportingPrivacyEvent, error) {
	return s.Store.Events(ctx, id, limit)
}

func conservativeReportingPrivacyPolicy(at time.Time) ReportingPrivacyPolicy {
	return ReportingPrivacyPolicy{ID: "SYSTEM-CONSERVATIVE-DEFAULT", Status: ReportingPrivacyActive, MinimumCohortSize: 10, SuppressionLabel: "SUPPRESSED_SMALL_COHORT", ApplyGeography: true, ApplyDemographics: true, ApplyAttributes: true, EffectiveFrom: time.Unix(0, 0).UTC(), Version: 1, CreatedBy: "SYSTEM", ApprovedBy: "SYSTEM", Reason: "conservative reporting privacy default", CreatedAt: at.UTC(), UpdatedAt: at.UTC()}
}

func newReportingPrivacyEvent(policy ReportingPrivacyPolicy, eventType, actor, reason string, at time.Time) (ReportingPrivacyEvent, error) {
	identifier, err := id.New()
	if err != nil {
		return ReportingPrivacyEvent{}, err
	}
	return ReportingPrivacyEvent{ID: identifier, PolicyID: policy.ID, EventType: eventType, ActorID: actor, Reason: strings.TrimSpace(reason), PolicyVersion: policy.Version, OccurredAt: at.UTC()}, nil
}
func recordReportingPrivacyAudit(ctx context.Context, recorder *audit.Recorder, actor, action, objectID string, after any, reason, correlation string, at time.Time) error {
	if recorder == nil {
		return nil
	}
	_, err := recorder.Record(ctx, audit.Input{ActorType: "USER", ActorID: actor, Action: action, ObjectType: "REPORTING_PRIVACY_POLICY", ObjectID: objectID, After: after, Reason: reason, CorrelationID: correlation, OccurredAt: at})
	return err
}

// MemoryReportingPrivacyStore is used in development and deterministic tests.
type MemoryReportingPrivacyStore struct {
	mu       sync.RWMutex
	policies map[string]ReportingPrivacyPolicy
	events   map[string][]ReportingPrivacyEvent
}

func NewMemoryReportingPrivacyStore() *MemoryReportingPrivacyStore {
	return &MemoryReportingPrivacyStore{policies: map[string]ReportingPrivacyPolicy{}, events: map[string][]ReportingPrivacyEvent{}}
}
func (m *MemoryReportingPrivacyStore) Create(_ context.Context, p ReportingPrivacyPolicy, e ReportingPrivacyEvent) (ReportingPrivacyPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.policies[p.ID]; ok {
		return ReportingPrivacyPolicy{}, ErrConflict
	}
	m.policies[p.ID] = p
	m.events[p.ID] = append(m.events[p.ID], e)
	return p, nil
}
func (m *MemoryReportingPrivacyStore) Get(_ context.Context, id string) (ReportingPrivacyPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.policies[id]
	if !ok {
		return ReportingPrivacyPolicy{}, ErrNotFound
	}
	return p, nil
}
func (m *MemoryReportingPrivacyStore) List(_ context.Context, org string, limit int) ([]ReportingPrivacyPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := []ReportingPrivacyPolicy{}
	for _, p := range m.policies {
		if org != "" && p.OrganisationID != org {
			continue
		}
		out = append(out, p)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
func (m *MemoryReportingPrivacyStore) ListReportingPrivacyPage(_ context.Context, org string, limit int, before *time.Time, beforeID string) ([]ReportingPrivacyPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ReportingPrivacyPolicy, 0, len(m.policies))
	for _, p := range m.policies {
		if org != "" && p.OrganisationID != org {
			continue
		}
		if before != nil && !(p.CreatedAt.Before(*before) || (p.CreatedAt.Equal(*before) && p.ID < beforeID)) {
			continue
		}
		out = append(out, p)
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

func (m *MemoryReportingPrivacyStore) CompareAndSwap(_ context.Context, p ReportingPrivacyPolicy, expected int64, e ReportingPrivacyEvent) (ReportingPrivacyPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.policies[p.ID]
	if !ok {
		return ReportingPrivacyPolicy{}, ErrNotFound
	}
	if cur.Version != expected {
		return ReportingPrivacyPolicy{}, ErrConflict
	}
	if p.Status == ReportingPrivacyActive {
		for id, other := range m.policies {
			if id == p.ID || other.Status != ReportingPrivacyActive || other.OrganisationID != p.OrganisationID {
				continue
			}
			if !periodsOverlap(other.EffectiveFrom, other.EffectiveTo, p.EffectiveFrom, p.EffectiveTo) {
				continue
			}
			end := p.EffectiveFrom
			other.EffectiveTo = &end
			if !p.EffectiveFrom.After(p.UpdatedAt) {
				other.Status = ReportingPrivacyRetired
			}
			other.Version++
			other.UpdatedAt = p.UpdatedAt
			m.policies[id] = other
		}
	}
	m.policies[p.ID] = p
	m.events[p.ID] = append(m.events[p.ID], e)
	return p, nil
}
func (m *MemoryReportingPrivacyStore) Resolve(_ context.Context, org string, at time.Time) (ReportingPrivacyPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var selected *ReportingPrivacyPolicy
	for _, p := range m.policies {
		if p.Status != ReportingPrivacyActive || p.EffectiveFrom.After(at) || (p.EffectiveTo != nil && !p.EffectiveTo.After(at)) {
			continue
		}
		if p.OrganisationID != org && p.OrganisationID != "" {
			continue
		}
		candidate := p
		if selected == nil || (candidate.OrganisationID == org && selected.OrganisationID == "") || candidate.EffectiveFrom.After(selected.EffectiveFrom) {
			selected = &candidate
		}
	}
	if selected == nil {
		return ReportingPrivacyPolicy{}, ErrNotFound
	}
	return *selected, nil
}
func (m *MemoryReportingPrivacyStore) Events(_ context.Context, id string, limit int) ([]ReportingPrivacyEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.policies[id]; !ok {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	items := m.events[id]
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	return append([]ReportingPrivacyEvent(nil), items...), nil
}

func (m *MemoryReportingPrivacyStore) ListReportingPrivacyEventPage(_ context.Context, id string, limit int, before *time.Time, beforeID string) ([]ReportingPrivacyEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.policies[id]; !ok {
		return nil, ErrNotFound
	}
	items := append([]ReportingPrivacyEvent(nil), m.events[id]...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].OccurredAt.Equal(items[j].OccurredAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].OccurredAt.After(items[j].OccurredAt)
	})
	out := make([]ReportingPrivacyEvent, 0, limit)
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

type PostgreSQLReportingPrivacyStore struct{ DB *sql.DB }

const reportingPrivacyColumns = `id::text,coalesce(organisation_id::text,''),status,minimum_cohort_size,suppression_label,apply_geography,apply_demographics,apply_attributes,effective_from,effective_to,created_by::text,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,version,created_at,updated_at`
const reportingPrivacySelect = `SELECT ` + reportingPrivacyColumns + ` FROM reporting_privacy_policies`

func scanReportingPrivacy(s interface{ Scan(...any) error }) (ReportingPrivacyPolicy, error) {
	var p ReportingPrivacyPolicy
	err := s.Scan(&p.ID, &p.OrganisationID, &p.Status, &p.MinimumCohortSize, &p.SuppressionLabel, &p.ApplyGeography, &p.ApplyDemographics, &p.ApplyAttributes, &p.EffectiveFrom, &p.EffectiveTo, &p.CreatedBy, &p.SubmittedBy, &p.ApprovedBy, &p.Reason, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}
func (p *PostgreSQLReportingPrivacyStore) Create(ctx context.Context, v ReportingPrivacyPolicy, e ReportingPrivacyEvent) (ReportingPrivacyPolicy, error) {
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO reporting_privacy_policies(id,organisation_id,status,minimum_cohort_size,suppression_label,apply_geography,apply_demographics,apply_attributes,effective_from,effective_to,created_by,reason,version,created_at,updated_at) VALUES($1::uuid,NULLIF($2,'')::uuid,$3,$4,$5,$6,$7,$8,$9,$10,$11::uuid,$12,$13,$14,$15)`, v.ID, v.OrganisationID, v.Status, v.MinimumCohortSize, v.SuppressionLabel, v.ApplyGeography, v.ApplyDemographics, v.ApplyAttributes, v.EffectiveFrom, v.EffectiveTo, v.CreatedBy, v.Reason, v.Version, v.CreatedAt, v.UpdatedAt)
	if err != nil {
		return v, err
	}
	if err = insertReportingPrivacyEvent(ctx, tx, e); err != nil {
		return v, err
	}
	if err = tx.Commit(); err != nil {
		return v, err
	}
	return v, nil
}
func (p *PostgreSQLReportingPrivacyStore) Get(ctx context.Context, id string) (ReportingPrivacyPolicy, error) {
	v, err := scanReportingPrivacy(p.DB.QueryRowContext(ctx, reportingPrivacySelect+` WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}
func (p *PostgreSQLReportingPrivacyStore) List(ctx context.Context, org string, limit int) ([]ReportingPrivacyPolicy, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := p.DB.QueryContext(ctx, reportingPrivacySelect+` WHERE ($1='' OR organisation_id=NULLIF($1,'')::uuid) ORDER BY created_at DESC,id DESC LIMIT $2`, org, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportingPrivacyPolicy{}
	for rows.Next() {
		v, e := scanReportingPrivacy(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (p *PostgreSQLReportingPrivacyStore) ListReportingPrivacyPage(ctx context.Context, org string, limit int, before *time.Time, beforeID string) ([]ReportingPrivacyPolicy, error) {
	if p == nil || p.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	rows, err := p.DB.QueryContext(ctx, reportingPrivacySelect+` WHERE ($1='' OR organisation_id=NULLIF($1,'')::uuid) AND ($3::timestamptz IS NULL OR created_at<$3 OR (created_at=$3 AND id<NULLIF($4,'')::uuid)) ORDER BY created_at DESC,id DESC LIMIT $2`, org, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ReportingPrivacyPolicy, 0, limit)
	for rows.Next() {
		v, scanErr := scanReportingPrivacy(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (p *PostgreSQLReportingPrivacyStore) CompareAndSwap(ctx context.Context, v ReportingPrivacyPolicy, expected int64, e ReportingPrivacyEvent) (ReportingPrivacyPolicy, error) {
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if v.Status == ReportingPrivacyActive {
		scope := "platform"
		if v.OrganisationID != "" {
			scope = v.OrganisationID
		}
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "report-privacy:"+scope); err != nil {
			return v, err
		}
		_, err = tx.ExecContext(ctx, `WITH superseded AS (
 UPDATE reporting_privacy_policies
 SET status=CASE WHEN $1<=$2 THEN 'RETIRED' ELSE status END,
     effective_to=$1,version=version+1,updated_at=$2,reason=$3,approved_by=$4::uuid
 WHERE id<>$5::uuid AND coalesce(organisation_id::text,'')=$6 AND status='ACTIVE'
   AND effective_from<$7 AND (effective_to IS NULL OR effective_to>$1)
 RETURNING id,version
) INSERT INTO reporting_privacy_policy_events(id,policy_id,event_type,actor_id,reason,policy_version,occurred_at)
 SELECT gen_random_uuid(),id,'SUPERSEDED',$4::uuid,$3,version,$2 FROM superseded`, v.EffectiveFrom, v.UpdatedAt, "superseded by policy "+v.ID, v.ApprovedBy, v.ID, v.OrganisationID, coalescePolicyEnd(v.EffectiveTo))
		if err != nil {
			return v, err
		}
	}
	updated, err := scanReportingPrivacy(tx.QueryRowContext(ctx, `UPDATE reporting_privacy_policies SET organisation_id=NULLIF($3,'')::uuid,status=$4,minimum_cohort_size=$5,suppression_label=$6,apply_geography=$7,apply_demographics=$8,apply_attributes=$9,effective_from=$10,effective_to=$11,submitted_by=NULLIF($12,'')::uuid,approved_by=NULLIF($13,'')::uuid,reason=$14,version=$15,updated_at=$16 WHERE id=$1::uuid AND version=$2 RETURNING `+reportingPrivacyColumns, v.ID, expected, v.OrganisationID, v.Status, v.MinimumCohortSize, v.SuppressionLabel, v.ApplyGeography, v.ApplyDemographics, v.ApplyAttributes, v.EffectiveFrom, v.EffectiveTo, v.SubmittedBy, v.ApprovedBy, v.Reason, v.Version, v.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrConflict
	}
	if err != nil {
		return v, err
	}
	e.PolicyVersion = updated.Version
	if err = insertReportingPrivacyEvent(ctx, tx, e); err != nil {
		return v, err
	}
	if err = tx.Commit(); err != nil {
		return v, err
	}
	return updated, nil
}
func (p *PostgreSQLReportingPrivacyStore) Resolve(ctx context.Context, org string, at time.Time) (ReportingPrivacyPolicy, error) {
	v, err := scanReportingPrivacy(p.DB.QueryRowContext(ctx, reportingPrivacySelect+` WHERE status='ACTIVE' AND effective_from<=$2 AND (effective_to IS NULL OR effective_to>$2) AND (organisation_id=NULLIF($1,'')::uuid OR organisation_id IS NULL) ORDER BY (organisation_id IS NOT NULL) DESC,effective_from DESC,id DESC LIMIT 1`, org, at))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}
func (p *PostgreSQLReportingPrivacyStore) Events(ctx context.Context, id string, limit int) ([]ReportingPrivacyEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return p.ListReportingPrivacyEventPage(ctx, id, limit, nil, "")
}

func (p *PostgreSQLReportingPrivacyStore) ListReportingPrivacyEventPage(ctx context.Context, id string, limit int, before *time.Time, beforeID string) ([]ReportingPrivacyEvent, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,policy_id::text,event_type,actor_id::text,coalesce(reason,''),policy_version,occurred_at FROM reporting_privacy_policy_events WHERE policy_id=$1::uuid AND ($3::timestamptz IS NULL OR occurred_at<$3 OR (occurred_at=$3 AND id<NULLIF($4,'')::uuid)) ORDER BY occurred_at DESC,id DESC LIMIT $2`, id, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportingPrivacyEvent{}
	for rows.Next() {
		var e ReportingPrivacyEvent
		if err := rows.Scan(&e.ID, &e.PolicyID, &e.EventType, &e.ActorID, &e.Reason, &e.PolicyVersion, &e.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func insertReportingPrivacyEvent(ctx context.Context, tx *sql.Tx, e ReportingPrivacyEvent) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO reporting_privacy_policy_events(id,policy_id,event_type,actor_id,reason,policy_version,occurred_at) VALUES($1::uuid,$2::uuid,$3,$4::uuid,NULLIF($5,''),$6,$7)`, e.ID, e.PolicyID, e.EventType, e.ActorID, e.Reason, e.PolicyVersion, e.OccurredAt)
	return err
}
func coalescePolicyEnd(v *time.Time) time.Time {
	if v != nil {
		return v.UTC()
	}
	return time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
}

func periodsOverlap(aStart time.Time, aEnd *time.Time, bStart time.Time, bEnd *time.Time) bool {
	aLimit, bLimit := coalescePolicyEnd(aEnd), coalescePolicyEnd(bEnd)
	return aStart.Before(bLimit) && bStart.Before(aLimit)
}
