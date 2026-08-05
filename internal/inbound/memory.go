package inbound

import (
	"context"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu      sync.Mutex
	byID    map[string]Reply
	byEvent map[string]string
	order   []string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{byID: map[string]Reply{}, byEvent: map[string]string{}}
}
func (r *MemoryRepository) Create(_ context.Context, v Reply) (Reply, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.byEvent[v.EventID]; ok {
		old := r.byID[id]
		if old.MessageFingerprint != v.MessageFingerprint || old.RecipientID != v.RecipientID {
			return Reply{}, false, ErrReplayConflict
		}
		return old, false, nil
	}
	r.byID[v.ID] = v
	r.byEvent[v.EventID] = v.ID
	r.order = append(r.order, v.ID)
	return v, true, nil
}
func (r *MemoryRepository) Get(_ context.Context, id string) (Reply, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.byID[id]
	if !ok {
		return Reply{}, ErrNotFound
	}
	return v, nil
}
func (r *MemoryRepository) List(_ context.Context, limit int) ([]Reply, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Reply, 0, limit)
	for i := len(r.order) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, r.byID[r.order[i]])
	}
	return out, nil
}
func (r *MemoryRepository) UpdateReview(_ context.Context, id string, expected int64, c Classification, e bool, reason, actor string, now time.Time) (Reply, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.byID[id]
	if !ok {
		return Reply{}, ErrNotFound
	}
	if v.Version != expected {
		return Reply{}, ErrConflict
	}
	v.Classification = c
	v.Escalated = e
	v.EscalationReason = reason
	v.ReviewedBy = actor
	v.ReviewedAt = &now
	v.Version++
	r.byID[id] = v
	return v, nil
}
func (r *MemoryRepository) Summary(_ context.Context, campaignID string) (Summary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var s Summary
	for _, v := range r.byID {
		if v.CampaignID != campaignID {
			continue
		}
		s.Total++
		if v.Escalated {
			s.Escalated++
		}
		switch v.Classification {
		case ClassificationUnreviewed:
			s.Unreviewed++
		case ClassificationOptOut:
			s.OptOut++
		case ClassificationQuestion:
			s.Questions++
		case ClassificationComplaint:
			s.Complaints++
		case ClassificationOther:
			s.Other++
		}
	}
	return s, nil
}

func (r *MemoryRepository) RedactExpired(_ context.Context, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for id, v := range r.byID {
		if !v.LegalHold && v.ContentRedactedAt == nil && !v.ContentRetainUntil.After(now) {
			v.MessageText = ""
			at := now
			v.ContentRedactedAt = &at
			v.Version++
			r.byID[id] = v
			count++
		}
	}
	return count, nil
}

func (r *MemoryRepository) SetLegalHold(_ context.Context, id string, expected int64, hold bool, reason, actor string, now time.Time) (Reply, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.byID[id]
	if !ok {
		return Reply{}, ErrNotFound
	}
	if v.Version != expected {
		return Reply{}, ErrConflict
	}
	if hold {
		v.LegalHold = true
		v.LegalHoldReason = reason
		v.LegalHoldAppliedBy = actor
		v.LegalHoldAppliedAt = &now
		v.LegalHoldReleasedBy = ""
		v.LegalHoldReleasedAt = nil
	} else {
		v.LegalHold = false
		v.LegalHoldReleasedBy = actor
		v.LegalHoldReleasedAt = &now
	}
	v.Version++
	r.byID[id] = v
	return v, nil
}

func (r *MemoryRepository) ReencryptContent(context.Context, int) (int64, error) { return 0, nil }
