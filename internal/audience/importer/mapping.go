package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/audit"
	"campaign-platform/internal/shared/id"
)

type MappingStatus string

const (
	MappingDraft    MappingStatus = "DRAFT"
	MappingPending  MappingStatus = "PENDING_APPROVAL"
	MappingActive   MappingStatus = "ACTIVE"
	MappingRejected MappingStatus = "REJECTED"
	MappingRetired  MappingStatus = "RETIRED"
)

type MappingDefinition struct {
	ID              string        `json:"id"`
	OrganisationID  string        `json:"organisationId,omitempty"`
	Name            string        `json:"name"`
	SourceSystem    string        `json:"sourceSystem,omitempty"`
	TemplateVersion string        `json:"templateVersion"`
	Worksheet       string        `json:"worksheet,omitempty"`
	Mapping         ColumnMapping `json:"mapping"`
	Status          MappingStatus `json:"status"`
	EffectiveFrom   time.Time     `json:"effectiveFrom"`
	EffectiveTo     *time.Time    `json:"effectiveTo,omitempty"`
	CreatedBy       string        `json:"createdBy"`
	SubmittedBy     string        `json:"submittedBy,omitempty"`
	ApprovedBy      string        `json:"approvedBy,omitempty"`
	Reason          string        `json:"reason"`
	Version         int64         `json:"version"`
	CreatedAt       time.Time     `json:"createdAt"`
	UpdatedAt       time.Time     `json:"updatedAt"`
}
type MappingEvent struct {
	ID             string    `json:"id"`
	MappingID      string    `json:"mappingId"`
	EventType      string    `json:"eventType"`
	ActorID        string    `json:"actorId"`
	Reason         string    `json:"reason"`
	MappingVersion int64     `json:"mappingVersion"`
	OccurredAt     time.Time `json:"occurredAt"`
}

type MappingStore interface {
	Create(context.Context, MappingDefinition, MappingEvent) (MappingDefinition, error)
	Get(context.Context, string) (MappingDefinition, error)
	List(context.Context, string, string, int) ([]MappingDefinition, error)
	CompareAndSwap(context.Context, MappingDefinition, int64, MappingEvent) (MappingDefinition, error)
	Resolve(context.Context, string, string, string, time.Time) (MappingDefinition, error)
	Events(context.Context, string, int) ([]MappingEvent, error)
}

type MappingAdministration struct {
	Store MappingStore
	Audit *audit.Recorder
	Clock func() time.Time
}

