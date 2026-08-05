package segment

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
)

func TestCreateSnapshotSortsMembersAndPersistsImmutableEvidence(t *testing.T) {
	store := NewMemoryRepository()
	service := NewService(store)
	service.clock = func() time.Time { return time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC) }
	snapshot, err := service.Create(context.Background(), CreateInput{
		CampaignID: "campaign-1", Definition: audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorEquals, Values: []any{"NG"}}}}, DefinitionVersion: 1,
		ConsentPolicyVersion: "consent-v1", ConfigurationVersion: "config-v1", CreatedBy: "operator",
		Members: []Member{{ContactID: "contact-b", EligibilityEvidenceHash: "hash-b"}, {ContactID: "contact-a", EligibilityEvidenceHash: "hash-a"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.EligibleCount != 2 || snapshot.SnapshotHash == "" {
		t.Fatalf("invalid snapshot: %#v", snapshot)
	}
	members, err := service.Members(context.Background(), snapshot.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || members[0].ContactID != "contact-a" || members[1].ContactID != "contact-b" {
		t.Fatalf("members not sorted: %#v", members)
	}
}

func TestCreateSnapshotRejectsDuplicateContact(t *testing.T) {
	service := NewService(NewMemoryRepository())
	_, err := service.Create(context.Background(), CreateInput{CampaignID: "c", Definition: audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorEquals, Values: []any{"NG"}}}}, DefinitionVersion: 1, ConsentPolicyVersion: "p", ConfigurationVersion: "v", CreatedBy: "u", Members: []Member{{ContactID: "a", EligibilityEvidenceHash: "h1"}, {ContactID: "a", EligibilityEvidenceHash: "h2"}}})
	if err == nil {
		t.Fatal("expected duplicate rejection")
	}
}

func TestCreateSnapshotReplayReturnsOriginalImmutableSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 30, 0, 0, time.UTC)
	repository := NewMemoryRepository()
	service := NewService(repository)
	service.clock = func() time.Time { return now }
	input := CreateInput{
		CampaignID: "campaign-1", Definition: audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"NG"}}}},
		DefinitionVersion: 1, ConsentPolicyVersion: "consent-v1", ConfigurationVersion: "config-v1", CreatedBy: "operator-1",
		Members: []Member{{ContactID: "contact-1", EligibilityEvidenceHash: "evidence-1"}},
	}
	first, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.SnapshotHash != second.SnapshotHash {
		t.Fatalf("snapshot replay created new evidence: first=%+v second=%+v", first, second)
	}
	if len(repository.items) != 1 {
		t.Fatalf("expected one stored snapshot, got %d", len(repository.items))
	}
}

func TestStoreRejectsFabricatedSnapshotHash(t *testing.T) {
	repository := NewMemoryRepository()
	definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorEquals, Values: []any{"NG"}}}}
	builder, err := NewBuilder("campaign-1", "", definition, 1, "consent-v1", "config-v1", "operator")
	if err != nil {
		t.Fatal(err)
	}
	members := []Member{{ContactID: "contact-1", EligibilityEvidenceHash: "evidence-1"}}
	if err := builder.Add(members[0].ContactID, members[0].EligibilityEvidenceHash); err != nil {
		t.Fatal(err)
	}
	snapshot, err := builder.Finalise(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	snapshot.SnapshotHash = strings.Repeat("0", 64)
	if _, _, err := repository.EnsureWithMembers(context.Background(), snapshot, members); !errors.Is(err, ErrSnapshotConflict) {
		t.Fatalf("expected fabricated hash rejection, got %v", err)
	}
}
