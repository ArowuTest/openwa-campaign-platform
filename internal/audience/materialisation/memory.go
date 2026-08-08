package materialisation

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"campaign-platform/internal/segment"
)

type MemoryMaterialisationRepository struct {
	mu            sync.Mutex
	jobs          map[string]MaterialisationJob
	members       map[string][]segment.Member
	byFingerprint map[string]string
}

func NewMemoryMaterialisationRepository() *MemoryMaterialisationRepository {
	return &MemoryMaterialisationRepository{jobs: map[string]MaterialisationJob{}, members: map[string][]segment.Member{}, byFingerprint: map[string]string{}}
}

func (r *MemoryMaterialisationRepository) Create(_ context.Context, job MaterialisationJob) (MaterialisationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existingID, exists := r.byFingerprint[job.RequestFingerprint]; exists {
		return r.jobs[existingID], nil
	}
	for _, existing := range r.jobs {
		if existing.CampaignID == job.CampaignID && (existing.Status == MaterialisationPending || existing.Status == MaterialisationRunning) {
			return MaterialisationJob{}, ErrMaterialisationConflict
		}
	}
	if _, exists := r.jobs[job.ID]; exists {
		return MaterialisationJob{}, ErrMaterialisationConflict
	}
	r.jobs[job.ID] = job
	r.byFingerprint[job.RequestFingerprint] = job.ID
	return job, nil
}

func (r *MemoryMaterialisationRepository) Get(_ context.Context, identifier string) (MaterialisationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, exists := r.jobs[identifier]
	if !exists {
		return MaterialisationJob{}, ErrMaterialisationNotFound
	}
	return job, nil
}