func (s *MappingAdministration) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
func validateMappingDefinition(v MappingDefinition) error {
	if strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.TemplateVersion) == "" || strings.TrimSpace(v.Mapping.MSISDN) == "" {
		return errors.New("mapping name, template version and MSISDN column are required")
	}
	payload, err := json.Marshal(v.Mapping)
	if err != nil || len(payload) > 64<<10 {
		return errors.New("mapping definition is invalid or too large")
	}
	return nil
}
func (s *MappingAdministration) Create(ctx context.Context, v MappingDefinition, actor, reason, correlation string) (MappingDefinition, error) {
	if s == nil || s.Store == nil {
		return v, errors.New("mapping administration unavailable")
	}
	if err := validateMappingDefinition(v); err != nil {
		return v, err
	}
	if strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 8 {
		return v, ErrImportTransition
	}
	identifier, err := id.New()
	if err != nil {
		return v, err
	}
	now := s.now()
	if v.EffectiveFrom.IsZero() {
		v.EffectiveFrom = now
	}
	if v.EffectiveTo != nil && !v.EffectiveTo.After(v.EffectiveFrom) {
		return v, ErrImportTransition
	}
	v.ID, v.Status, v.CreatedBy, v.Reason, v.Version, v.CreatedAt, v.UpdatedAt = identifier, MappingDraft, actor, strings.TrimSpace(reason), 1, now, now
	eventID, err := id.New()
	if err != nil {
		return v, err
	}
	e := MappingEvent{ID: eventID, MappingID: v.ID, EventType: "CREATED", ActorID: actor, Reason: v.Reason, MappingVersion: 1, OccurredAt: now}
	out, err := s.Store.Create(ctx, v, e)
	if err == nil {
		err = recordMappingAudit(ctx, s.Audit, actor, "AUDIENCE_IMPORT_MAPPING_CREATED", out, reason, correlation, now)
	}
	return out, err
}
func (s *MappingAdministration) Submit(ctx context.Context, idv string, expected int64, actor, reason, correlation string) (MappingDefinition, error) {
	cur, err := s.Store.Get(ctx, idv)
	if err != nil {
		return cur, err
	}
	if cur.Status != MappingDraft || len(strings.TrimSpace(reason)) < 8 {
		return cur, ErrImportTransition
	}
	now := s.now()
	cur.Status, cur.SubmittedBy, cur.Reason, cur.Version, cur.UpdatedAt = MappingPending, actor, strings.TrimSpace(reason), cur.Version+1, now
	eventID, err := id.New()
	if err != nil {
		return cur, err
	}
	e := MappingEvent{ID: eventID, MappingID: cur.ID, EventType: "SUBMITTED", ActorID: actor, Reason: cur.Reason, MappingVersion: cur.Version, OccurredAt: now}
	out, err := s.Store.CompareAndSwap(ctx, cur, expected, e)
	if err == nil {
		err = recordMappingAudit(ctx, s.Audit, actor, "AUDIENCE_IMPORT_MAPPING_SUBMITTED", out, reason, correlation, now)
	}
	return out, err
}
func (s *MappingAdministration) Decide(ctx context.Context, idv string, expected int64, approve bool, actor, reason, correlation string) (MappingDefinition, error) {
	cur, err := s.Store.Get(ctx, idv)
	if err != nil {
		return cur, err
	}
	if cur.Status != MappingPending || cur.CreatedBy == actor || cur.SubmittedBy == actor || len(strings.TrimSpace(reason)) < 8 {
		return cur, ErrImportTransition
	}
	now := s.now()
	eventType := "REJECTED"
	cur.Status = MappingRejected
	if approve {
		cur.Status = MappingActive
		eventType = "ACTIVATED"
	}
	cur.ApprovedBy, cur.Reason, cur.Version, cur.UpdatedAt = actor, strings.TrimSpace(reason), cur.Version+1, now
	eventID, err := id.New()
	if err != nil {
		return cur, err
	}
	e := MappingEvent{ID: eventID, MappingID: cur.ID, EventType: eventType, ActorID: actor, Reason: cur.Reason, MappingVersion: cur.Version, OccurredAt: now}
	out, err := s.Store.CompareAndSwap(ctx, cur, expected, e)
	if err == nil {
		err = recordMappingAudit(ctx, s.Audit, actor, "AUDIENCE_IMPORT_MAPPING_"+eventType, out, reason, correlation, now)
	}
	return out, err
}
func (s *MappingAdministration) Retire(ctx context.Context, idv string, expected int64, actor, reason, correlation string) (MappingDefinition, error) {
	cur, err := s.Store.Get(ctx, idv)
	if err != nil {
		return cur, err
	}
	if cur.Status != MappingActive || len(strings.TrimSpace(reason)) < 8 {
		return cur, ErrImportTransition
	}
	now := s.now()
	cur.Status, cur.ApprovedBy, cur.Reason, cur.EffectiveTo, cur.Version, cur.UpdatedAt = MappingRetired, actor, strings.TrimSpace(reason), &now, cur.Version+1, now
	eventID, err := id.New()
	if err != nil {
		return cur, err
	}
	e := MappingEvent{ID: eventID, MappingID: cur.ID, EventType: "RETIRED", ActorID: actor, Reason: cur.Reason, MappingVersion: cur.Version, OccurredAt: now}
	out, err := s.Store.CompareAndSwap(ctx, cur, expected, e)
	if err == nil {
		err = recordMappingAudit(ctx, s.Audit, actor, "AUDIENCE_IMPORT_MAPPING_RETIRED", out, reason, correlation, now)
	}
	return out, err
}
func (s *MappingAdministration) Resolve(ctx context.Context, org, source, name string, at time.Time) (MappingDefinition, error) {
	return s.Store.Resolve(ctx, strings.TrimSpace(org), strings.ToUpper(strings.TrimSpace(source)), strings.TrimSpace(name), at.UTC())
}
func (s *MappingAdministration) Get(ctx context.Context, id string) (MappingDefinition, error) {
	return s.Store.Get(ctx, id)
}
func (s *MappingAdministration) List(ctx context.Context, org, source string, limit int) ([]MappingDefinition, error) {
	return s.Store.List(ctx, org, source, limit)
}
func (s *MappingAdministration) Events(ctx context.Context, id string, limit int) ([]MappingEvent, error) {
	return s.Store.Events(ctx, id, limit)
}
func recordMappingAudit(ctx context.Context, r *audit.Recorder, actor, action string, v MappingDefinition, reason, correlation string, at time.Time) error {
	if r == nil {
		return nil
	}
	_, err := r.Record(ctx, audit.Input{ActorType: "USER", ActorID: actor, Action: action, ObjectType: "AUDIENCE_IMPORT_MAPPING", ObjectID: v.ID, After: v, Reason: reason, CorrelationID: correlation, OccurredAt: at})
	return err
}

