package contactlife

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

type Status string

const (
	StatusActive     Status = "ACTIVE"
	StatusInactive   Status = "INACTIVE"
	StatusSuppressed Status = "SUPPRESSED"
	StatusInvalid    Status = "INVALID"
	StatusRecycled   Status = "RECYCLED"
	StatusDeceased   Status = "DECEASED"
	StatusAnonymised Status = "ANONYMISED"
	StatusDeleted    Status = "DELETED"
)

var (
	ErrNotFound = errors.New("contact lifecycle record not found")
	ErrConflict = errors.New("contact lifecycle version conflict")
	ErrInvalid  = errors.New("invalid contact lifecycle transition")
)

type Record struct {
	ContactID            string    `json:"contactId"`
	MaskedMSISDN         string    `json:"maskedMsisdn"`
	Status               Status    `json:"status"`
	ProcessingRestricted bool      `json:"processingRestricted"`
	Reason               string    `json:"reason,omitempty"`
	UpdatedBy            string    `json:"updatedBy,omitempty"`
	UpdatedAt            time.Time `json:"updatedAt"`
	Version              int64     `json:"version"`
}

type Event struct {
	ID                  string    `json:"id"`
	ContactID           string    `json:"contactId"`
	PreviousStatus      Status    `json:"previousStatus"`
	NewStatus           Status    `json:"newStatus"`
	PreviousRestriction bool      `json:"previousProcessingRestricted"`
	NewRestriction      bool      `json:"newProcessingRestricted"`
	ActorID             string    `json:"actorId"`
	Reason              string    `json:"reason"`
	ContactVersion      int64     `json:"contactVersion"`
	OccurredAt          time.Time `json:"occurredAt"`
}

type Repository interface {
	Get(context.Context, string) (Record, error)
	Transition(context.Context, Record, int64, Event) (Record, error)
	Events(context.Context, string, int) ([]Event, error)
}

type Service struct {
	Repository Repository
	Audit      *audit.Recorder
	Clock      func() time.Time
}

func (s *Service) Get(ctx context.Context, contactID string) (Record, error) {
	if s == nil || s.Repository == nil {
		return Record{}, errors.New("contact lifecycle service is not configured")
	}
	return s.Repository.Get(ctx, strings.TrimSpace(contactID))
}

func (s *Service) Events(ctx context.Context, contactID string, limit int) ([]Event, error) {
	if s == nil || s.Repository == nil {
		return nil, errors.New("contact lifecycle service is not configured")
	}
	return s.Repository.Events(ctx, strings.TrimSpace(contactID), limit)
}

