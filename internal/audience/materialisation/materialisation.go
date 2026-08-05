package materialisation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/segment"
	"campaign-platform/internal/shared/id"
)

type MaterialisationStatus string

const (
	MaterialisationPending   MaterialisationStatus = "PENDING"
	MaterialisationRunning   MaterialisationStatus = "RUNNING"
	MaterialisationCompleted MaterialisationStatus = "COMPLETED"
	MaterialisationFailed    MaterialisationStatus = "FAILED"
	MaterialisationCancelled MaterialisationStatus = "CANCELLED"
)

var (
	ErrMaterialisationNotFound = errors.New("audience materialisation job not found")
	ErrMaterialisationConflict = errors.New("audience materialisation job conflict")
)

type MaterialisationJob struct {
	ID                   string                    `json:"id"`
	CampaignID           string                    `json:"campaignId"`
	SegmentID            string                    `json:"segmentId,omitempty"`
	Definition           audiencefilter.Group      `json:"definition"`
	DefinitionVersion    int64                     `json:"definitionVersion"`
	Eligibility          cohort.EligibilityContext `json:"eligibility"`
	ConsentPolicyVersion string                    `json:"consentPolicyVersion"`
	ConfigurationVersion string                    `json:"configurationVersion"`
	RequestedBy          string                    `json:"requestedBy"`
	RequestFingerprint   string                    `json:"requestFingerprint"`
	Status               MaterialisationStatus     `json:"status"`
	ProcessedCount       int64                     `json:"processedCount"`
	ExpectedCount        int64                     `json:"expectedCount"`
	LastContactID        string                    `json:"lastContactId,omitempty"`
	RollingHash          string                    `json:"rollingHash"`
	SnapshotID           string                    `json:"snapshotId,omitempty"`
	AttemptCount         int                       `json:"attemptCount"`
	NextAttemptAt        time.Time                 `json:"nextAttemptAt"`
	FailureCode          string                    `json:"failureCode,omitempty"`
	FailureReference     string                    `json:"failureReference,omitempty"`
	LeaseOwner           string                    `json:"-"`
	LeaseToken           int64                     `json:"-"`
	LeaseExpiresAt       *time.Time                `json:"-"`
	Version              int64                     `json:"version"`
	RequestedAt          time.Time                 `json:"requestedAt"`
	StartedAt            *time.Time                `json:"startedAt,omitempty"`
	CompletedAt          *time.Time                `json:"completedAt,omitempty"`
	CancelledBy          string                    `json:"cancelledBy,omitempty"`
	CancellationReason   string                    `json:"cancellationReason,omitempty"`
	ProgressPercent      float64                   `json:"progressPercent"`
	UpdatedAt            time.Time                 `json:"updatedAt"`
}

type ScheduleMaterialisation struct {
	CampaignID           string
	SegmentID            string
	Definition           audiencefilter.Group
	DefinitionVersion    int64
	Eligibility          cohort.EligibilityContext
	ConsentPolicyVersion string
	ConfigurationVersion string
	RequestedBy          string
	ExpectedCount        int64
}

type MaterialisationRepository interface {
	Create(context.Context, MaterialisationJob) (MaterialisationJob, error)
	Get(context.Context, string) (MaterialisationJob, error)
	ListByCampaign(context.Context, string, int) ([]MaterialisationJob, error)
	Claim(context.Context, string, int, time.Duration, time.Time) ([]MaterialisationJob, error)
	Renew(context.Context, string, string, int64, time.Time, time.Duration) error
	AppendMembers(context.Context, string, int64, []segment.Member, string, string, int64, time.Time) (MaterialisationJob, error)
	Complete(context.Context, string, int64, segment.Snapshot, time.Time) (MaterialisationJob, error)
	Retry(context.Context, string, int64, string, string, time.Time, time.Time) error
	Fail(context.Context, string, int64, string, string, time.Time) error
	Cancel(context.Context, string, int64, string, string, time.Time) (MaterialisationJob, error)
}

type MaterialisationService struct {
	Repository MaterialisationRepository
	Clock      func() time.Time
}