type MemoryMappingStore struct {
	mu     sync.RWMutex
	items  map[string]MappingDefinition
	events map[string][]MappingEvent
}

func NewMemoryMappingStore() *MemoryMappingStore {
	return &MemoryMappingStore{items: map[string]MappingDefinition{}, events: map[string][]MappingEvent{}}
}
func (m *MemoryMappingStore) Create(_ context.Context, v MappingDefinition, e MappingEvent) (MappingDefinition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[v.ID] = v
	m.events[v.ID] = append(m.events[v.ID], e)
	return v, nil
}
func (m *MemoryMappingStore) Get(_ context.Context, idv string) (MappingDefinition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.items[idv]
	if !ok {
		return v, ErrImportNotFound
	}
	return v, nil
}
func (m *MemoryMappingStore) List(_ context.Context, org, source string, limit int) ([]MappingDefinition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := []MappingDefinition{}
	for _, v := range m.items {
		if org != "" && v.OrganisationID != org {
			continue
		}
		if source != "" && strings.ToUpper(v.SourceSystem) != strings.ToUpper(source) {
			continue
		}
		out = append(out, v)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
func (m *MemoryMappingStore) ListMappingPage(_ context.Context, org, source string, limit int, before *time.Time, beforeID string) ([]MappingDefinition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MappingDefinition, 0, len(m.items))
	for _, v := range m.items {
		if org != "" && v.OrganisationID != org {
			continue
		}
		if source != "" && !strings.EqualFold(v.SourceSystem, source) {
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

func (m *MemoryMappingStore) CompareAndSwap(_ context.Context, v MappingDefinition, expected int64, e MappingEvent) (MappingDefinition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.items[v.ID]
	if !ok {
		return v, ErrImportNotFound
	}
	if cur.Version != expected {
		return v, ErrImportConflict
	}
	if v.Status == MappingActive {
		for key, existing := range m.items {
			if key == v.ID || existing.Status != MappingActive || existing.OrganisationID != v.OrganisationID || !strings.EqualFold(existing.SourceSystem, v.SourceSystem) || !strings.EqualFold(existing.Name, v.Name) {
				continue
			}
			if !existing.EffectiveFrom.Before(v.EffectiveFrom) {
				return v, ErrImportConflict
			}
			if existing.EffectiveTo != nil && !existing.EffectiveTo.After(v.EffectiveFrom) {
				continue
			}
			end := v.EffectiveFrom
			existing.EffectiveTo = &end
			existing.Version++
			existing.UpdatedAt = v.UpdatedAt
			m.items[key] = existing
			eventID, err := id.New()
			if err != nil {
				return v, err
			}
			m.events[key] = append(m.events[key], MappingEvent{ID: eventID, MappingID: key, EventType: "SUPERSEDED", ActorID: v.ApprovedBy, Reason: "superseded by mapping " + v.ID, MappingVersion: existing.Version, OccurredAt: v.UpdatedAt})
		}
	}
	m.items[v.ID] = v
	m.events[v.ID] = append(m.events[v.ID], e)
	return v, nil
}
func (m *MemoryMappingStore) Resolve(_ context.Context, org, source, name string, at time.Time) (MappingDefinition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var selected *MappingDefinition
	for _, v := range m.items {
		if v.Status != MappingActive || v.EffectiveFrom.After(at) || (v.EffectiveTo != nil && !v.EffectiveTo.After(at)) || !strings.EqualFold(v.SourceSystem, source) || !strings.EqualFold(v.Name, name) {
			continue
		}
		if v.OrganisationID != org && v.OrganisationID != "" {
			continue
		}
		copy := v
		if selected == nil || (copy.OrganisationID == org && selected.OrganisationID == "") || copy.EffectiveFrom.After(selected.EffectiveFrom) {
			selected = &copy
		}
	}
	if selected == nil {
		return MappingDefinition{}, ErrImportNotFound
	}
	return *selected, nil
}
func (m *MemoryMappingStore) Events(_ context.Context, idv string, limit int) ([]MappingEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.items[idv]; !ok {
		return nil, ErrImportNotFound
	}
	items := m.events[idv]
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	return append([]MappingEvent(nil), items...), nil
}

func (m *MemoryMappingStore) ListMappingEventPage(_ context.Context, idv string, limit int, before *time.Time, beforeID string) ([]MappingEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.items[idv]; !ok {
		return nil, ErrImportNotFound
	}
	items := append([]MappingEvent(nil), m.events[idv]...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].OccurredAt.Equal(items[j].OccurredAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].OccurredAt.After(items[j].OccurredAt)
	})
	out := make([]MappingEvent, 0, limit)
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

type PostgreSQLMappingStore struct{ DB *sql.DB }

const mappingColumns = `id::text,coalesce(organisation_id::text,''),name,coalesce(source_system,''),template_version,coalesce(worksheet,''),mapping,status,effective_from,effective_to,created_by::text,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,version,created_at,updated_at`
const mappingSelect = `SELECT ` + mappingColumns + ` FROM audience_import_mapping_definitions`

type mappingScanner interface{ Scan(...any) error }

func scanMapping(s mappingScanner) (MappingDefinition, error) {
	var v MappingDefinition
	var raw []byte
	err := s.Scan(&v.ID, &v.OrganisationID, &v.Name, &v.SourceSystem, &v.TemplateVersion, &v.Worksheet, &raw, &v.Status, &v.EffectiveFrom, &v.EffectiveTo, &v.CreatedBy, &v.SubmittedBy, &v.ApprovedBy, &v.Reason, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(raw, &v.Mapping)
	}
	return v, err
}
func (p *PostgreSQLMappingStore) Create(ctx context.Context, v MappingDefinition, e MappingEvent) (MappingDefinition, error) {
	raw, err := json.Marshal(v.Mapping)
	if err != nil {
		return v, err
	}
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO audience_import_mapping_definitions(id,organisation_id,name,source_system,template_version,worksheet,mapping,status,effective_from,effective_to,created_by,reason,version,created_at,updated_at) VALUES($1::uuid,NULLIF($2,'')::uuid,$3,NULLIF($4,''),$5,NULLIF($6,''),$7::jsonb,$8,$9,$10,$11::uuid,$12,$13,$14,$15)`, v.ID, v.OrganisationID, v.Name, v.SourceSystem, v.TemplateVersion, v.Worksheet, string(raw), v.Status, v.EffectiveFrom, v.EffectiveTo, v.CreatedBy, v.Reason, v.Version, v.CreatedAt, v.UpdatedAt)
	if err == nil {
		err = insertMappingEvent(ctx, tx, e)
	}
	if err == nil {
		err = tx.Commit()
	}
	return v, err
}
func (p *PostgreSQLMappingStore) Get(ctx context.Context, idv string) (MappingDefinition, error) {
	v, err := scanMapping(p.DB.QueryRowContext(ctx, mappingSelect+` WHERE id=$1::uuid`, idv))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrImportNotFound
	}
	return v, err
}
func (p *PostgreSQLMappingStore) List(ctx context.Context, org, source string, limit int) ([]MappingDefinition, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := p.DB.QueryContext(ctx, mappingSelect+` WHERE ($1='' OR organisation_id=NULLIF($1,'')::uuid) AND ($2='' OR upper(coalesce(source_system,''))=upper($2)) ORDER BY created_at DESC,id DESC LIMIT $3`, org, source, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MappingDefinition{}
	for rows.Next() {
		v, e := scanMapping(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (p *PostgreSQLMappingStore) ListMappingPage(ctx context.Context, org, source string, limit int, before *time.Time, beforeID string) ([]MappingDefinition, error) {
	if p == nil || p.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	rows, err := p.DB.QueryContext(ctx, mappingSelect+` WHERE ($1='' OR organisation_id=NULLIF($1,'')::uuid) AND ($2='' OR upper(coalesce(source_system,''))=upper($2)) AND ($4::timestamptz IS NULL OR created_at<$4 OR (created_at=$4 AND id<NULLIF($5,'')::uuid)) ORDER BY created_at DESC,id DESC LIMIT $3`, org, source, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MappingDefinition, 0, limit)
	for rows.Next() {
		v, scanErr := scanMapping(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (p *PostgreSQLMappingStore) CompareAndSwap(ctx context.Context, v MappingDefinition, expected int64, e MappingEvent) (MappingDefinition, error) {
	raw, err := json.Marshal(v.Mapping)
	if err != nil {
		return v, err
	}
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if v.Status == MappingActive {
		rows, queryErr := tx.QueryContext(ctx, `SELECT id::text,effective_from,effective_to,version FROM audience_import_mapping_definitions WHERE id<>$1::uuid AND coalesce(organisation_id::text,'')=$2 AND upper(coalesce(source_system,''))=upper($3) AND lower(name)=lower($4) AND status='ACTIVE' AND tstzrange(effective_from,coalesce(effective_to,'infinity'::timestamptz),'[)') && tstzrange($5,coalesce($6,'infinity'::timestamptz),'[)') FOR UPDATE`, v.ID, v.OrganisationID, v.SourceSystem, v.Name, v.EffectiveFrom, v.EffectiveTo)
		if queryErr != nil {
			return v, queryErr
		}
		type overlappingMapping struct {
			id            string
			effectiveFrom time.Time
			effectiveTo   *time.Time
			version       int64
		}
		overlaps := []overlappingMapping{}
		for rows.Next() {
			var item overlappingMapping
			if scanErr := rows.Scan(&item.id, &item.effectiveFrom, &item.effectiveTo, &item.version); scanErr != nil {
				rows.Close()
				return v, scanErr
			}
			overlaps = append(overlaps, item)
		}
		if closeErr := rows.Close(); closeErr != nil {
			return v, closeErr
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return v, rowsErr
		}
		for _, existing := range overlaps {
			if !existing.effectiveFrom.Before(v.EffectiveFrom) {
				return v, ErrImportConflict
			}
			res, updateErr := tx.ExecContext(ctx, `UPDATE audience_import_mapping_definitions SET effective_to=$1,approved_by=$2::uuid,reason=$3,version=version+1,updated_at=$4 WHERE id=$5::uuid AND version=$6`, v.EffectiveFrom, v.ApprovedBy, "superseded by mapping "+v.ID, v.UpdatedAt, existing.id, existing.version)
			if updateErr != nil {
				return v, updateErr
			}
			updated, rowsErr := res.RowsAffected()
			if rowsErr != nil {
				return v, rowsErr
			}
			if updated != 1 {
				return v, ErrImportConflict
			}
			eventID, idErr := id.New()
			if idErr != nil {
				return v, idErr
			}
			if eventErr := insertMappingEvent(ctx, tx, MappingEvent{ID: eventID, MappingID: existing.id, EventType: "SUPERSEDED", ActorID: v.ApprovedBy, Reason: "superseded by mapping " + v.ID, MappingVersion: existing.version + 1, OccurredAt: v.UpdatedAt}); eventErr != nil {
				return v, eventErr
			}
		}
	}
	v, err = scanMapping(tx.QueryRowContext(ctx, `UPDATE audience_import_mapping_definitions SET organisation_id=NULLIF($3,'')::uuid,name=$4,source_system=NULLIF($5,''),template_version=$6,worksheet=NULLIF($7,''),mapping=$8::jsonb,status=$9,effective_from=$10,effective_to=$11,submitted_by=NULLIF($12,'')::uuid,approved_by=NULLIF($13,'')::uuid,reason=$14,version=$15,updated_at=$16 WHERE id=$1::uuid AND version=$2 RETURNING `+mappingColumns, v.ID, expected, v.OrganisationID, v.Name, v.SourceSystem, v.TemplateVersion, v.Worksheet, string(raw), v.Status, v.EffectiveFrom, v.EffectiveTo, v.SubmittedBy, v.ApprovedBy, v.Reason, v.Version, v.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrImportConflict
	}
	if err == nil {
		e.MappingVersion = v.Version
		err = insertMappingEvent(ctx, tx, e)
	}
	if err == nil {
		err = tx.Commit()
	}
	return v, err
}
func (p *PostgreSQLMappingStore) Resolve(ctx context.Context, org, source, name string, at time.Time) (MappingDefinition, error) {
	v, err := scanMapping(p.DB.QueryRowContext(ctx, mappingSelect+` WHERE status='ACTIVE' AND effective_from<=$4 AND (effective_to IS NULL OR effective_to>$4) AND (organisation_id=NULLIF($1,'')::uuid OR organisation_id IS NULL) AND upper(coalesce(source_system,''))=upper($2) AND lower(name)=lower($3) ORDER BY (organisation_id IS NOT NULL) DESC,effective_from DESC,id DESC LIMIT 1`, org, source, name, at))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrImportNotFound
	}
	return v, err
}
func (p *PostgreSQLMappingStore) Events(ctx context.Context, idv string, limit int) ([]MappingEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return p.ListMappingEventPage(ctx, idv, limit, nil, "")
}

func (p *PostgreSQLMappingStore) ListMappingEventPage(ctx context.Context, idv string, limit int, before *time.Time, beforeID string) ([]MappingEvent, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,mapping_id::text,event_type,actor_id::text,coalesce(reason,''),mapping_version,occurred_at FROM audience_import_mapping_events WHERE mapping_id=$1::uuid AND ($3::timestamptz IS NULL OR occurred_at<$3 OR (occurred_at=$3 AND id<NULLIF($4,'')::uuid)) ORDER BY occurred_at DESC,id DESC LIMIT $2`, idv, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MappingEvent{}
	for rows.Next() {
		var e MappingEvent
		if err := rows.Scan(&e.ID, &e.MappingID, &e.EventType, &e.ActorID, &e.Reason, &e.MappingVersion, &e.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func insertMappingEvent(ctx context.Context, tx *sql.Tx, e MappingEvent) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audience_import_mapping_events(id,mapping_id,event_type,actor_id,reason,mapping_version,occurred_at) VALUES($1::uuid,$2::uuid,$3,$4::uuid,NULLIF($5,''),$6,$7)`, e.ID, e.MappingID, e.EventType, e.ActorID, e.Reason, e.MappingVersion, e.OccurredAt)
	return err
}
