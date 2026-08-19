package execution

import (
	"strings"
	"testing"
)

func TestActiveClaimPredicateTypesTimestampParameter(t *testing.T) {
	if !strings.Contains(activeClaimPredicate, "$3::timestamptz") {
		t.Fatalf("active claim predicate must type timestamp parameter: %s", activeClaimPredicate)
	}
}
