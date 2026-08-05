package orchestration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/organisation"
	"campaign-platform/internal/segment"
)

// CampaignReader exposes the authoritative campaign evidence required to release
// a frozen audience. Implementations must return immutable snapshot/message links.
type CampaignReader interface {
	Get(context.Context, string) (campaign.Campaign, error)
}

// SnapshotReader pages immutable snapshot members in deterministic contact order.
type SnapshotReader interface {
	Get(context.Context, string) (segment.Snapshot, error)
	Members(context.Context, string, string, int) ([]segment.Member, error)
}

// ReleaseService materialises campaign recipient obligations in bounded,
// restart-safe transactions. Re-running the same release is idempotent because
// the store enforces the campaign/contact/message natural key.
type ReleaseService struct {
	Campaigns     CampaignReader
	Organisations interface {
		Get(context.Context, string) (organisation.Organisation, error)
	}
	Snapshots   SnapshotReader
	Store       AtomicStore
	Eligibility EligibilityChecker
	BatchSize   int
	ShardCount  int
	Clock       func() time.Time
}

// ReleaseSummary is cumulative for this invocation. Existing recipients are
// reported separately so operators can distinguish resume/replay from new work.
type ReleaseSummary struct {
	CampaignID       string         `json:"campaignId"`
	SnapshotID       string         `json:"snapshotId"`
	SnapshotMembers  int64          `json:"snapshotMembers"`
	Authorised       int            `json:"authorised"`
	Excluded         int            `json:"excluded"`
	Existing         int            `json:"existing"`
	OutboxCreated    int            `json:"outboxCreated"`
	BatchesProcessed int            `json:"batchesProcessed"`
	ExclusionReasons map[string]int `json:"exclusionReasons"`
	CompletedAt      time.Time      `json:"completedAt"`
}

func (s *ReleaseService) Release(ctx context.Context, campaignID string) (ReleaseSummary, error) {
	if s == nil || s.Campaigns == nil || s.Snapshots == nil || s.Store == nil || s.Eligibility == nil {
		return ReleaseSummary{}, errors.New("release service dependencies are required")
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return ReleaseSummary{}, errors.New("campaign ID is required")
	}
	entity, err := s.Campaigns.Get(ctx, campaignID)
	if err != nil {
		return ReleaseSummary{}, fmt.Errorf("load campaign: %w", err)
	}
	if s.Organisations != nil {
		org, orgErr := s.Organisations.Get(ctx, entity.OrganisationID)
		if orgErr != nil {
			return ReleaseSummary{}, fmt.Errorf("load organisation: %w", orgErr)
		}
		if org.Status != organisation.StatusActive {
			return ReleaseSummary{}, organisation.ErrNotActive
		}
	}
	if entity.Status != campaign.StatusScheduled && entity.Status != campaign.StatusDispatching {
		return ReleaseSummary{}, fmt.Errorf("campaign status %s cannot release recipients", entity.Status)
	}
	if entity.AudienceSnapshotID == "" || entity.AudienceSnapshotHash == "" || entity.MessageVersionID == "" || entity.MessageContentHash == "" {
		return ReleaseSummary{}, errors.New("campaign lacks approved immutable audience or message evidence")
	}
	snapshot, err := s.Snapshots.Get(ctx, entity.AudienceSnapshotID)
	if err != nil {
		return ReleaseSummary{}, fmt.Errorf("load audience snapshot: %w", err)
	}
	if snapshot.CampaignID != entity.ID || snapshot.SnapshotHash != entity.AudienceSnapshotHash || snapshot.EligibleCount != entity.EligibleAudienceCount {
		return ReleaseSummary{}, ErrReleaseConflict
	}
	if snapshot.EligibleCount > entity.MaximumUniqueRecipients {
		return ReleaseSummary{}, ErrEntitlementExceeded
	}

	batchSize := s.BatchSize
	if batchSize <= 0 || batchSize > 5_000 {
		batchSize = 1_000
	}
	shardCount := s.ShardCount
	if shardCount <= 0 {
		shardCount = 256
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	summary := ReleaseSummary{
		CampaignID: entity.ID, SnapshotID: snapshot.ID, SnapshotMembers: snapshot.EligibleCount,
		ExclusionReasons: make(map[string]int),
	}
	after := ""
	var visited int64
	for {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		members, err := s.Snapshots.Members(ctx, snapshot.ID, after, batchSize)
		if err != nil {
			return summary, fmt.Errorf("load snapshot members: %w", err)
		}
		if len(members) == 0 {
			break
		}
		commandMembers := make([]Member, len(members))
		for i, member := range members {
			commandMembers[i] = Member{ContactID: member.ContactID, EligibilityEvidenceHash: member.EligibilityEvidenceHash}
		}
		result, err := s.Store.Authorise(ctx, Command{
			CampaignID: entity.ID, SnapshotID: snapshot.ID, MessageVersionID: entity.MessageVersionID,
			OrganisationID: entity.OrganisationID, PurposeID: entity.PurposeID, Channel: "WHATSAPP",
			MaximumUniqueRecipients: entity.MaximumUniqueRecipients, Members: commandMembers,
			ShardSize: shardCount, AsOf: now,
		}, s.Eligibility)
		if err != nil {
			return summary, fmt.Errorf("authorise release batch after %q: %w", after, err)
		}
		summary.Authorised += result.Authorised
		summary.Excluded += result.Excluded
		summary.Existing += result.Existing
		summary.OutboxCreated += result.OutboxCreated
		summary.BatchesProcessed++
		for reason, count := range result.ExclusionReasons {
			summary.ExclusionReasons[reason] += count
		}
		visited += int64(len(members))
		after = members[len(members)-1].ContactID
		if len(members) < batchSize {
			break
		}
	}
	if visited != snapshot.EligibleCount {
		return summary, fmt.Errorf("snapshot member count mismatch: expected %d, read %d", snapshot.EligibleCount, visited)
	}
	summary.CompletedAt = now
	return summary, nil
}
