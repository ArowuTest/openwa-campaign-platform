package cohort

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"campaign-platform/internal/consent"
	"campaign-platform/internal/jobs"
	"campaign-platform/internal/organisation"
)

type EstimateRepository interface {
	Schedule(context.Context, EstimateJobRequest, time.Time) (EstimateJobRecord, bool, error)
	Get(context.Context, string) (EstimateJobRecord, error)
	ListPage(context.Context, string, int, string) (EstimateJobPage, error)
	StoreResult(context.Context, string, string, int64, Estimate, EstimateGovernanceEvidence, time.Time) error
}

type EstimateService struct {
	Repository EstimateRepository
	Compiler   *Compiler
	Clock      func() time.Time
}

func (s *EstimateService) now() time.Time {
	if s != nil && s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func (s *EstimateService) Schedule(ctx context.Context, input EstimateJobRequest, hasPermission func(string) bool) (EstimateJobRecord, bool, error) {
	if s == nil || s.Repository == nil || s.Compiler == nil {
		return EstimateJobRecord{}, false, errors.New("cohort estimate service is unavailable")
	}
	now := s.now()
	normalized, err := normalizeEstimateRequest(input)
	if err != nil {
		return EstimateJobRecord{}, false, err
	}
	if _, err := s.Compiler.CompileBreakdownForPermissions(normalized.Definition, EligibilityContext{
		OrganisationID: normalized.OrganisationID,
		PurposeID:      normalized.PurposeID,
		Channel:        normalized.Channel,
		AsOf:           now,
	}, hasPermission); err != nil {
		return EstimateJobRecord{}, false, err
	}
	return s.Repository.Schedule(ctx, normalized, now)
}

func (s *EstimateService) Get(ctx context.Context, identifier string) (EstimateJobRecord, error) {
	if s == nil || s.Repository == nil {
		return EstimateJobRecord{}, errors.New("cohort estimate service is unavailable")
	}
	return s.Repository.Get(ctx, strings.TrimSpace(identifier))
}

func (s *EstimateService) ListPage(ctx context.Context, organisationID string, limit int, cursor string) (EstimateJobPage, error) {
	if s == nil || s.Repository == nil {
		return EstimateJobPage{}, errors.New("cohort estimate service is unavailable")
	}
	return s.Repository.ListPage(ctx, strings.TrimSpace(organisationID), limit, cursor)
}

type EstimateEvidenceResolver interface {
	ResolveEstimateEvidence(context.Context, EstimateJobRecord) (EstimateGovernanceEvidence, error)
}

type GovernanceEvidenceResolver struct {
	Purposes *consent.PurposeService
	Reviews  *consent.Service
	Policies *organisation.PolicyAdministration
}

func (r *GovernanceEvidenceResolver) ResolveEstimateEvidence(ctx context.Context, record EstimateJobRecord) (EstimateGovernanceEvidence, error) {
	if r == nil || r.Purposes == nil || r.Reviews == nil || r.Policies == nil || r.Policies.Store == nil {
		return EstimateGovernanceEvidence{}, errors.New("cohort estimate governance evidence resolver is unavailable")
	}
	purpose, err := r.Purposes.Get(ctx, record.PurposeID)
	if err != nil {
		return EstimateGovernanceEvidence{}, err
	}
	if !purpose.Active || purpose.OrganisationID != record.OrganisationID || strings.ToUpper(strings.TrimSpace(purpose.Channel)) != record.Channel {
		return EstimateGovernanceEvidence{}, errors.New("cohort estimate purpose is not active for the requested organisation/channel")
	}
	if strings.TrimSpace(purpose.ConsentReviewID) == "" {
		return EstimateGovernanceEvidence{}, errors.New("cohort estimate purpose has no consent-review evidence")
	}
	review, err := r.Reviews.Get(ctx, purpose.ConsentReviewID)
	if err != nil {
		return EstimateGovernanceEvidence{}, err
	}
	if review.OrganisationID != record.OrganisationID || review.Status != consent.StatusApproved ||
		strings.ToUpper(strings.TrimSpace(review.Channel)) != record.Channel {
		return EstimateGovernanceEvidence{}, errors.New("cohort estimate consent review is not approved for the requested organisation/channel")
	}
	if review.ExpiresAt != nil && !record.AsOf.Before(review.ExpiresAt.UTC()) {
		return EstimateGovernanceEvidence{}, errors.New("cohort estimate consent review was expired at the estimate as-of time")
	}
	if strings.TrimSpace(purpose.WordingVersion) == "" || strings.TrimSpace(review.WordingVersion) == "" ||
		purpose.WordingVersion != review.WordingVersion {
		return EstimateGovernanceEvidence{}, errors.New("cohort estimate purpose/review wording evidence is inconsistent")
	}
	policy, err := r.Policies.Store.Active(ctx, record.OrganisationID, record.AsOf)
	if err != nil {
		return EstimateGovernanceEvidence{}, err
	}
	if policy.Version <= 0 || strings.TrimSpace(policy.ID) == "" {
		return EstimateGovernanceEvidence{}, errors.New("active organisation policy evidence is incomplete")
	}
	return EstimateGovernanceEvidence{
		ConsentReviewID:           review.ID,
		ConsentReviewVersion:      review.Version,
		ConsentWordingVersion:     review.WordingVersion,
		OrganisationPolicyID:      policy.ID,
		OrganisationPolicyVersion: policy.Version,
	}, nil
}

type EstimateWorker struct {
	Queue         jobs.Repository
	Estimates     EstimateRepository
	Cohorts       *ExecutionService
	Evidence      EstimateEvidenceResolver
	WorkerID      string
	ClaimBatch    int
	LeaseDuration time.Duration
	PollInterval  time.Duration
	RetryBackoff  time.Duration
	Clock         func() time.Time
	OnError       func(jobs.Job, error)
	active        atomic.Int64
}

func (w *EstimateWorker) now() time.Time {
	if w != nil && w.Clock != nil {
		return w.Clock().UTC()
	}
	return time.Now().UTC()
}

func (w *EstimateWorker) leaseDuration() time.Duration {
	if w.LeaseDuration <= 0 {
		return 5 * time.Minute
	}
	return w.LeaseDuration
}

func (w *EstimateWorker) claimBatch() int {
	if w.ClaimBatch <= 0 || w.ClaimBatch > 100 {
		return 5
	}
	return w.ClaimBatch
}

func (w *EstimateWorker) Active() int64 {
	if w == nil {
		return 0
	}
	return w.active.Load()
}

func (w *EstimateWorker) validate() error {
	if w == nil || w.Queue == nil || w.Estimates == nil || w.Cohorts == nil || w.Evidence == nil {
		return errors.New("cohort estimate worker dependencies are required")
	}
	if strings.TrimSpace(w.WorkerID) == "" {
		return errors.New("cohort estimate worker ID is required")
	}
	return nil
}

func (w *EstimateWorker) Process(ctx context.Context) (int, error) {
	if err := w.validate(); err != nil {
		return 0, err
	}
	processed := 0
	var processErrors []error
	// Execution is serial: claim only when ready to process, so no waiting
	// jobs hold leases that this worker cannot yet maintain.
	for attempt := 0; attempt < w.claimBatch(); attempt++ {
		if err := ctx.Err(); err != nil {
			processErrors = append(processErrors, err)
			return processed, errors.Join(processErrors...)
		}
		claimed, err := w.Queue.Claim(ctx, w.WorkerID, w.now(), w.leaseDuration(), 1, []string{EstimateJobType})
		if err != nil {
			processErrors = append(processErrors, err)
			return processed, errors.Join(processErrors...)
		}
		if len(claimed) == 0 {
			break
		}
		job := claimed[0]
		w.active.Add(1)
		err = w.processJob(ctx, job)
		w.active.Add(-1)
		if err != nil {
			if w.OnError != nil {
				w.OnError(job, err)
			}
			processErrors = append(processErrors, err)
			continue
		}
		processed++
	}
	return processed, errors.Join(processErrors...)
}

func (w *EstimateWorker) processJob(ctx context.Context, job jobs.Job) error {
	record, err := w.Estimates.Get(ctx, job.ID)
	if err != nil {
		return w.failJob(ctx, job, err)
	}
	// Crash recovery: result persistence is fenced and durable. If the process
	// died before marking the generic job complete, a new lease completes it
	// without rerunning the potentially expensive query.
	if record.Result != nil {
		return w.Queue.Complete(ctx, job.ID, job.LeaseOwner, job.LeaseVersion, w.now())
	}

	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	heartbeatErr := make(chan error, 1)
	done := make(chan struct{})
	go w.heartbeat(processCtx, cancel, job, heartbeatErr, done)
	defer func() {
		cancel()
		<-done
	}()

	evidence, err := w.Evidence.ResolveEstimateEvidence(processCtx, record)
	if err != nil {
		return w.failJob(processCtx, job, err)
	}
	result, err := w.Cohorts.Estimate(processCtx, record.Definition, EligibilityContext{
		OrganisationID: record.OrganisationID,
		PurposeID:      record.PurposeID,
		Channel:        record.Channel,
		AsOf:           record.AsOf,
	}, nil)
	if err != nil {
		select {
		case leaseErr := <-heartbeatErr:
			return leaseErr
		default:
		}
		return w.failJob(processCtx, job, err)
	}
	select {
	case leaseErr := <-heartbeatErr:
		return leaseErr
	default:
	}
	// Counting and governance resolution use separate reads. Fail closed if
	// policy/review/wording evidence changed or became unavailable during the
	// count, rather than publishing a result labelled with stale evidence.
	currentEvidence, err := w.Evidence.ResolveEstimateEvidence(processCtx, record)
	if err != nil {
		return w.failJob(processCtx, job, err)
	}
	if currentEvidence != evidence {
		return w.failJob(processCtx, job, errors.New("cohort estimate governance evidence changed during calculation"))
	}
	now := w.now()
	if err := w.Estimates.StoreResult(processCtx, record.ID, job.LeaseOwner, job.LeaseVersion, result, evidence, now); err != nil {
		return err
	}
	return w.Queue.Complete(processCtx, job.ID, job.LeaseOwner, job.LeaseVersion, w.now())
}

func (w *EstimateWorker) heartbeat(ctx context.Context, cancel context.CancelFunc, job jobs.Job, failures chan<- error, done chan<- struct{}) {
	defer close(done)
	interval := w.leaseDuration() / 3
	if interval < time.Second {
		interval = time.Second
	}
	if interval > 30*time.Second {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	renewTimeout := w.leaseDuration() / 2
	if renewTimeout < 100*time.Millisecond {
		renewTimeout = 100 * time.Millisecond
	}
	if renewTimeout > 10*time.Second {
		renewTimeout = 10 * time.Second
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := w.now()
			renewCtx, renewCancel := context.WithTimeout(ctx, renewTimeout)
			err := w.Queue.Renew(renewCtx, job.ID, job.LeaseOwner, job.LeaseVersion, now, w.leaseDuration())
			renewCancel()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				select {
				case failures <- err:
				default:
				}
				cancel()
				return
			}
		}
	}
}

func (w *EstimateWorker) failJob(ctx context.Context, job jobs.Job, cause error) error {
	backoff := w.RetryBackoff
	if backoff <= 0 {
		backoff = 5 * time.Second
	}
	failCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.Queue.Fail(failCtx, job.ID, job.LeaseOwner, job.LeaseVersion, w.now(), true, backoff, "COHORT_ESTIMATE_FAILED", cause.Error()); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (w *EstimateWorker) Run(ctx context.Context) error {
	if err := w.validate(); err != nil {
		return err
	}
	poll := w.PollInterval
	if poll <= 0 {
		poll = time.Second
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		processed, err := w.Process(ctx)
		if err != nil && w.OnError == nil {
			return err
		}
		if processed > 0 {
			continue
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
