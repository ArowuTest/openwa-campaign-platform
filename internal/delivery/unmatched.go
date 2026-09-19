package delivery

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

type UnmatchedEvent struct {
	ProviderEventID   string
	ProviderMessageID string
	ClientReference   string
	EventType         string
	Payload           []byte
	OccurredAt        time.Time
	ReceivedAt        time.Time
}

type UnmatchedEventStore interface {
	Enqueue(context.Context, UnmatchedEvent) error
	Match(context.Context, UnmatchedEvent) (bool, error)
	List(context.Context, int) ([]UnmatchedEvent, error)
	Resolve(context.Context, string, time.Time) error
}

type MemoryUnmatchedEventStore struct {
	mu       sync.Mutex
	items    map[string]UnmatchedEvent
	resolved map[string]bool
}

func NewMemoryUnmatchedEventStore() *MemoryUnmatchedEventStore {
	return &MemoryUnmatchedEventStore{items: map[string]UnmatchedEvent{}, resolved: map[string]bool{}}
}

func (s *MemoryUnmatchedEventStore) Enqueue(_ context.Context, value UnmatchedEvent) error {
	if s == nil {
		return errors.New("unmatched event store is required")
	}
	value.ProviderEventID = strings.TrimSpace(value.ProviderEventID)
	if value.ProviderEventID == "" {
		return errors.New("provider event id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = map[string]UnmatchedEvent{}
	}
	if s.resolved == nil {
		s.resolved = map[string]bool{}
	}
	if existing, exists := s.items[value.ProviderEventID]; exists {
		if !sameUnmatchedEventEvidence(existing, value) {
			return ErrEventDedupMismatch
		}
		return nil
	}
	value.Payload = append([]byte(nil), value.Payload...)
	s.items[value.ProviderEventID] = value
	return nil
}

type PostgreSQLUnmatchedEventStore struct{ DB *sql.DB }

func (s *PostgreSQLUnmatchedEventStore) Enqueue(ctx context.Context, value UnmatchedEvent) error {
	if s == nil || s.DB == nil {
		return errors.New("database is required")
	}
	value.ProviderEventID = strings.TrimSpace(value.ProviderEventID)
	if value.ProviderEventID == "" {
		return errors.New("provider event id is required")
	}
	providerMessageID := strings.TrimSpace(value.ProviderMessageID)
	clientReference := strings.TrimSpace(value.ClientReference)
	result, err := s.DB.ExecContext(ctx, `INSERT INTO unmatched_delivery_events(provider_event_id,provider_message_id,client_reference,event_type,payload,occurred_at,received_at) VALUES($1,NULLIF($2,''),NULLIF($3,''),$4,$5::jsonb,$6,$7) ON CONFLICT(provider_event_id) DO NOTHING`, value.ProviderEventID, providerMessageID, clientReference, value.EventType, string(value.Payload), value.OccurredAt.UTC(), value.ReceivedAt.UTC())
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if inserted == 1 {
		return nil
	}
	var same bool
	row := s.DB.QueryRowContext(ctx, `SELECT
		(provider_message_id IS NOT DISTINCT FROM NULLIF($2,''))
		AND (client_reference IS NOT DISTINCT FROM NULLIF($3,''))
		AND event_type=$4
		AND payload=$5::jsonb
		AND occurred_at=$6
		FROM unmatched_delivery_events WHERE provider_event_id=$1`, value.ProviderEventID, providerMessageID, clientReference, value.EventType, string(value.Payload), value.OccurredAt.UTC())
	if err := row.Scan(&same); err != nil {
		return err
	}
	if !same {
		return ErrEventDedupMismatch
	}
	return nil
}

func (s *PostgreSQLUnmatchedEventStore) Match(ctx context.Context, value UnmatchedEvent) (bool, error) {
	if s == nil || s.DB == nil {
		return false, errors.New("database is required")
	}
	value.ProviderEventID = strings.TrimSpace(value.ProviderEventID)
	if value.ProviderEventID == "" {
		return false, errors.New("provider event id is required")
	}
	providerMessageID := strings.TrimSpace(value.ProviderMessageID)
	clientReference := strings.TrimSpace(value.ClientReference)
	var same bool
	err := s.DB.QueryRowContext(ctx, `SELECT
		(provider_message_id IS NOT DISTINCT FROM NULLIF($2,''))
		AND (client_reference IS NOT DISTINCT FROM NULLIF($3,''))
		AND event_type=$4
		AND payload=$5::jsonb
		AND occurred_at=$6
		FROM unmatched_delivery_events WHERE provider_event_id=$1`,
		value.ProviderEventID, providerMessageID, clientReference, value.EventType, string(value.Payload), value.OccurredAt.UTC()).Scan(&same)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !same {
		return false, ErrEventDedupMismatch
	}
	return true, nil
}

func (s *PostgreSQLUnmatchedEventStore) Resolve(ctx context.Context, providerEventID string, resolvedAt time.Time) error {
	if s == nil || s.DB == nil {
		return errors.New("database is required")
	}
	providerEventID = strings.TrimSpace(providerEventID)
	if providerEventID == "" || resolvedAt.IsZero() {
		return errors.New("provider event id and resolution time are required")
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE unmatched_delivery_events SET status='RESOLVED',resolved_at=COALESCE(resolved_at,$2) WHERE provider_event_id=$1`, providerEventID, resolvedAt.UTC())
	return err
}

func (s *PostgreSQLUnmatchedEventStore) List(ctx context.Context, limit int) ([]UnmatchedEvent, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT provider_event_id,coalesce(provider_message_id,''),coalesce(client_reference,''),event_type,payload,occurred_at,received_at FROM unmatched_delivery_events WHERE status='PENDING' ORDER BY received_at,provider_event_id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UnmatchedEvent
	for rows.Next() {
		var v UnmatchedEvent
		if err := rows.Scan(&v.ProviderEventID, &v.ProviderMessageID, &v.ClientReference, &v.EventType, &v.Payload, &v.OccurredAt, &v.ReceivedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func sameUnmatchedEventEvidence(left, right UnmatchedEvent) bool {
	if strings.TrimSpace(left.ProviderEventID) != strings.TrimSpace(right.ProviderEventID) ||
		strings.TrimSpace(left.ProviderMessageID) != strings.TrimSpace(right.ProviderMessageID) ||
		strings.TrimSpace(left.ClientReference) != strings.TrimSpace(right.ClientReference) ||
		left.EventType != right.EventType || !left.OccurredAt.UTC().Equal(right.OccurredAt.UTC()) {
		return false
	}
	return bytes.Equal(canonicalUnmatchedPayload(left.Payload), canonicalUnmatchedPayload(right.Payload))
}

func canonicalUnmatchedPayload(payload []byte) []byte {
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return bytes.TrimSpace(payload)
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return bytes.TrimSpace(payload)
	}
	return canonical
}

func (s *MemoryUnmatchedEventStore) Match(_ context.Context, value UnmatchedEvent) (bool, error) {
	if s == nil {
		return false, errors.New("unmatched event store is required")
	}
	value.ProviderEventID = strings.TrimSpace(value.ProviderEventID)
	if value.ProviderEventID == "" {
		return false, errors.New("provider event id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, exists := s.items[value.ProviderEventID]
	if !exists {
		return false, nil
	}
	if !sameUnmatchedEventEvidence(existing, value) {
		return false, ErrEventDedupMismatch
	}
	return true, nil
}

func (s *MemoryUnmatchedEventStore) Resolve(_ context.Context, providerEventID string, resolvedAt time.Time) error {
	if s == nil {
		return errors.New("unmatched event store is required")
	}
	providerEventID = strings.TrimSpace(providerEventID)
	if providerEventID == "" || resolvedAt.IsZero() {
		return errors.New("provider event id and resolution time are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.items[providerEventID]; exists {
		if s.resolved == nil {
			s.resolved = map[string]bool{}
		}
		s.resolved[providerEventID] = true
	}
	return nil
}

func (s *MemoryUnmatchedEventStore) List(_ context.Context, limit int) ([]UnmatchedEvent, error) {
	if s == nil {
		return nil, errors.New("unmatched event store is required")
	}
	if limit <= 0 {
		limit = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]UnmatchedEvent, 0, len(s.items))
	for eventID, v := range s.items {
		if s.resolved[eventID] {
			continue
		}
		v.Payload = append([]byte(nil), v.Payload...)
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ReceivedAt.Equal(out[j].ReceivedAt) {
			return out[i].ProviderEventID < out[j].ProviderEventID
		}
		return out[i].ReceivedAt.Before(out[j].ReceivedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
