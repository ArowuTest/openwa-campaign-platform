package postgres

import (
	"strings"
	"testing"
)

func TestIdentityQueryPlacesPredicateBeforeGrouping(t *testing.T) {
	query := identityQuery("u.id=$1::uuid")
	where := strings.Index(query, "WHERE u.id=$1::uuid")
	group := strings.Index(query, "GROUP BY")
	if where < 0 || group < 0 || where > group {
		t.Fatalf("predicate must precede grouping: %s", query)
	}
}
