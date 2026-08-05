package orchestration

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestReleaseIsAtomicIdempotentAndRechecksEligibility(t *testing.T) {
	store := NewMemoryStore()
	checker := EligibilityFunc(func(_ context.Context, contactID, _, _, _ string, _ time.Time) (EligibilityDecision, error) {
		if contactID == "withdrawn" {
			return EligibilityDecision{Eligible: false, ExclusionReason: "WITHDRAWN_AFTER_SNAPSHOT"}, nil
		}
		return EligibilityDecision{Eligible: true}, nil
	})
	cmd := Command{CampaignID: "c", SnapshotID: "s", MessageVersionID: "m", OrganisationID: "o", PurposeID: "p", Channel: "WHATSAPP", MaximumUniqueRecipients: 2, Members: []Member{{ContactID: "a", EligibilityEvidenceHash: "e1"}, {ContactID: "withdrawn", EligibilityEvidenceHash: "e2"}}, ShardSize: 10, AsOf: time.Now().UTC()}
	first, err := store.Authorise(context.Background(), cmd, checker)
	if err != nil {
		t.Fatal(err)
	}
	if first.Authorised != 1 || first.Excluded != 1 || first.OutboxCreated != 1 {
		t.Fatalf("first=%+v", first)
	}
	second, err := store.Authorise(context.Background(), cmd, checker)
	if err != nil {
		t.Fatal(err)
	}
	if second.Existing != 2 || second.OutboxCreated != 0 {
		t.Fatalf("second=%+v", second)
	}
	if len(mustOutbox(t, store)) != 1 {
		t.Fatal("duplicate outbox created")
	}
}
func TestReleaseRejectsEntitlementOverflowWithoutPartialWrites(t *testing.T) {
	store := NewMemoryStore()
	checker := EligibilityFunc(func(context.Context, string, string, string, string, time.Time) (EligibilityDecision, error) {
		return EligibilityDecision{Eligible: true}, nil
	})
	cmd := Command{CampaignID: "c", SnapshotID: "s", MessageVersionID: "m", OrganisationID: "o", PurposeID: "p", Channel: "WHATSAPP", MaximumUniqueRecipients: 1, Members: []Member{{ContactID: "a", EligibilityEvidenceHash: "e1"}, {ContactID: "b", EligibilityEvidenceHash: "e2"}}, AsOf: time.Now()}
	if _, err := store.Authorise(context.Background(), cmd, checker); err != ErrEntitlementExceeded {
		t.Fatalf("err=%v", err)
	}
	if len(mustRecipients(t, store, "c")) != 0 || len(mustOutbox(t, store)) != 0 {
		t.Fatal("partial release persisted")
	}
}

func TestShardForUsesEntitlementDerivedShardCount(t *testing.T) {
	for i := 0; i < 100; i++ {
		shard := shardFor(fmt.Sprintf("contact-%d", i), 50_000, 10_000)
		if shard < 0 || shard >= 5 {
			t.Fatalf("shard=%d", shard)
		}
	}
	if shardFor("contact", 100, 10_000) != 0 {
		t.Fatal("small campaign must use one shard")
	}
}
