package segment

import (
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