func (r *MemoryMaterialisationRepository) ListByCampaign(_ context.Context, campaignID string, limit int) ([]MaterialisationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []MaterialisationJob{}
	for _, job := range r.jobs {
		if campaignID == "" || job.CampaignID == campaignID {
			items = append(items, job)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].RequestedAt.After(items[j].RequestedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *MemoryMaterialisationRepository) ListByCampaignPage(_ context.Context, campaignID string, limit int, before *time.Time, beforeID string) ([]MaterialisationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []MaterialisationJob{}
	for _, job := range r.jobs {
		if job.CampaignID != campaignID {
			continue
		}
		if before != nil && (job.RequestedAt.After(*before) || (job.RequestedAt.Equal(*before) && job.ID >= beforeID)) {
			continue
		}
		items = append(items, job)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].RequestedAt.Equal(items[j].RequestedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].RequestedAt.After(items[j].RequestedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *MemoryMaterialisationRepository) Claim(_ context.Context, owner string, limit int, lease time.Duration, now time.Time) ([]MaterialisationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []MaterialisationJob{}
	for identifier, job := range r.jobs {
		leaseExpired := job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(now)
		if (job.Status == MaterialisationPending || job.Status == MaterialisationRunning) && leaseExpired && !job.NextAttemptAt.After(now) {
			expires := now.Add(lease)
			job.Status = MaterialisationRunning
			job.LeaseOwner = owner
			job.LeaseToken++
			job.LeaseExpiresAt = &expires
			job.AttemptCount++
			job.Version++
			job.UpdatedAt = now
			if job.StartedAt == nil {
				started := now
				job.StartedAt = &started
			}
			r.jobs[identifier] = job
			items = append(items, job)
			if len(items) >= limit {
				break
			}
		}
	}
	return items, nil
}

func (r *MemoryMaterialisationRepository) Renew(_ context.Context, identifier, owner string, token int64, now time.Time, lease time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, exists := r.jobs[identifier]
	if !exists {
		return ErrMaterialisationNotFound
	}
	if job.Status != MaterialisationRunning || job.LeaseToken != token || job.LeaseOwner != owner {
		return ErrMaterialisationConflict
	}
	expires := now.Add(lease)
	job.LeaseExpiresAt = &expires
	job.UpdatedAt = now
	r.jobs[identifier] = job
	return nil
}

func (r *MemoryMaterialisationRepository) AppendMembers(_ context.Context, identifier string, token int64, members []segment.Member, lastContactID, rollingHash string, processedCount int64, now time.Time) (MaterialisationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, exists := r.jobs[identifier]
	if !exists {
		return MaterialisationJob{}, ErrMaterialisationNotFound
	}
	if job.Status != MaterialisationRunning || job.LeaseToken != token || processedCount != job.ProcessedCount+int64(len(members)) {
		return MaterialisationJob{}, ErrMaterialisationConflict
	}
	existing := r.members[identifier]
	for _, member := range members {
		if len(existing) > 0 && member.ContactID <= existing[len(existing)-1].ContactID {
			return MaterialisationJob{}, errors.New("materialisation members must be strictly ordered")
		}
		existing = append(existing, member)
	}
	r.members[identifier] = existing
	job.ProcessedCount = processedCount
	job.LastContactID = lastContactID
	job.RollingHash = rollingHash
	job.LeaseOwner = ""
	job.LeaseExpiresAt = nil
	job.Version++
	job.UpdatedAt = now
	r.jobs[identifier] = job
	return job, nil
}

func (r *MemoryMaterialisationRepository) Complete(_ context.Context, identifier string, token int64, snapshot segment.Snapshot, now time.Time) (MaterialisationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, exists := r.jobs[identifier]
	if !exists {
		return MaterialisationJob{}, ErrMaterialisationNotFound
	}
	if job.Status != MaterialisationRunning || job.LeaseToken != token || int64(len(r.members[identifier])) != job.ExpectedCount || snapshot.SnapshotHash != job.RollingHash {
		return MaterialisationJob{}, ErrMaterialisationConflict
	}
	job.Status = MaterialisationCompleted
	job.SnapshotID = snapshot.ID
	job.LeaseOwner = ""
	job.LeaseExpiresAt = nil
	job.FailureCode = ""
	job.FailureReference = ""
	job.Version++
	job.UpdatedAt = now
	completed := now
	job.CompletedAt = &completed
	r.jobs[identifier] = job
	return job, nil
}

func (r *MemoryMaterialisationRepository) Retry(_ context.Context, identifier string, token int64, code, reference string, nextAttemptAt, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, exists := r.jobs[identifier]
	if !exists {
		return ErrMaterialisationNotFound
	}
	if job.Status != MaterialisationRunning || job.LeaseToken != token {
		return ErrMaterialisationConflict
	}
	job.Status = MaterialisationPending
	job.FailureCode = code
	job.FailureReference = reference
	job.NextAttemptAt = nextAttemptAt
	job.LeaseOwner = ""
	job.LeaseExpiresAt = nil
	job.Version++
	job.UpdatedAt = now
	r.jobs[identifier] = job
	return nil
}

func (r *MemoryMaterialisationRepository) Fail(_ context.Context, identifier string, token int64, code, reference string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, exists := r.jobs[identifier]
	if !exists {
		return ErrMaterialisationNotFound
	}
	if job.Status != MaterialisationRunning || job.LeaseToken != token {
		return ErrMaterialisationConflict
	}
	job.Status = MaterialisationFailed
	job.FailureCode = code
	job.FailureReference = reference
	job.LeaseOwner = ""
	job.LeaseExpiresAt = nil
	job.Version++
	job.UpdatedAt = now
	completed := now
	job.CompletedAt = &completed
	r.jobs[identifier] = job
	return nil
}

func (r *MemoryMaterialisationRepository) Cancel(_ context.Context, identifier string, version int64, actor, reason string, now time.Time) (MaterialisationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, exists := r.jobs[identifier]
	if !exists {
		return MaterialisationJob{}, ErrMaterialisationNotFound
	}
	if job.Version != version || job.Status == MaterialisationCompleted || job.Status == MaterialisationFailed || job.Status == MaterialisationCancelled {
		return MaterialisationJob{}, ErrMaterialisationConflict
	}
	job.Status = MaterialisationCancelled
	job.CancelledBy = actor
	job.CancellationReason = reason
	job.LeaseOwner = ""
	job.LeaseExpiresAt = nil
	job.Version++
	job.UpdatedAt = now
	completed := now
	job.CompletedAt = &completed
	r.jobs[identifier] = job
	return job, nil
}

func (r *MemoryMaterialisationRepository) StagedMembers(_ context.Context, identifier string) ([]segment.Member, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.jobs[identifier]; !exists {
		return nil, ErrMaterialisationNotFound
	}
	return append([]segment.Member(nil), r.members[identifier]...), nil
}
