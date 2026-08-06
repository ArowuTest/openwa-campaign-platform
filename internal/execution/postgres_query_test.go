package execution

import (
	"strings"
	"testing"
)

func TestActiveClaimPredicateTypesTimestampParameter(t *testing.T) {
	if !strings.Contains(activeClaimPredicate, "$4::timestamptz") {
		t.Fatalf("active claim predicate must type parameter $4: %s", activeClaimPredicate)
	}
}
