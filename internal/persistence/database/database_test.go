package database

import (
	"context"
	"strings"
	"testing"
)

func TestOpenFailsClosedWhenPostgreSQLDriverIsNotLinked(t *testing.T) {
	_, err := Open(context.Background(), PoolConfig{Driver: "definitely-not-linked", DSN: "postgres://example"})
	if err == nil || !strings.Contains(err.Error(), "not linked") {
		t.Fatalf("expected missing driver error, got %v", err)
	}
}

func TestOpenRequiresDSNBeforeDriverLookup(t *testing.T) {
	_, err := Open(context.Background(), PoolConfig{Driver: "missing"})
	if err == nil || !strings.Contains(err.Error(), "DSN") {
		t.Fatalf("expected DSN error, got %v", err)
	}
}
