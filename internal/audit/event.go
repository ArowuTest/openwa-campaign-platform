package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/shared/id"
)

// Event is append-only evidence for a security or business operation. Before and
// After must contain redacted summaries rather than secrets or raw personal data.
type Event struct {
	ID             string          `json:"id"`
	Sequence       uint64          `json:"sequence"`
	ActorType      string          `json:"actorType"`
	ActorID        string          `json:"actorId"`
	Action         string          `json:"action"`
	ObjectType     string          `json:"objectType"`
	ObjectID       string          `json:"objectId"`
	OrganisationID string          `json:"organisationId,omitempty"`
	Outcome        string          `json:"outcome,omitempty"`
	Sensitivity    string          `json:"sensitivity,omitempty"`
	Before         json.RawMessage `json:"before,omitempty"`
	After          json.RawMessage `json:"after,omitempty"`
	ReasonCode     string          `json:"reasonCode,omitempty"`
	Reason         string          `json:"reason,omitempty"`
	IPAddress      string          `json:"ipAddress,omitempty"`
	Device         string          `json:"device,omitempty"`
	CorrelationID  string          `json:"correlationId"`
	OccurredAt     time.Time       `json:"occurredAt"`
	PreviousHash   string          `json:"previousHash,omitempty"`
	Hash           string          `json:"hash"`
}

type Input struct {
	ActorType      string
	ActorID        string
	Action         string
	ObjectType     string
	ObjectID       string
	OrganisationID string
	Outcome        string
	Sensitivity    string
	Before         any
	After          any
	ReasonCode     string
	Reason         string
	IPAddress      string
	Device         string
	CorrelationID  string
	OccurredAt     time.Time
}

var (
	ErrInvalidEvent        = errors.New("invalid audit event")
	ErrChainConflict       = errors.New("audit chain changed")
	ErrIntegrity           = errors.New("audit chain integrity failure")
	ErrIdempotencyConflict = errors.New("audit event idempotency conflict")
)

func New(input Input) (Event, error) {
	if strings.TrimSpace(input.ActorType) == "" || strings.TrimSpace(input.ActorID) == "" {
		return Event{}, fmt.Errorf("%w: actor type and actor ID are required", ErrInvalidEvent)
	}
	if strings.TrimSpace(input.Action) == "" || strings.TrimSpace(input.ObjectType) == "" || strings.TrimSpace(input.ObjectID) == "" {
		return Event{}, fmt.Errorf("%w: action and object identity are required", ErrInvalidEvent)
	}
	if strings.TrimSpace(input.CorrelationID) == "" {
		return Event{}, fmt.Errorf("%w: correlation ID is required", ErrInvalidEvent)
	}
	identifier, err := id.New()
	if err != nil {
		return Event{}, err
	}
	occurred := input.OccurredAt.UTC()
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	occurred = occurred.UTC().Round(time.Microsecond)
	before, err := marshalSummary(input.Before)
	if err != nil {
		return Event{}, fmt.Errorf("marshal before summary: %w", err)
	}
	after, err := marshalSummary(input.After)
	if err != nil {
		return Event{}, fmt.Errorf("marshal after summary: %w", err)
	}
	return Event{
		ID: identifier, ActorType: clean(input.ActorType), ActorID: clean(input.ActorID),
		Action: clean(input.Action), ObjectType: clean(input.ObjectType), ObjectID: clean(input.ObjectID),
		OrganisationID: clean(input.OrganisationID), Outcome: clean(input.Outcome), Sensitivity: clean(input.Sensitivity),
		Before: before, After: after, ReasonCode: clean(input.ReasonCode), Reason: clean(input.Reason),
		IPAddress: canonicalAuditIP(input.IPAddress), Device: clean(input.Device), CorrelationID: clean(input.CorrelationID),
		OccurredAt: occurred,
	}, nil
}

func marshalSummary(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(payload) > 64<<10 {
		return nil, errors.New("audit summary exceeds 64 KiB")
	}
	return payload, nil
}

func clean(value string) string { return strings.TrimSpace(value) }

func canonicalAuditIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Addr().String()
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return address.String()
	}
	return value
}

func canonicalAuditJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func calculateHash(event Event) (string, error) {
	before, err := canonicalAuditJSON(event.Before)
	if err != nil {
		return "", err
	}
	after, err := canonicalAuditJSON(event.After)
	if err != nil {
		return "", err
	}
	event.Before = before
	event.After = after
	event.IPAddress = canonicalAuditIP(event.IPAddress)
	event.OccurredAt = event.OccurredAt.UTC().Round(time.Microsecond)
	canonical := struct {
		ID             string          `json:"id"`
		Sequence       uint64          `json:"sequence"`
		ActorType      string          `json:"actorType"`
		ActorID        string          `json:"actorId"`
		Action         string          `json:"action"`
		ObjectType     string          `json:"objectType"`
		ObjectID       string          `json:"objectId"`
		OrganisationID string          `json:"organisationId,omitempty"`
		Outcome        string          `json:"outcome,omitempty"`
		Sensitivity    string          `json:"sensitivity,omitempty"`
		Before         json.RawMessage `json:"before,omitempty"`
		After          json.RawMessage `json:"after,omitempty"`
		ReasonCode     string          `json:"reasonCode,omitempty"`
		Reason         string          `json:"reason,omitempty"`
		IPAddress      string          `json:"ipAddress,omitempty"`
		Device         string          `json:"device,omitempty"`
		CorrelationID  string          `json:"correlationId"`
		OccurredAt     time.Time       `json:"occurredAt"`
		PreviousHash   string          `json:"previousHash,omitempty"`
	}{event.ID, event.Sequence, event.ActorType, event.ActorID, event.Action, event.ObjectType, event.ObjectID,
		event.OrganisationID, event.Outcome, event.Sensitivity, event.Before, event.After, event.ReasonCode, event.Reason, event.IPAddress, event.Device,
		event.CorrelationID, event.OccurredAt.UTC(), event.PreviousHash}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func sameEventIntent(left, right Event) bool {
	return left.ID == right.ID &&
		left.ActorType == right.ActorType && left.ActorID == right.ActorID &&
		left.Action == right.Action && left.ObjectType == right.ObjectType && left.ObjectID == right.ObjectID &&
		left.OrganisationID == right.OrganisationID && left.Outcome == right.Outcome && left.Sensitivity == right.Sensitivity &&
		string(left.Before) == string(right.Before) && string(left.After) == string(right.After) &&
		left.ReasonCode == right.ReasonCode && left.Reason == right.Reason &&
		left.IPAddress == right.IPAddress && left.Device == right.Device &&
		left.CorrelationID == right.CorrelationID && left.OccurredAt.UTC().Equal(right.OccurredAt.UTC())
}

type Query struct {
	AfterSequence  uint64
	ActorID        string
	Action         string
	ObjectType     string
	ObjectID       string
	OrganisationID string
	Outcome        string
	Sensitivity    string
	CorrelationID  string
	IPAddress      string
	From           *time.Time
	To             *time.Time
	Limit          int
}

type Page struct {
	Items        []Event `json:"items"`
	NextSequence uint64  `json:"nextSequence,omitempty"`
}

type Repository interface {
	Append(context.Context, Event, uint64, string) (Event, error)
	List(context.Context, uint64, int) ([]Event, error)
	Search(context.Context, Query) (Page, error)
	Verify(context.Context) error
	Head(context.Context) (uint64, string, error)
}

type Recorder struct {
	repository Repository
	clock      func() time.Time
	mu         sync.Mutex
}

const (
	auditChainConflictMaxAttempts = 32
	auditChainConflictBaseDelay   = time.Millisecond
	auditChainConflictMaxDelay    = 50 * time.Millisecond
)

func NewRecorder(repository Repository) *Recorder {
	return &Recorder{repository: repository, clock: time.Now}
}

func (r *Recorder) Record(ctx context.Context, input Input) (Event, error) {
	if input.OccurredAt.IsZero() {
		input.OccurredAt = r.clock().UTC()
	}
	event, err := New(input)
	if err != nil {
		return Event{}, err
	}
	return r.RecordPrepared(ctx, event)
}

// RecordPrepared appends an already-identified event, such as one recovered
// from the transactional audit outbox. The event ID is preserved so replay is
// idempotent across worker crashes and outbox redelivery.
func (r *Recorder) RecordPrepared(ctx context.Context, event Event) (Event, error) {
	if r == nil || r.repository == nil || strings.TrimSpace(event.ID) == "" || strings.TrimSpace(event.ActorType) == "" ||
		strings.TrimSpace(event.ActorID) == "" || strings.TrimSpace(event.Action) == "" || strings.TrimSpace(event.ObjectType) == "" ||
		strings.TrimSpace(event.ObjectID) == "" || strings.TrimSpace(event.CorrelationID) == "" || event.OccurredAt.IsZero() ||
		event.Sequence != 0 || event.PreviousHash != "" || event.Hash != "" {
		return Event{}, ErrInvalidEvent
	}
	var err error
	event.Before, err = canonicalAuditJSON(event.Before)
	if err != nil {
		return Event{}, fmt.Errorf("canonicalise audit before summary: %w", err)
	}
	event.After, err = canonicalAuditJSON(event.After)
	if err != nil {
		return Event{}, fmt.Errorf("canonicalise audit after summary: %w", err)
	}
	event.IPAddress = canonicalAuditIP(event.IPAddress)
	event.OccurredAt = event.OccurredAt.UTC().Round(time.Microsecond)

	// A hash chain is inherently sequential. Serialising through one recorder
	// avoids same-process writers repeatedly invalidating each other's observed
	// head while PostgreSQL still provides the cross-process CAS boundary.
	r.mu.Lock()
	defer r.mu.Unlock()
	for attempt := 0; attempt < auditChainConflictMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return Event{}, err
		}
		sequence, hash, err := r.repository.Head(ctx)
		if err != nil {
			return Event{}, err
		}
		stored, err := r.repository.Append(ctx, event, sequence, hash)
		if !errors.Is(err, ErrChainConflict) {
			return stored, err
		}
		if attempt == auditChainConflictMaxAttempts-1 {
			return Event{}, ErrChainConflict
		}
		if err := waitForAuditChainRetry(ctx, attempt); err != nil {
			return Event{}, err
		}
	}
	return Event{}, ErrChainConflict
}

