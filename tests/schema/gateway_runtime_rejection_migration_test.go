package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGatewayRuntimeRejectionEvidenceAllowsOnlyRejectedEventsWithoutPoolAnchor(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0090_gateway_runtime_rejection_evidence.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{
		"ALTER COLUMN gateway_pool_id DROP NOT NULL",
		"gateway_runtime_events_pool_anchor_check",
		"event_type = 'REJECTED' OR gateway_pool_id IS NOT NULL",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("0090 gateway rejection evidence hardening missing %q", required)
		}
	}
}
