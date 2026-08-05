package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/dispatch"
	"campaign-platform/internal/jobs"
)

type Status string

const (
	StatusPending    Status = "PENDING"
	StatusProcessing Status = "PROCESSING"
	StatusPublished  Status = "PUBLISHED"
	StatusFailed     Status = "FAILED"
)

var (
	ErrNotFound      = errors.New("outbox record not found")
	ErrLeaseConflict = errors.New("outbox lease conflict")
)

type Record struct {
	ID             string          `json:"id"`
	DedupKey       string          `json:"dedupKey"`
	AggregateType  string          `json:"aggregateType"`
	AggregateID    string          `json:"aggregateId"`
	EventType      string          `json:"eventType"`
	Payload        json.RawMessage `json:"payload"`
	Status         Status          `json:"status"`
	AvailableAt    time.Time       `json:"availableAt"`
	AttemptCount   int             `json:"attemptCount"`
	MaxAttempts    int             `json:"maxAttempts"`
	LeaseOwner     string          `json:"leaseOwner,omitempty"`
	LeaseExpiresAt *time.Time      `json:"leaseExpiresAt,omitempty"`
	LeaseVersion   int64           `json:"leaseVersion"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	PublishedAt    *time.Time      `json:"publishedAt,omitempty"`
	LastErrorCode  string          `json:"lastErrorCode,omitempty"`
	LastError      string          `json:"lastError,omitempty"`
}
type Repository interface {
	Claim(context.Context, string, time.Time, time.Duration, int) ([]Record, error)
	Complete(context.Context, string, string, int64, time.Time) error
	Fail(context.Context, string, string, int64, time.Time, bool, time.Duration, string, string) error
	Renew(context.Context, string, string, int64, time.Time, time.Duration) error
	Get(context.Context, string) (Record, error)
}

type QueueBackpressureError struct {
	Pending    int
	Limit      int
	RetryAfter time.Duration
}

func (e QueueBackpressureError) Error() string {
	return fmt.Sprintf("dispatch queue backpressure: %d pending jobs reached limit %d", e.Pending, e.Limit)
}

type Publisher struct {
	Outbox                 Repository
	Jobs                   *jobs.Service
	Clock                  func() time.Time
	QueueBackpressureLimit int
	BackpressureRetryAfter time.Duration
}

func (p *Publisher) Publish(ctx context.Context, record Record) error {
	if p == nil || p.Outbox == nil || p.Jobs == nil {
		return errors.New("publisher dependencies are required")
	}
	var payload map[string]any
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return PermanentError{Err: fmt.Errorf("decode outbox payload: %w", err)}
	}
	jobType := ""
	switch record.EventType {
	case "CAMPAIGN_RECIPIENT_AUTHORISED":
		jobType = dispatch.JobType
	default:
		return PermanentError{Err: errors.New("unsupported outbox event type")}
	}
	recipientID, _ := payload["campaignRecipientId"].(string)
	if recipientID == "" {
		return PermanentError{Err: errors.New("campaignRecipientId is required")}
	}
	now := time.Now().UTC()
	if p.Clock != nil {
		now = p.Clock().UTC()
	}
	if p.QueueBackpressureLimit > 0 {
		pending, countErr := p.Jobs.PendingCount(ctx, []string{jobType}, now)
		if countErr != nil {
			return fmt.Errorf("check dispatch queue backpressure: %w", countErr)
		}
		if pending >= p.QueueBackpressureLimit {
			after := p.BackpressureRetryAfter
			if after <= 0 {
				after = 5 * time.Second
			}
			return QueueBackpressureError{Pending: pending, Limit: p.QueueBackpressureLimit, RetryAfter: after}
		}
	}
	_, _, err := p.Jobs.Enqueue(ctx, jobs.EnqueueInput{Type: jobType, DedupKey: record.DedupKey, Payload: dispatch.JobPayload{CampaignRecipientID: recipientID}, Priority: 0, MaxAttempts: 8, AvailableAt: record.AvailableAt})
	return err
}

// MemoryRepository models lease semantics and is used for failure-injection tests.
type MemoryRepository struct {
	mu    sync.Mutex
	items map[string]Record
}

func NewMemoryRepository(records ...Record) *MemoryRepository {
	r := &MemoryRepository{items: map[string]Record{}}
	for _, v := range records {
		if v.MaxAttempts <= 0 {
			v.MaxAttempts = 20
		}
		if v.UpdatedAt.IsZero() {
			v.UpdatedAt = v.CreatedAt
		}
		r.items[v.ID] = clone(v)
	}
	return r
}
func (r *MemoryRepository) Claim(_ context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]Record, error) {
	if strings.TrimSpace(owner) == "" || lease <= 0 {
		return nil, errors.New("owner and lease are required")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []Record{}
	for _, v := range r.items {
		if v.AvailableAt.After(now) {
			continue
		}
		if v.Status == StatusPending || (v.Status == StatusProcessing && v.LeaseExpiresAt != nil && !v.LeaseExpiresAt.After(now)) {
			items = append(items, v)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	expires := now.UTC().Add(lease)
	for i := range items {
		v := r.items[items[i].ID]
		v.Status = StatusProcessing
		v.LeaseOwner = owner
		v.LeaseExpiresAt = &expires
		v.AttemptCount++
		v.LeaseVersion++
		v.UpdatedAt = now.UTC()
		r.items[v.ID] = v
		items[i] = clone(v)
	}
	return items, nil
}
func (r *MemoryRepository) Complete(_ context.Context, id, owner string, leaseVersion int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.items[id]
	if !ok {
		return ErrNotFound
	}
	if v.Status != StatusProcessing || v.LeaseOwner != owner || v.LeaseVersion != leaseVersion || v.LeaseExpiresAt == nil || !v.LeaseExpiresAt.After(now) {
		return ErrLeaseConflict
	}
	published := now.UTC()
	v.Status = StatusPublished
	v.PublishedAt = &published
	v.UpdatedAt = published
	v.LeaseOwner = ""
	v.LeaseExpiresAt = nil
	r.items[id] = v
	return nil
}
func (r *MemoryRepository) Fail(_ context.Context, id, owner string, leaseVersion int64, now time.Time, retryable bool, retryAfter time.Duration, code, detail string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.items[id]
	if !ok {
		return ErrNotFound
	}
	if v.Status != StatusProcessing || v.LeaseOwner != owner || v.LeaseVersion != leaseVersion || v.LeaseExpiresAt == nil || !v.LeaseExpiresAt.After(now) {
		return ErrLeaseConflict
	}
	v.LeaseOwner = ""
	v.LeaseExpiresAt = nil
	v.LastErrorCode = strings.TrimSpace(code)
	v.LastError = strings.TrimSpace(detail)
	v.UpdatedAt = now.UTC()
	if !retryable || v.AttemptCount >= v.MaxAttempts {
		v.Status = StatusFailed
	} else {
		v.Status = StatusPending
		v.AvailableAt = now.UTC().Add(retryAfter)
	}
	r.items[id] = v
	return nil
}
func (r *MemoryRepository) Renew(_ context.Context, id, owner string, leaseVersion int64, now time.Time, lease time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.items[id]
	if !ok {
		return ErrNotFound
	}
	if v.Status != StatusProcessing || v.LeaseOwner != owner || v.LeaseVersion != leaseVersion || v.LeaseExpiresAt == nil || !v.LeaseExpiresAt.After(now) {
		return ErrLeaseConflict
	}
	expires := now.UTC().Add(lease)
	v.LeaseExpiresAt = &expires
	v.UpdatedAt = now.UTC()
	r.items[id] = v
	return nil
}
func (r *MemoryRepository) Get(_ context.Context, id string) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.items[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return clone(v), nil
}
func clone(v Record) Record { v.Payload = append(json.RawMessage(nil), v.Payload...); return v }
