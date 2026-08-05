package execution

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrReallocationInvalid  = errors.New("campaign shard reallocation is invalid")
	ErrReallocationUnsafe   = errors.New("campaign shard contains submitted or terminal recipients")
	ErrReallocationConflict = errors.New("campaign shard reallocation conflict")
)

type ShardReallocation struct {
	ID                   string    `json:"id"`
	CampaignID           string    `json:"campaignId"`
	RoutingPlanID        string    `json:"routingPlanId"`
	DispatchShardID      string    `json:"dispatchShardId"`
	FromSenderPoolID     string    `json:"fromSenderPoolId"`
	ToSenderPoolID       string    `json:"toSenderPoolId"`
	ActorID              string    `json:"actorId"`
	Reason               string    `json:"reason"`
	EvidenceReference    string    `json:"evidenceReference"`
	PreviousLeaseVersion int64     `json:"previousLeaseVersion"`
	NewLeaseVersion      int64     `json:"newLeaseVersion"`
	CreatedAt            time.Time `json:"createdAt"`
}

type ReallocationStore interface {
	ReallocateShard(context.Context, string, string, string, string, time.Time) (ShardReallocation, error)
	ListShardReallocations(context.Context, string) ([]ShardReallocation, error)
}

type ReallocationAdministration struct {
	Store ReallocationStore
	Plans RoutingPlanStore
	Clock func() time.Time
}

func (s *ReallocationAdministration) Reallocate(ctx context.Context, shardID, targetPoolID, actor, reason, evidence string) (ShardReallocation, error) {
	shardID = strings.TrimSpace(shardID)
	targetPoolID = strings.TrimSpace(targetPoolID)
	actor = strings.TrimSpace(actor)
	reason = strings.TrimSpace(reason)
	evidence = strings.TrimSpace(evidence)
	if s == nil || s.Store == nil || shardID == "" || targetPoolID == "" || actor == "" || len(reason) < 8 || evidence == "" {
		return ShardReallocation{}, ErrReallocationInvalid
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	return s.Store.ReallocateShard(ctx, shardID, targetPoolID, actor, reason+"\x1f"+evidence, now)
}

func (s *ReallocationAdministration) List(ctx context.Context, shardID string) ([]ShardReallocation, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(shardID) == "" {
		return nil, ErrReallocationInvalid
	}
	return s.Store.ListShardReallocations(ctx, strings.TrimSpace(shardID))
}