func (s *Service) Transition(ctx context.Context, contactID string, target Status, restricted bool, expected int64, actor, reason, correlation string) (Record, error) {
	if s == nil || s.Repository == nil {
		return Record{}, errors.New("contact lifecycle service is not configured")
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if strings.TrimSpace(contactID) == "" || expected <= 0 || actor == "" || len(reason) < 12 || !operatorAssignable(target) {
		return Record{}, ErrInvalid
	}
	current, err := s.Repository.Get(ctx, contactID)
	if err != nil {
		return Record{}, err
	}
	if current.Version != expected || current.Status == StatusAnonymised || current.Status == StatusDeleted || current.Status == StatusDeceased && target != StatusDeceased {
		return Record{}, ErrConflict
	}
	if current.Status == target && current.ProcessingRestricted == restricted {
		return current, nil
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	eventID, err := id.New()
	if err != nil {
		return Record{}, err
	}
	updated := current
	updated.Status, updated.ProcessingRestricted = target, restricted
	updated.Reason, updated.UpdatedBy, updated.UpdatedAt, updated.Version = reason, actor, now, expected+1
	event := Event{ID: eventID, ContactID: current.ContactID, PreviousStatus: current.Status, NewStatus: target, PreviousRestriction: current.ProcessingRestricted, NewRestriction: restricted, ActorID: actor, Reason: reason, ContactVersion: updated.Version, OccurredAt: now}
	out, err := s.Repository.Transition(ctx, updated, expected, event)
	if err != nil {
		return Record{}, err
	}
	if s.Audit != nil {
		_, err = s.Audit.Record(ctx, audit.Input{ActorType: "USER", ActorID: actor, Action: "CONTACT_LIFECYCLE_CHANGED", ObjectType: "CONTACT", ObjectID: current.ContactID, Outcome: "SUCCESS", Sensitivity: "HIGH", Before: map[string]any{"status": current.Status, "processingRestricted": current.ProcessingRestricted}, After: map[string]any{"status": out.Status, "processingRestricted": out.ProcessingRestricted, "version": out.Version}, Reason: reason, CorrelationID: correlation, OccurredAt: now})
	}
	return out, err
}

func operatorAssignable(value Status) bool {
	switch value {
	case StatusActive, StatusInactive, StatusSuppressed, StatusInvalid, StatusRecycled, StatusDeceased:
		return true
	default:
		return false
	}
}

type MemoryRepository struct {
	mu      sync.RWMutex
	records map[string]Record
	events  map[string][]Event
}

func NewMemoryRepository(records ...Record) *MemoryRepository {
	r := &MemoryRepository{records: map[string]Record{}, events: map[string][]Event{}}
	for _, record := range records {
		r.records[record.ContactID] = record
	}
	return r
}
func (r *MemoryRepository) Get(_ context.Context, id string) (Record, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return v, nil
}
func (r *MemoryRepository) Transition(_ context.Context, v Record, expected int64, event Event) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.records[v.ContactID]
	if !ok {
		return Record{}, ErrNotFound
	}
	if current.Version != expected {
		return Record{}, ErrConflict
	}
	r.records[v.ContactID] = v
	r.events[v.ContactID] = append(r.events[v.ContactID], event)
	return v, nil
}
func (r *MemoryRepository) Events(_ context.Context, id string, limit int) ([]Event, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.records[id]; !ok {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	items := r.events[id]
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	return append([]Event(nil), items...), nil
}

func (r *MemoryRepository) ListEventPage(_ context.Context, id string, limit int, before *time.Time, beforeID string) ([]Event, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.records[id]; !ok {
		return nil, ErrNotFound
	}
	items := append([]Event(nil), r.events[id]...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].OccurredAt.Equal(items[j].OccurredAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].OccurredAt.After(items[j].OccurredAt)
	})
	out := make([]Event, 0, limit)
	for _, item := range items {
		if before != nil && (item.OccurredAt.After(*before) || (item.OccurredAt.Equal(*before) && item.ID >= beforeID)) {
			continue
		}
		out = append(out, item)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

type PostgreSQLRepository struct{ DB *sql.DB }

func (r *PostgreSQLRepository) Get(ctx context.Context, id string) (Record, error) {
	var v Record
	var status string
	err := r.DB.QueryRowContext(ctx, `SELECT id::text,masked_msisdn,status,processing_restricted,coalesce(status_reason,''),coalesce(status_updated_by::text,''),coalesce(status_updated_at,updated_at),lifecycle_version FROM contacts WHERE id=$1::uuid`, id).Scan(&v.ContactID, &v.MaskedMSISDN, &status, &v.ProcessingRestricted, &v.Reason, &v.UpdatedBy, &v.UpdatedAt, &v.Version)
	v.Status = Status(status)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	return v, err
}
func (r *PostgreSQLRepository) Transition(ctx context.Context, v Record, expected int64, event Event) (Record, error) {
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback()
	var current Record
	var status string
	err = tx.QueryRowContext(ctx, `SELECT id::text,masked_msisdn,status,processing_restricted,coalesce(status_reason,''),coalesce(status_updated_by::text,''),coalesce(status_updated_at,updated_at),lifecycle_version FROM contacts WHERE id=$1::uuid FOR UPDATE`, v.ContactID).Scan(&current.ContactID, &current.MaskedMSISDN, &status, &current.ProcessingRestricted, &current.Reason, &current.UpdatedBy, &current.UpdatedAt, &current.Version)
	current.Status = Status(status)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	if current.Version != expected || current.Status == StatusAnonymised || current.Status == StatusDeleted || current.Status == StatusDeceased && v.Status != StatusDeceased {
		return Record{}, ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO contact_profile_history(contact_id,audience_import_id,privacy_case_id,actor_id,change_reason,reported_age,age_recorded_at,gender_code,country_id,state_id,lga_id,source_record_hash,recorded_at,profile_recorded_at,source_system,source_record_id,age_source,age_verified,preferred_language_code,contact_status) SELECT id,NULL,NULL,$2::uuid,$3,reported_age,age_recorded_at,gender_code,country_id,state_id,lga_id,$4,$5,profile_recorded_at,source_system,source_record_id,age_source,age_verified,preferred_language_code,status FROM contacts WHERE id=$1::uuid`, v.ContactID, event.ActorID, event.Reason, "lifecycle:"+event.ID, event.OccurredAt); err != nil {
		return Record{}, err
	}
	updated := Record{}
	err = tx.QueryRowContext(ctx, `UPDATE contacts SET status=$3,processing_restricted=$4,status_reason=$5,status_updated_by=$6::uuid,status_updated_at=$7,lifecycle_version=lifecycle_version+1,updated_at=$7 WHERE id=$1::uuid AND lifecycle_version=$2 RETURNING id::text,masked_msisdn,status,processing_restricted,coalesce(status_reason,''),coalesce(status_updated_by::text,''),status_updated_at,lifecycle_version`, v.ContactID, expected, v.Status, v.ProcessingRestricted, v.Reason, v.UpdatedBy, v.UpdatedAt).Scan(&updated.ContactID, &updated.MaskedMSISDN, &status, &updated.ProcessingRestricted, &updated.Reason, &updated.UpdatedBy, &updated.UpdatedAt, &updated.Version)
	updated.Status = Status(status)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrConflict
	}
	if err != nil {
		return Record{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO contact_lifecycle_events(id,contact_id,previous_status,new_status,previous_processing_restricted,new_processing_restricted,actor_id,reason,contact_version,occurred_at) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7::uuid,$8,$9,$10)`, event.ID, event.ContactID, event.PreviousStatus, event.NewStatus, event.PreviousRestriction, event.NewRestriction, event.ActorID, event.Reason, updated.Version, event.OccurredAt); err != nil {
		return Record{}, err
	}
	if err = tx.Commit(); err != nil {
		return Record{}, err
	}
	return updated, nil
}
func (r *PostgreSQLRepository) Events(ctx context.Context, id string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,contact_id::text,previous_status,new_status,previous_processing_restricted,new_processing_restricted,actor_id::text,reason,contact_version,occurred_at FROM contact_lifecycle_events WHERE contact_id=$1::uuid ORDER BY occurred_at DESC,id DESC LIMIT $2`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.ContactID, &e.PreviousStatus, &e.NewStatus, &e.PreviousRestriction, &e.NewRestriction, &e.ActorID, &e.Reason, &e.ContactVersion, &e.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (r *PostgreSQLRepository) ListEventPage(ctx context.Context, id string, limit int, before *time.Time, beforeID string) ([]Event, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,contact_id::text,previous_status,new_status,previous_processing_restricted,new_processing_restricted,actor_id::text,reason,contact_version,occurred_at FROM contact_lifecycle_events WHERE contact_id=$1::uuid AND ($3::timestamptz IS NULL OR occurred_at<$3 OR (occurred_at=$3 AND id<NULLIF($4,'')::uuid)) ORDER BY occurred_at DESC,id DESC LIMIT $2`, id, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.ContactID, &event.PreviousStatus, &event.NewStatus, &event.PreviousRestriction, &event.NewRestriction, &event.ActorID, &event.Reason, &event.ContactVersion, &event.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}