func (s *MaterialisationService) Schedule(ctx context.Context, input ScheduleMaterialisation) (MaterialisationJob, error) {
	if s == nil || s.Repository == nil {
		return MaterialisationJob{}, errors.New("materialisation repository is required")
	}
	input.CampaignID = strings.TrimSpace(input.CampaignID)
	input.RequestedBy = strings.TrimSpace(input.RequestedBy)
	input.ConsentPolicyVersion = strings.TrimSpace(input.ConsentPolicyVersion)
	input.ConfigurationVersion = strings.TrimSpace(input.ConfigurationVersion)
	if input.CampaignID == "" || input.RequestedBy == "" || input.DefinitionVersion <= 0 || input.ConsentPolicyVersion == "" || input.ConfigurationVersion == "" {
		return MaterialisationJob{}, errors.New("campaign, requester, definition version and policy versions are required")
	}
	if input.ExpectedCount <= 0 {
		return MaterialisationJob{}, errors.New("expected eligible count must be positive")
	}
	builder, err := segment.NewBuilder(input.CampaignID, strings.TrimSpace(input.SegmentID), input.Definition, input.DefinitionVersion, input.ConsentPolicyVersion, input.ConfigurationVersion, input.RequestedBy)
	if err != nil {
		return MaterialisationJob{}, err
	}
	_, seedHash, _ := builder.Checkpoint()
	fingerprintPayload, err := json.Marshal(struct {
		CampaignID           string                    `json:"campaignId"`
		SegmentID            string                    `json:"segmentId,omitempty"`
		Definition           audiencefilter.Group      `json:"definition"`
		DefinitionVersion    int64                     `json:"definitionVersion"`
		Eligibility          cohort.EligibilityContext `json:"eligibility"`
		ConsentPolicyVersion string                    `json:"consentPolicyVersion"`
		ConfigurationVersion string                    `json:"configurationVersion"`
		ExpectedCount        int64                     `json:"expectedCount"`
	}{input.CampaignID, strings.TrimSpace(input.SegmentID), input.Definition, input.DefinitionVersion, input.Eligibility, input.ConsentPolicyVersion, input.ConfigurationVersion, input.ExpectedCount})
	if err != nil {
		return MaterialisationJob{}, err
	}
	fingerprintDigest := sha256.Sum256(append([]byte("audience-materialisation-v1\x00"), fingerprintPayload...))
	fingerprint := hex.EncodeToString(fingerprintDigest[:])
	identifier, err := id.New()
	if err != nil {
		return MaterialisationJob{}, err
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	job := MaterialisationJob{
		ID: identifier, CampaignID: input.CampaignID, SegmentID: strings.TrimSpace(input.SegmentID),
		Definition: input.Definition, DefinitionVersion: input.DefinitionVersion, Eligibility: input.Eligibility,
		ConsentPolicyVersion: input.ConsentPolicyVersion, ConfigurationVersion: input.ConfigurationVersion,
		RequestedBy: input.RequestedBy, RequestFingerprint: fingerprint, Status: MaterialisationPending, ExpectedCount: input.ExpectedCount,
		RollingHash: seedHash, NextAttemptAt: now, Version: 1, RequestedAt: now, UpdatedAt: now,
	}
	return s.Repository.Create(ctx, job)
}

func (s *MaterialisationService) Get(ctx context.Context, identifier string) (MaterialisationJob, error) {
	if s == nil || s.Repository == nil {
		return MaterialisationJob{}, errors.New("materialisation repository is required")
	}
	item, err := s.Repository.Get(ctx, identifier)
	return withProgress(item), err
}

func (s *MaterialisationService) List(ctx context.Context, campaignID string, limit int) ([]MaterialisationJob, error) {
	if s == nil || s.Repository == nil {
		return nil, errors.New("materialisation repository is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	items, err := s.Repository.ListByCampaign(ctx, campaignID, limit)
	for i := range items {
		items[i] = withProgress(items[i])
	}
	return items, err
}

func (s *MaterialisationService) Cancel(ctx context.Context, identifier, actor, reason string, expectedVersion int64) (MaterialisationJob, error) {
	if s == nil || s.Repository == nil {
		return MaterialisationJob{}, errors.New("materialisation repository is required")
	}
	if strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 5 || len(strings.TrimSpace(reason)) > 1000 {
		return MaterialisationJob{}, errors.New("actor and a meaningful cancellation reason are required")
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	item, err := s.Repository.Cancel(ctx, identifier, expectedVersion, strings.TrimSpace(actor), strings.TrimSpace(reason), now)
	return withProgress(item), err
}

type MaterialisationWorker struct {
	Repository    MaterialisationRepository
	Cohorts       *cohort.ExecutionService
	Snapshots     segment.Store
	WorkerID      string
	BatchSize     int
	ClaimBatch    int
	LeaseDuration time.Duration
	PollInterval  time.Duration
	RetryBackoff  time.Duration
	MaxAttempts   int
	Clock         func() time.Time
	OnError       func(MaterialisationJob, error)
	active        atomic.Int64
}

func (w *MaterialisationWorker) Active() int64 { return w.active.Load() }

func (w *MaterialisationWorker) Run(ctx context.Context) error {
	if w.Repository == nil || w.Cohorts == nil || w.Cohorts.Compiler == nil || w.Cohorts.Repository == nil || w.Snapshots == nil {
		return errors.New("materialisation worker dependencies are required")
	}
	if strings.TrimSpace(w.WorkerID) == "" {
		return errors.New("worker ID is required")
	}
	if w.BatchSize <= 0 || w.BatchSize > 10_000 {
		w.BatchSize = 5_000
	}
	if w.ClaimBatch <= 0 || w.ClaimBatch > 16 {
		w.ClaimBatch = 2
	}
	if w.LeaseDuration <= 0 {
		w.LeaseDuration = 2 * time.Minute
	}
	if w.PollInterval <= 0 {
		w.PollInterval = time.Second
	}
	if w.RetryBackoff <= 0 {
		w.RetryBackoff = 5 * time.Second
	}
	if w.MaxAttempts <= 0 {
		w.MaxAttempts = 5
	}
	ticker := time.NewTicker(w.PollInterval)
	defer ticker.Stop()
	for {
		if err := w.runOnce(ctx); err != nil && w.OnError != nil {
			w.OnError(MaterialisationJob{}, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *MaterialisationWorker) runOnce(ctx context.Context) error {
	now := w.now()
	jobs, err := w.Repository.Claim(ctx, w.WorkerID, w.ClaimBatch, w.LeaseDuration, now)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	for _, job := range jobs {
		job := job
		wg.Add(1)
		w.active.Add(1)
		go func() {
			defer wg.Done()
			defer w.active.Add(-1)
			if processErr := w.processWithLease(ctx, job); processErr != nil {
				if errors.Is(processErr, context.Canceled) || errors.Is(processErr, context.DeadlineExceeded) && ctx.Err() != nil {
					return
				}
				if w.OnError != nil {
					w.OnError(job, processErr)
				}
				failureReference := safeFailureReference(processErr)
				evidenceCtx := context.WithoutCancel(ctx)
				if job.AttemptCount < w.MaxAttempts {
					delay := w.RetryBackoff * time.Duration(job.AttemptCount)
					if delay > 5*time.Minute {
						delay = 5 * time.Minute
					}
					now := w.now()
					_ = w.Repository.Retry(evidenceCtx, job.ID, job.LeaseToken, "MATERIALISATION_RETRY", failureReference, now.Add(delay), now)
					return
				}
				_ = w.Repository.Fail(evidenceCtx, job.ID, job.LeaseToken, "MATERIALISATION_FAILED", failureReference, w.now())
			}
		}()
	}
	wg.Wait()
	return nil
}

func (w *MaterialisationWorker) processWithLease(ctx context.Context, job MaterialisationJob) error {
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- w.process(processCtx, job) }()
	interval := w.LeaseDuration / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			return err
		case now := <-ticker.C:
			if err := w.Repository.Renew(context.WithoutCancel(ctx), job.ID, w.WorkerID, job.LeaseToken, now.UTC(), w.LeaseDuration); err != nil {
				cancel()
				select {
				case <-result:
				case <-time.After(5 * time.Second):
				}
				return fmt.Errorf("renew materialisation lease: %w", err)
			}
		case <-ctx.Done():
			cancel()
			select {
			case <-result:
			case <-time.After(5 * time.Second):
			}
			return ctx.Err()
		}
	}
}

func (w *MaterialisationWorker) process(ctx context.Context, job MaterialisationJob) error {
	compiled, err := w.Cohorts.Compiler.Compile(job.Definition, job.Eligibility)
	if err != nil {
		return err
	}
	ids, err := pageMembers(ctx, w.Cohorts.Repository, compiled, job.LastContactID, w.BatchSize)
	if err != nil {
		return err
	}
	builder, err := segment.RestoreBuilder(job.CampaignID, job.SegmentID, job.Definition, job.DefinitionVersion, job.ConsentPolicyVersion, job.ConfigurationVersion, job.RequestedBy, job.LastContactID, job.RollingHash, job.ProcessedCount)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		if job.ProcessedCount != job.ExpectedCount {
			return fmt.Errorf("eligible count changed during materialisation: expected %d processed %d", job.ExpectedCount, job.ProcessedCount)
		}
		snapshot, err := builder.Finalise(w.now())
		if err != nil {
			return err
		}
		if committer, ok := w.Repository.(snapshotCommitter); ok {
			_, err = committer.CommitSnapshot(ctx, job.ID, job.LeaseToken, snapshot, w.now())
			return err
		}
		members, err := loadAllStagedMembers(ctx, w.Repository, job.ID)
		if err != nil {
			return err
		}
		stored, _, err := w.Snapshots.EnsureWithMembers(ctx, snapshot, members)
		if err != nil {
			return err
		}
		_, err = w.Repository.Complete(ctx, job.ID, job.LeaseToken, stored, w.now())
		return err
	}
	members := make([]segment.Member, 0, len(ids))
	for _, contactID := range ids {
		member := segment.Member{ContactID: contactID, EligibilityEvidenceHash: cohort.EvidenceHash(job.Definition, job.Eligibility, contactID)}
		if err := builder.Add(member.ContactID, member.EligibilityEvidenceHash); err != nil {
			return err
		}
		members = append(members, member)
	}
	lastContactID, rollingHash, processedCount := builder.Checkpoint()
	if processedCount > job.ExpectedCount {
		return fmt.Errorf("materialisation exceeded expected count %d", job.ExpectedCount)
	}
	_, err = w.Repository.AppendMembers(ctx, job.ID, job.LeaseToken, members, lastContactID, rollingHash, processedCount, w.now())
	return err
}

func (w *MaterialisationWorker) now() time.Time {
	if w.Clock != nil {
		return w.Clock().UTC()
	}
	return time.Now().UTC()
}

type snapshotCommitter interface {
	CommitSnapshot(context.Context, string, int64, segment.Snapshot, time.Time) (MaterialisationJob, error)
}

type stagedMemberReader interface {
	StagedMembers(context.Context, string) ([]segment.Member, error)
}

func loadAllStagedMembers(ctx context.Context, repository MaterialisationRepository, jobID string) ([]segment.Member, error) {
	reader, ok := repository.(stagedMemberReader)
	if !ok {
		return nil, errors.New("repository cannot read staged materialisation members")
	}
	return reader.StagedMembers(ctx, jobID)
}

type pagedQueryRepository interface {
	MembersAfter(context.Context, cohort.CompiledQuery, string, int) ([]string, error)
}

func pageMembers(ctx context.Context, repository cohort.QueryRepository, compiled cohort.CompiledQuery, after string, limit int) ([]string, error) {
	if paged, ok := repository.(pagedQueryRepository); ok {
		return paged.MembersAfter(ctx, compiled, after, limit)
	}
	ids, err := repository.Members(ctx, compiled, 10_000_001)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, limit)
	for _, value := range ids {
		if value > after {
			out = append(out, value)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func safeFailureReference(err error) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(err.Error())))
	return hex.EncodeToString(digest[:])
}

func withProgress(job MaterialisationJob) MaterialisationJob {
	if job.ExpectedCount > 0 {
		job.ProgressPercent = float64(job.ProcessedCount) * 100 / float64(job.ExpectedCount)
		if job.ProgressPercent > 100 {
			job.ProgressPercent = 100
		}
	}
	if job.Status == MaterialisationCompleted {
		job.ProgressPercent = 100
	}
	return job
}
