package importer

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

type UploadFinalisationWork struct {
	Session UploadSession
	Lease   FinalisationLease
}

type UploadFinalisationRepository interface {
	ClaimReadyFinalisations(context.Context, string, time.Duration, int, time.Time) ([]UploadFinalisationWork, error)
	RenewUploadFinalisation(context.Context, string, FinalisationLease, time.Duration, time.Time) (UploadSession, FinalisationLease, error)
	CompleteUploadFinalisation(context.Context, string, string, string, string, FinalisationLease, time.Time) (UploadSession, error)
	FailUploadFinalisation(context.Context, string, string, FinalisationLease, time.Time) (UploadSession, error)
}

func (r *MemoryUploadSessionRepository) ClaimReadyFinalisations(_ context.Context, workerID string, leaseDuration time.Duration, limit int, now time.Time) ([]UploadFinalisationWork, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.TrimSpace(workerID) == "" {
		return nil, errors.New("finalisation worker ID is required")
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	ids := make([]string, 0, len(r.items))
	for id := range r.items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	items := make([]UploadFinalisationWork, 0, min(limit, len(ids)))
	for _, id := range ids {
		if len(items) >= limit {
			break
		}
		current := r.items[id]
		eligible := current.State == UploadSessionUploaded ||
			(current.State == UploadSessionFinalising && current.FinaliserLeaseExpiresAt != nil && !now.Before(current.FinaliserLeaseExpiresAt.UTC()))
		if !eligible {
			continue
		}
		next, lease, err := current.ClaimFinalisation(workerID, leaseDuration, now)
		if err != nil {
			if errors.Is(err, ErrUploadFinalisationLease) || errors.Is(err, ErrUploadSessionState) {
				continue
			}
			return nil, err
		}
		r.items[id] = cloneUploadSession(next)
		items = append(items, UploadFinalisationWork{Session: cloneUploadSession(next), Lease: lease})
	}
	return items, nil
}

func (r *MemoryUploadSessionRepository) RenewUploadFinalisation(_ context.Context, identifier string, lease FinalisationLease, leaseDuration time.Duration, now time.Time) (UploadSession, FinalisationLease, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.items[identifier]
	if !exists {
		return UploadSession{}, FinalisationLease{}, ErrUploadSessionNotFound
	}
	next, renewed, err := current.RenewFinalisation(lease, leaseDuration, now)
	if err != nil {
		return UploadSession{}, FinalisationLease{}, err
	}
	r.items[identifier] = cloneUploadSession(next)
	return cloneUploadSession(next), renewed, nil
}

func (r *MemoryUploadSessionRepository) CompleteUploadFinalisation(_ context.Context, identifier, importID, wholeFileSHA256, mediaType string, lease FinalisationLease, now time.Time) (UploadSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.items[identifier]
	if !exists {
		return UploadSession{}, ErrUploadSessionNotFound
	}
	next, err := current.CompleteFinalisation(importID, wholeFileSHA256, mediaType, lease, now)
	if err != nil {
		return UploadSession{}, err
	}
	r.items[identifier] = cloneUploadSession(next)
	return cloneUploadSession(next), nil
}

func (r *MemoryUploadSessionRepository) FailUploadFinalisation(_ context.Context, identifier, reason string, lease FinalisationLease, now time.Time) (UploadSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.items[identifier]
	if !exists {
		return UploadSession{}, ErrUploadSessionNotFound
	}
	next, err := current.FailFinalisation(reason, lease, now)
	if err != nil {
		return UploadSession{}, err
	}
	r.items[identifier] = cloneUploadSession(next)
	return cloneUploadSession(next), nil
}
