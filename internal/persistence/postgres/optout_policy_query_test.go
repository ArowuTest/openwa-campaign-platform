package postgres

import (
	"strings"
	"testing"
)

func TestOptOutKeywordsEncodeAsJSONText(t *testing.T) {
	got, err := encodeOptOutKeywords([]string{"STOP", "END"})
	if err != nil {
		t.Fatal(err)
	}
	if got != `["STOP","END"]` {
		t.Fatalf("encoded keywords=%q", got)
	}
}

func TestOptOutCreatePersistsGovernanceActors(t *testing.T) {
	for _, required := range []string{"$2::jsonb", "submitted_by", "approved_by", "NULLIF($8,'')", "NULLIF($9,'')"} {
		if !strings.Contains(insertOptOutPolicySQL, required) {
			t.Fatalf("opt-out insert must contain %q: %s", required, insertOptOutPolicySQL)
		}
	}
}
