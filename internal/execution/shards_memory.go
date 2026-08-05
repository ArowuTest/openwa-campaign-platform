package execution

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

type MemoryShardRepository struct {
	mu            sync.Mutex
	Shards        map[string]DispatchShard
	DiscoverCount int
	Reallocations map[string][]ShardReallocation
}

func NewMemoryShardRepository() *MemoryShardRepository {
	return &MemoryShardRepository{Shards: map[string]DispatchShard{}, Reallocations: map[string][]ShardReallocation{}}
}
func (r *MemoryShardRepository) Discover(context.Context, int, time.Time, int) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.DiscoverCount
	r.DiscoverCount = 0
	return n, nil
}
func (r *MemoryShardRepository) Claim(_ context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]DispatchShard, error) {
	if owner == "" || lease <= 0 {
		return nil, errors.New("owner and positive lease are required")
	}
	if limit <= 0 {
		limit = 20
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0)
	for id, s := range r.Shards {
		if s.Status == ShardPending || (s.Status == ShardRunning && s.LeaseExpiresAt != nil && !s.LeaseExpiresAt.After(now)) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := make([]DispatchShard, 0, len(ids))
	for _, id := range ids {
		s := r.Shards[id]
		s.Status = ShardRunning
		s.LeaseOwner = owner
		s.LeaseVersion++
		v := now.Add(lease)
		s.LeaseExpiresAt = &v
		if s.StartedAt == nil {
			t := now
			s.StartedAt = &t
		}
		s.UpdatedAt = now
		r.Shards[id] = s
		out = append(out, s)
	}
	return out, nil
}
func (r *MemoryShardRepository) Renew(_ context.Context, s DispatchShard, now time.Time, lease time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.Shards[s.ID]
	if !ok || v.Status != ShardRunning || v.LeaseOwner != s.LeaseOwner || v.LeaseVersion != s.LeaseVersion || v.LeaseExpiresAt == nil || !v.LeaseExpiresAt.After(now) {
		return ErrShardLeaseConflict
	}
	e := now.Add(lease)
	v.LeaseExpiresAt = &e
	v.UpdatedAt = now
	r.Shards[v.ID] = v
	return nil
}
func (r *MemoryShardRepository) Refresh(_ context.Context, s DispatchShard, now time.Time, _ time.Duration) (DispatchShard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.Shards[s.ID]
	if !ok || v.Status != ShardRunning || v.LeaseOwner != s.LeaseOwner || v.LeaseVersion != s.LeaseVersion || v.LeaseExpiresAt == nil || !v.LeaseExpiresAt.After(now) {
		return DispatchShard{}, ErrShardLeaseConflict
	}
	if v.RecipientCount > 0 && v.TerminalCount == v.RecipientCount {
		v.Status = ShardCompleted
		if v.FailedCount > 0 || v.UnknownCount > 0 {
			v.Status = ShardExceptions
		}
		t := now
		v.CompletedAt = &t
	} else {
		v.Status = ShardPending
	}
	v.LeaseOwner = ""
	v.LeaseExpiresAt = nil
	v.UpdatedAt = now
	r.Shards[v.ID] = v
	return v, nil
}
