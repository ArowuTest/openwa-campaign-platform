package retention

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/shared/id"
)

type Store interface {
	ListPolicies(context.Context, Status, int) ([]Policy, error)
	GetPolicy(context.Context, string) (Policy, error)
	CreatePolicy(context.Context, Policy, Event) (Policy, error)
	UpdatePolicy(context.Context, Policy, int64, Event) (Policy, error)
	ActivatePolicy(context.Context, Policy, int64, Event) (Policy, error)
	ListEvents(context.Context, string, int) ([]Event, error)
	ScheduleDue(context.Context, time.Time, int) (int, error)
	ClaimJobs(context.Context, string, time.Time, time.Duration, int) ([]Job, error)
	CompleteJob(context.Context, Job, map[string]any, time.Time) error
	FailJob(context.Context, Job, string, string, time.Time, time.Duration) error
	HoldJob(context.Context, Job, string, map[string]any, time.Time) error
	ListJobs(context.Context, JobStatus, int) ([]Job, error)
}

type Administration struct {
	Store Store
	Clock func() time.Time
}

func (a *Administration) now() time.Time {
	if a != nil && a.Clock != nil {
		return a.Clock().UTC()
	}
	return time.Now().UTC()
}

func periodsOverlap(aStart time.Time, aEnd *time.Time, bStart time.Time, bEnd *time.Time) bool {
	if aEnd != nil && !aEnd.After(bStart) {
		return false
	}
	if bEnd != nil && !bEnd.After(aStart) {
		return false
	}
	return true
}

func validObject(v ObjectType) bool {
	switch v {
	case ObjectInboundContent, ObjectAudienceImportSource, ObjectExportObject, ObjectProviderEvent, ObjectDeliveryEvent, ObjectAuditEvent, ObjectIncident, ObjectPrivacyCase:
		return true
	}
	return false
}
func validAction(v Action) bool {
	switch v {
	case ActionArchive, ActionDelete, ActionAnonymise, ActionReviewRequired, ActionRetainIndefinitely:
		return true
	}
	return false
}

