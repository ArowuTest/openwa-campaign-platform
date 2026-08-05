package segment

import (
	"context"
	"strings"

	audiencefilter "campaign-platform/internal/audience/filter"
	"testing"
	"time"
)

func TestSnapshotHashIsDeterministicAndOrdered(t *testing.T) {
	definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorEquals, Values: []any{"NG"}}}}
	a, _ := NewBuilder("c", "s", definition, 1, "consent-v1", "config-v1", "u")
	b, _ := NewBuilder("c", "s", definition, 1, "consent-v1", "config-v1", "u")
	for _, v := range [][2]string{{"a", "e1"}, {"b", "e2"}} {
		if err := a.Add(v[0], v[1]); err != nil {
			t.Fatal(err)
		}
		_ = b.Add(v[0], v[1])
	}
	sa, _ := a.Finalise(time.Now())
	sb, _ := b.Finalise(time.Now())
	if sa.SnapshotHash != sb.SnapshotHash {
		t.Fatal("hash not deterministic")
	}
	if err := a.Add("a", "e3"); err == nil {
		t.Fatal("out of order member accepted")
	}
}

func TestSnapshotHashCommitsPolicyAndConfigurationVersions(t *testing.T) {
	definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"NG"}}}}
	first, _ := NewBuilder("campaign", "segment", definition, 1, "consent-v1", "config-v1", "operator")
	second, _ := NewBuilder("campaign", "segment", definition, 1, "consent-v2", "config-v1", "operator")
	third, _ := NewBuilder("campaign", "segment", definition, 1, "consent-v1", "config-v2", "operator")
	for _, builder := range []*Builder{first, second, third} {
		if err := builder.Add("contact-1", "evidence-1"); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := first.Finalise(time.Now())
	b, _ := second.Finalise(time.Now())
	c, _ := third.Finalise(time.Now())
	if a.SnapshotHash == b.SnapshotHash || a.SnapshotHash == c.SnapshotHash || b.SnapshotHash == c.SnapshotHash {
		t.Fatal("snapshot hash did not commit policy/configuration versions")
	}
}

func TestSnapshotOverlapReturnsCountsWithoutIdentities(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorEquals, Values: []any{"NG"}}}}
	left, err := service.Create(context.Background(), CreateInput{CampaignID: "campaign-left", Definition: definition, DefinitionVersion: 1, ConsentPolicyVersion: "p1", ConfigurationVersion: "c1", CreatedBy: "user-1", Members: []Member{{ContactID: "00000000-0000-4000-8000-000000000001", EligibilityEvidenceHash: strings.Repeat("a", 64)}, {ContactID: "00000000-0000-4000-8000-000000000002", EligibilityEvidenceHash: strings.Repeat("b", 64)}}})
	if err != nil {
		t.Fatal(err)
	}
	right, err := service.Create(context.Background(), CreateInput{CampaignID: "campaign-right", Definition: definition, DefinitionVersion: 1, ConsentPolicyVersion: "p1", ConfigurationVersion: "c1", CreatedBy: "user-1", Members: []Member{{ContactID: "00000000-0000-4000-8000-000000000002", EligibilityEvidenceHash: strings.Repeat("b", 64)}, {ContactID: "00000000-0000-4000-8000-000000000003", EligibilityEvidenceHash: strings.Repeat("c", 64)}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Overlap(context.Background(), left.ID, right.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Intersection != 1 || result.OnlyLeft != 1 || result.OnlyRight != 1 || result.Union != 3 {
		t.Fatalf("unexpected overlap: %+v", result)
	}
}
