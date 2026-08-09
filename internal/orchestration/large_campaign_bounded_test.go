package orchestration

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/segment"
)

type fiveMillionSnapshotReader struct {
	snapshot segment.Snapshot
}

func (r fiveMillionSnapshotReader) Get(context.Context, string) (segment.Snapshot, error) {
	return r.snapshot, nil
}
func (r fiveMillionSnapshotReader) Members(_ context.Context, _ string, after string, limit int) ([]segment.Member, error) {
	start := 0
	if after != "" {
		value, err := strconv.Atoi(after)
		if err != nil {
			return nil, err
		}
		start = value + 1
	}
	remaining := int(r.snapshot.EligibleCount) - start
	if remaining <= 0 {
		return nil, nil
	}
	if remaining < limit {
		limit = remaining
	}
	items := make([]segment.Member, limit)
	for i := 0; i < limit; i++ {
		id := strconv.Itoa(start + i)
		items[i] = segment.Member{ContactID: id, EligibilityEvidenceHash: "e-" + id}
	}
	return items, nil
}

type boundedRecordingStore struct {
	calls, maximumBatch int
	shardTarget         int
}

func (s *boundedRecordingStore) Authorise(_ context.Context, command Command, _ EligibilityChecker) (Result, error) {
	s.calls++
	if len(command.Members) > s.maximumBatch {
		s.maximumBatch = len(command.Members)
	}
	if s.shardTarget == 0 {
		s.shardTarget = command.ShardSize
	}
	if command.ShardSize != s.shardTarget {
		return Result{}, fmt.Errorf("shard target changed")
	}
	return Result{Authorised: len(command.Members), OutboxCreated: len(command.Members), ExclusionReasons: map[string]int{}}, nil
}

func TestFiveMillionRecipientReleaseRemainsBounded(t *testing.T) {
	const total = int64(5_000_000)
	now := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	campaigns := campaign.NewMemoryRepository()
	entity := campaign.Campaign{
		ID: "campaign-five-million", OrganisationID: "org", PurposeID: "purpose",
		Status: campaign.StatusScheduled, MaximumUniqueRecipients: total,
		AudienceSnapshotID: "snapshot-five-million", AudienceSnapshotHash: "hash-five-million",
		EligibleAudienceCount: total, MessageVersionID: "message", MessageContentHash: "message-hash", Version: 1,
	}
	if err := campaigns.Create(context.Background(), entity); err != nil {
		t.Fatal(err)
	}
	reader := fiveMillionSnapshotReader{snapshot: segment.Snapshot{ID: entity.AudienceSnapshotID, CampaignID: entity.ID, SnapshotHash: entity.AudienceSnapshotHash, EligibleCount: total}}
	store := &boundedRecordingStore{}
	service := &ReleaseService{
		Campaigns: campaign.NewService(campaigns), Snapshots: reader, Store: store,
		Eligibility: EligibilityFunc(func(context.Context, string, string, string, string, time.Time) (EligibilityDecision, error) {
			return EligibilityDecision{Eligible: true}, nil
		}),
		BatchSize: 5_000, ShardCount: 10_000, Clock: func() time.Time { return now },
	}
	summary, err := service.Release(context.Background(), entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Authorised != int(total) || summary.OutboxCreated != int(total) {
		t.Fatalf("summary=%+v", summary)
	}
	if store.maximumBatch != 5_000 {
		t.Fatalf("maximum transaction batch=%d want=5000", store.maximumBatch)
	}
	if store.calls != 1_000 || summary.BatchesProcessed != 1_000 {
		t.Fatalf("store calls=%d batches=%d want=1000", store.calls, summary.BatchesProcessed)
	}
	if store.shardTarget != 10_000 {
		t.Fatalf("shard target=%d want=10000", store.shardTarget)
	}
	for _, contact := range []string{"0", "1", "4999999"} {
		shard := shardFor(contact, total, store.shardTarget)
		if shard < 0 || shard >= 500 {
			t.Fatalf("contact=%s shard=%d outside 500 bounded shards", contact, shard)
		}
	}
}
