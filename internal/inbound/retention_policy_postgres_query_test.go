package inbound

import (
	"strings"
	"testing"
)

func TestRetentionPolicyCreatePersistsApprovalActors(t *testing.T) {
	for _, required := range []string{
		"submitted_by",
		"approved_by",
		"NULLIF($8,'')::uuid",
		"NULLIF($9,'')::uuid",
	} {
		if !strings.Contains(insertRetentionPolicySQL, required) {
			t.Fatalf("retention insert must contain %q: %s", required, insertRetentionPolicySQL)
		}
	}
}
