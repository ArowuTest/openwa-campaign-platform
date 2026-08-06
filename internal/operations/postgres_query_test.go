package operations

import (
	"testing"
	"time"
)

func TestCapacityShortfallStatementBindsCurrentTime(t *testing.T) {
	now := time.Date(2026, 8, 6, 22, 0, 0, 0, time.FixedZone("BST", 3600))
	query, args := capacityShortfallStatement(now)
	if query == "" || len(args) != 1 {
		t.Fatalf("query=%q args=%v", query, args)
	}
	got, ok := args[0].(time.Time)
	if !ok || !got.Equal(now.UTC()) {
		t.Fatalf("bound time=%#v want=%s", args[0], now.UTC())
	}
}
