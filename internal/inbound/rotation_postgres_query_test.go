package inbound

import (
	"strings"
	"testing"
)

func TestRotationClaimColumnsAreQualified(t *testing.T) {
	if !strings.HasPrefix(rotationClaimColumns, "x.id::text") {
		t.Fatalf("claim columns must be qualified: %s", rotationClaimColumns)
	}
	if strings.Contains(rotationClaimColumns, ",id::text") {
		t.Fatalf("claim columns contain unqualified id: %s", rotationClaimColumns)
	}
}