func waitForAuditChainRetry(ctx context.Context, attempt int) error {
	delay := auditChainConflictBaseDelay << attempt
	if delay <= 0 || delay > auditChainConflictMaxDelay {
		delay = auditChainConflictMaxDelay
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// MemoryRepository is primarily for tests and local development. The PostgreSQL
// implementation must serialise append operations in a transaction and enforce
// unique sequence/hash values.
type MemoryRepository struct {
	mu     sync.RWMutex
	events []Event
}

func NewMemoryRepository() *MemoryRepository { return &MemoryRepository{} }

func (r *MemoryRepository) Head(_ context.Context) (uint64, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.events) == 0 {
		return 0, "", nil
	}
	head := r.events[len(r.events)-1]
	return head.Sequence, head.Hash, nil
}

func (r *MemoryRepository) Append(_ context.Context, event Event, expectedSequence uint64, expectedHash string) (Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.events {
		if existing.ID != event.ID {
			continue
		}
		if !sameEventIntent(existing, event) {
			return Event{}, ErrIdempotencyConflict
		}
		return clone(existing), nil
	}
	var currentSequence uint64
	var currentHash string
	if len(r.events) > 0 {
		head := r.events[len(r.events)-1]
		currentSequence, currentHash = head.Sequence, head.Hash
	}
	if currentSequence != expectedSequence || currentHash != expectedHash {
		return Event{}, ErrChainConflict
	}
	event.Sequence = currentSequence + 1
	event.PreviousHash = currentHash
	hash, err := calculateHash(event)
	if err != nil {
		return Event{}, err
	}
	event.Hash = hash
	r.events = append(r.events, clone(event))
	return clone(event), nil
}

func (r *MemoryRepository) List(ctx context.Context, after uint64, limit int) ([]Event, error) {
	page, err := r.Search(ctx, Query{AfterSequence: after, Limit: limit})
	return page.Items, err
}

func (r *MemoryRepository) Search(_ context.Context, query Query) (Page, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	limit := query.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	out := make([]Event, 0, limit)
	var next uint64
	for _, event := range r.events {
		if event.Sequence <= query.AfterSequence || !matchesQuery(event, query) {
			continue
		}
		if len(out) == limit {
			next = out[len(out)-1].Sequence
			break
		}
		out = append(out, clone(event))
	}
	return Page{Items: out, NextSequence: next}, nil
}

func matchesQuery(event Event, query Query) bool {
	if value := strings.TrimSpace(query.ActorID); value != "" && event.ActorID != value {
		return false
	}
	if value := strings.TrimSpace(query.Action); value != "" && event.Action != value {
		return false
	}
	if value := strings.TrimSpace(query.ObjectType); value != "" && event.ObjectType != value {
		return false
	}
	if value := strings.TrimSpace(query.ObjectID); value != "" && event.ObjectID != value {
		return false
	}
	if value := strings.TrimSpace(query.OrganisationID); value != "" && event.OrganisationID != value {
		return false
	}
	if value := strings.TrimSpace(query.Outcome); value != "" && !strings.EqualFold(event.Outcome, value) {
		return false
	}
	if value := strings.TrimSpace(query.Sensitivity); value != "" && !strings.EqualFold(event.Sensitivity, value) {
		return false
	}
	if value := strings.TrimSpace(query.CorrelationID); value != "" && event.CorrelationID != value {
		return false
	}
	if value := strings.TrimSpace(query.IPAddress); value != "" && event.IPAddress != value {
		return false
	}
	if query.From != nil && event.OccurredAt.Before(query.From.UTC()) {
		return false
	}
	if query.To != nil && !event.OccurredAt.Before(query.To.UTC()) {
		return false
	}
	return true
}

func (r *MemoryRepository) Verify(_ context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var previous string
	for index, event := range r.events {
		if event.Sequence != uint64(index+1) || event.PreviousHash != previous {
			return fmt.Errorf("%w at sequence %d", ErrIntegrity, event.Sequence)
		}
		expected, err := calculateHash(event)
		if err != nil || expected != event.Hash {
			return fmt.Errorf("%w at sequence %d", ErrIntegrity, event.Sequence)
		}
		previous = event.Hash
	}
	return nil
}

func clone(event Event) Event {
	event.Before = append(json.RawMessage(nil), event.Before...)
	event.After = append(json.RawMessage(nil), event.After...)
	return event
}
