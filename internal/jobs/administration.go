package jobs

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrAdministrativeConflict = errors.New("job is not eligible for the requested administrative action")

type Query struct {
	Statuses []Status
	Types    []string
	Limit    int
}

type QueueSummary struct {
	AsAt             time.Time        `json:"asAt"`
	CountsByStatus   map[Status]int64 `json:"countsByStatus"`
	CountsByType     map[string]int64 `json:"countsByType"`
	OldestPendingAt  *time.Time       `json:"oldestPendingAt,omitempty"`
	ProcessingLeases int64            `json:"processingLeases"`
	ExpiredLeases    int64            `json:"expiredLeases"`
}

type AdministrationEvent struct {
	ID         string    `json:"id"`
	JobID      string    `json:"jobId"`
	Action     string    `json:"action"`
	ActorID    string    `json:"actorId"`
	Reason     string    `json:"reason"`
	Previous   Status    `json:"previousStatus"`
	Current    Status    `json:"currentStatus"`
	OccurredAt time.Time `json:"occurredAt"`
}

type AdministrationRepository interface {
	List(context.Context, Query) ([]Job, error)
	Get(context.Context, string) (Job, error)
	Summary(context.Context, time.Time) (QueueSummary, error)
	RetryDeadLetter(context.Context, string, string, string, time.Time) (Job, error)
	CancelPending(context.Context, string, string, string, time.Time) (Job, error)
	Events(context.Context, string, int) ([]AdministrationEvent, error)
}

type AdministrationService struct {
	Repository AdministrationRepository
	Clock      func() time.Time
}

func (s *AdministrationService) now() time.Time {
	if s != nil && s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
func (s *AdministrationService) List(ctx context.Context, q Query) ([]Job, error) {
	if s == nil || s.Repository == nil {
		return nil, errors.New("job administration repository is required")
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 100
	}
	return s.Repository.List(ctx, q)
}
func (s *AdministrationService) Get(ctx context.Context, id string) (Job, error) {
	if s == nil || s.Repository == nil {
		return Job{}, errors.New("job administration repository is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Job{}, errors.New("job id is required")
	}
	return s.Repository.Get(ctx, id)
}
func (s *AdministrationService) Summary(ctx context.Context) (QueueSummary, error) {
	if s == nil || s.Repository == nil {
		return QueueSummary{}, errors.New("job administration repository is required")
	}
	return s.Repository.Summary(ctx, s.now())
}
func (s *AdministrationService) Retry(ctx context.Context, id, actor, reason string) (Job, error) {
	if err := validateAdministrativeAction(id, actor, reason); err != nil {
		return Job{}, err
	}
	return s.Repository.RetryDeadLetter(ctx, id, actor, reason, s.now())
}
func (s *AdministrationService) Cancel(ctx context.Context, id, actor, reason string) (Job, error) {
	if err := validateAdministrativeAction(id, actor, reason); err != nil {
		return Job{}, err
	}
	return s.Repository.CancelPending(ctx, id, actor, reason, s.now())
}
func (s *AdministrationService) Events(ctx context.Context, id string, limit int) ([]AdministrationEvent, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("job id is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.Repository.Events(ctx, id, limit)
}
func validateAdministrativeAction(id, actor, reason string) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(actor) == "" {
		return errors.New("job id and actor are required")
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 8 || len(reason) > 1000 {
		return errors.New("administrative reason must contain between 8 and 1000 characters")
	}
	return nil
}
