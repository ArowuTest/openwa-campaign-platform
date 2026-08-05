package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

type ShardStatus string

const (
	ShardPending    ShardStatus = "PENDING"
	ShardRunning    ShardStatus = "RUNNING"
	ShardCompleted  ShardStatus = "COMPLETED"
	ShardExceptions ShardStatus = "COMPLETED_WITH_EXCEPTIONS"
)

var ErrShardLeaseConflict = errors.New("campaign dispatch shard lease conflict")

type DispatchShard struct {
	ID              string      `json:"id"`
	CampaignID      string      `json:"campaignId"`
	Ordinal         int         `json:"ordinal"`
	TargetSize      int         `json:"targetSize"`
	RecipientCount  int64       `json:"recipientCount"`
	TerminalCount   int64       `json:"terminalCount"`
	FailedCount     int64       `json:"failedCount"`
	UnknownCount    int64       `json:"unknownCount"`
	Status          ShardStatus `json:"status"`
	LastRecipientID string      `json:"lastRecipientId,omitempty"`
	LeaseOwner      string      `json:"leaseOwner,omitempty"`
	LeaseVersion    int64       `json:"leaseVersion"`
	LeaseExpiresAt  *time.Time  `json:"leaseExpiresAt,omitempty"`
	StartedAt       *time.Time  `json:"startedAt,omitempty"`
	CompletedAt     *time.Time  `json:"completedAt,omitempty"`
	CreatedAt       time.Time   `json:"createdAt"`
	UpdatedAt       time.Time   `json:"updatedAt"`
}

type ShardRepository interface {
	Discover(context.Context, int, time.Time, int) (int, error)
	Claim(context.Context, string, time.Time, time.Duration, int) ([]DispatchShard, error)
	Renew(context.Context, DispatchShard, time.Time, time.Duration) error
	Refresh(context.Context, DispatchShard, time.Time, time.Duration) (DispatchShard, error)
}

type ShardRunner struct {
	Repository      ShardRepository
	Owner           string
	TargetSize      int
	DiscoveryBatch  int
	ClaimBatch      int
	Lease           time.Duration
	PollInterval    time.Duration
	RefreshInterval time.Duration
	active          atomic.Int64
}

func (r *ShardRunner) Active() int64 {
	if r == nil {
		return 0
	}
	return r.active.Load()
}

func (r *ShardRunner) Run(ctx context.Context) error {
	if r == nil || r.Repository == nil || strings.TrimSpace(r.Owner) == "" {
		return errors.New("shard runner dependencies are required")
	}
	if r.TargetSize <= 0 || r.TargetSize > 100000 {
		r.TargetSize = 10000
	}
	if r.DiscoveryBatch <= 0 || r.DiscoveryBatch > 100 {
		r.DiscoveryBatch = 10
	}
	if r.ClaimBatch <= 0 || r.ClaimBatch > 100 {
		r.ClaimBatch = 20
	}
	if r.Lease <= 0 {
		r.Lease = 45 * time.Second
	}
	if r.PollInterval <= 0 {
		r.PollInterval = 2 * time.Second
	}
	if r.RefreshInterval <= 0 {
		r.RefreshInterval = 5 * time.Second
	}
	ticker := time.NewTicker(r.PollInterval)
	defer ticker.Stop()
	for {
		if err := r.runOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *ShardRunner) runOnce(ctx context.Context) error {
	now := time.Now().UTC()
	if _, err := r.Repository.Discover(ctx, r.TargetSize, now, r.DiscoveryBatch); err != nil {
		return fmt.Errorf("discover campaign shards: %w", err)
	}
	items, err := r.Repository.Claim(ctx, r.Owner, now, r.Lease, r.ClaimBatch)
	if err != nil {
		return fmt.Errorf("claim campaign shards: %w", err)
	}
	for _, shard := range items {
		r.active.Add(1)
		_, refreshErr := r.Repository.Refresh(ctx, shard, time.Now().UTC(), r.RefreshInterval)
		r.active.Add(-1)
		if refreshErr != nil && !errors.Is(refreshErr, ErrShardLeaseConflict) {
			return fmt.Errorf("refresh campaign shard %s: %w", shard.ID, refreshErr)
		}
	}
	return nil
}
