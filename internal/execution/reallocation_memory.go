package execution

import (
	"campaign-platform/internal/shared/id"
	"context"
	"strings"
	"time"
)

func (r *MemoryShardRepository) ReallocateShard(_ context.Context, shardID, targetPoolID, actor, packed string, now time.Time) (ShardReallocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	shard, ok := r.Shards[shardID]
	if !ok || shard.RoutingPlanID == "" || shard.AssignedSenderPoolID == "" {
		return ShardReallocation{}, ErrReallocationInvalid
	}
	if shard.Status != ShardPending || shard.TerminalCount != 0 || shard.LeaseOwner != "" {
		return ShardReallocation{}, ErrReallocationUnsafe
	}
	if shard.AssignedSenderPoolID == targetPoolID {
		return ShardReallocation{}, ErrReallocationConflict
	}
	parts := strings.SplitN(packed, "\x1f", 2)
	ident, _ := id.New()
	event := ShardReallocation{ID: ident, CampaignID: shard.CampaignID, RoutingPlanID: shard.RoutingPlanID, DispatchShardID: shard.ID, FromSenderPoolID: shard.AssignedSenderPoolID, ToSenderPoolID: targetPoolID, ActorID: actor, Reason: parts[0], PreviousLeaseVersion: shard.LeaseVersion, NewLeaseVersion: shard.LeaseVersion + 1, CreatedAt: now}
	if len(parts) == 2 {
		event.EvidenceReference = parts[1]
	}
	shard.AssignedSenderPoolID = targetPoolID
	shard.LeaseVersion++
	shard.UpdatedAt = now
	r.Shards[shardID] = shard
	if r.Reallocations == nil {
		r.Reallocations = map[string][]ShardReallocation{}
	}
	r.Reallocations[shardID] = append(r.Reallocations[shardID], event)
	return event, nil
}
func (r *MemoryShardRepository) ListShardReallocations(_ context.Context, shardID string) ([]ShardReallocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ShardReallocation(nil), r.Reallocations[shardID]...), nil
}