func (a *Administration) Create(ctx context.Context, v Policy, actor, reason string) (Policy, error) {
	if a == nil || a.Store == nil {
		return Policy{}, errors.New("retention store is required")
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	v.Name = strings.TrimSpace(v.Name)
	v.ScopeID = strings.TrimSpace(v.ScopeID)
	if v.Name == "" || actor == "" || len(reason) < 8 || !validObject(v.ObjectType) || !validAction(v.Action) {
		return Policy{}, ErrInvalid
	}
	if v.ScopeType == ScopePlatform {
		if v.ScopeID != "" {
			return Policy{}, ErrInvalid
		}
	} else if v.ScopeType == ScopeOrganisation {
		if v.ScopeID == "" {
			return Policy{}, ErrInvalid
		}
	} else {
		return Policy{}, ErrInvalid
	}
	// Legal holds are absolute release gates. A retention policy cannot be
	// configured to bypass them.
	v.RespectLegalHolds = true
	if v.Action == ActionRetainIndefinitely {
		v.RetentionDays = 0
	} else if v.RetentionDays < 1 || v.RetentionDays > 36500 {
		return Policy{}, ErrInvalid
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
			return Policy{}, ErrInvalid
		}
	}
	ident, err := id.New()
	if err != nil {
		return Policy{}, err
	}
	v.ID = ident
	v.Status = StatusDraft
	v.CreatedBy = actor
	v.SubmittedBy = ""
	v.ApprovedBy = ""
	v.Reason = reason
	v.Version = 1
	v.CreatedAt = now
	v.UpdatedAt = now
	ev := Event{PolicyID: v.ID, EventType: "CREATED", Version: 1, ActorID: actor, Reason: reason, Evidence: map[string]any{"objectType": v.ObjectType, "action": v.Action, "scopeType": v.ScopeType, "scopeId": v.ScopeID}, OccurredAt: now}
	return a.Store.CreatePolicy(ctx, v, ev)
}
func (a *Administration) Submit(ctx context.Context, idv string, expected int64, actor, reason string) (Policy, error) {
	v, err := a.Store.GetPolicy(ctx, strings.TrimSpace(idv))
	if err != nil {
		return Policy{}, err
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if v.Version != expected || v.Status != StatusDraft || actor == "" || len(reason) < 8 {
		return Policy{}, ErrConflict
	}
	v.Status = StatusPendingApproval
	v.SubmittedBy = actor
	v.Reason = reason
	v.Version++
	v.UpdatedAt = a.now()
	return a.Store.UpdatePolicy(ctx, v, expected, Event{PolicyID: v.ID, EventType: "SUBMITTED", Version: v.Version, ActorID: actor, Reason: reason, OccurredAt: v.UpdatedAt})
}
func (a *Administration) Decide(ctx context.Context, idv string, expected int64, approve bool, actor, reason string) (Policy, error) {
	v, err := a.Store.GetPolicy(ctx, strings.TrimSpace(idv))
	if err != nil {
		return Policy{}, err
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if v.Version != expected || v.Status != StatusPendingApproval || actor == "" || len(reason) < 8 {
		return Policy{}, ErrConflict
	}
	if actor == v.CreatedBy || actor == v.SubmittedBy {
		return Policy{}, errors.New("retention approver must be independent")
	}
	v.ApprovedBy = actor
	v.Reason = reason
	v.Version++
	v.UpdatedAt = a.now()
	eventType := "REJECTED"
	v.Status = StatusRejected
	if approve {
		v.Status = StatusActive
		eventType = "ACTIVATED"
	}
	ev := Event{PolicyID: v.ID, EventType: eventType, Version: v.Version, ActorID: actor, Reason: reason, Evidence: map[string]any{"effectiveFrom": v.EffectiveFrom, "effectiveTo": v.EffectiveTo}, OccurredAt: v.UpdatedAt}
	if approve {
		return a.Store.ActivatePolicy(ctx, v, expected, ev)
	}
	return a.Store.UpdatePolicy(ctx, v, expected, ev)
}
func (a *Administration) Retire(ctx context.Context, idv string, expected int64, actor, reason string) (Policy, error) {
	v, err := a.Store.GetPolicy(ctx, strings.TrimSpace(idv))
	if err != nil {
		return Policy{}, err
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if v.Version != expected || v.Status != StatusActive || actor == "" || len(reason) < 8 {
		return Policy{}, ErrConflict
	}
	now := a.now()
	v.Status = StatusRetired
	if v.EffectiveTo == nil || v.EffectiveTo.After(now) {
		v.EffectiveTo = &now
	}
	v.Reason = reason
	v.Version++
	v.UpdatedAt = now
	return a.Store.UpdatePolicy(ctx, v, expected, Event{PolicyID: v.ID, EventType: "RETIRED", Version: v.Version, ActorID: actor, Reason: reason, OccurredAt: now})
}
func (a *Administration) List(ctx context.Context, status Status, limit int) ([]Policy, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return a.Store.ListPolicies(ctx, status, limit)
}
func (a *Administration) Get(ctx context.Context, id string) (Policy, error) {
	return a.Store.GetPolicy(ctx, strings.TrimSpace(id))
}
func (a *Administration) Events(ctx context.Context, id string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	return a.Store.ListEvents(ctx, strings.TrimSpace(id), limit)
}
func (a *Administration) Jobs(ctx context.Context, status JobStatus, limit int) ([]Job, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return a.Store.ListJobs(ctx, status, limit)
}

type Executor interface {
	Execute(context.Context, Job, time.Time) (map[string]any, error)
}
type ReviewRequiredError struct {
	Reason   string
	Evidence map[string]any
}

func (e ReviewRequiredError) Error() string {
	if e.Reason == "" {
		return "retention action requires review"
	}
	return e.Reason
}

type Worker struct {
	Store        Store
	Executor     Executor
	WorkerID     string
	Lease        time.Duration
	Batch        int
	PollInterval time.Duration
	Clock        func() time.Time
}

func (w *Worker) now() time.Time {
	if w != nil && w.Clock != nil {
		return w.Clock().UTC()
	}
	return time.Now().UTC()
}

func (w *Worker) Process(ctx context.Context) (int, error) {
	if w == nil || w.Store == nil || w.Executor == nil || strings.TrimSpace(w.WorkerID) == "" {
		return 0, errors.New("retention worker dependencies are required")
	}
	now := w.now()
	lease := w.Lease
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	batch := w.Batch
	if batch <= 0 || batch > 500 {
		batch = 50
	}
	_, err := w.Store.ScheduleDue(ctx, now, batch*4)
	if err != nil {
		return 0, fmt.Errorf("schedule retention work: %w", err)
	}
	jobs, err := w.Store.ClaimJobs(ctx, w.WorkerID, now, lease, batch)
	if err != nil {
		return 0, err
	}
	processed := 0
	var failures []error
	for _, job := range jobs {
		executionNow := w.now()
		evidence, execErr := w.Executor.Execute(ctx, job, executionNow)
		if execErr == nil {
			if e := w.Store.CompleteJob(ctx, job, evidence, w.now()); e != nil {
				failures = append(failures, e)
			} else {
				processed++
			}
			continue
		}
		var review ReviewRequiredError
		if errors.As(execErr, &review) {
			if e := w.Store.HoldJob(ctx, job, review.Reason, review.Evidence, w.now()); e != nil {
				failures = append(failures, e)
			}
			continue
		}
		if e := w.Store.FailJob(ctx, job, "RETENTION_EXECUTION_FAILED", safeError(execErr), w.now(), time.Minute); e != nil {
			failures = append(failures, e)
		}
	}
	return processed, errors.Join(failures...)
}
func (w *Worker) Run(ctx context.Context) error {
	poll := w.PollInterval
	if poll <= 0 {
		poll = time.Minute
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		n, err := w.Process(ctx)
		if err != nil && ctx.Err() == nil {
			return err
		}
		if n > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func safeError(err error) string {
	if err == nil {
		return ""
	}
	s := strings.TrimSpace(err.Error())
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}
