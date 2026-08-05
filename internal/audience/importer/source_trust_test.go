package importer

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSourceTrustServiceCreatesAndVersionsPolicy(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	repo := NewMemorySourceTrustRepository()
	svc := &SourceTrustService{Repository: repo, Clock: func() time.Time { return now }}
	created, err := svc.Upsert(context.Background(), "org-1", "crm_verified", 90, "verified customer master", "admin-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if created.SourceSystem != "CRM_VERIFIED" || created.Version != 1 || created.TrustLevel != 90 {
		t.Fatalf("unexpected policy: %+v", created)
	}
	if _, err := svc.Upsert(context.Background(), "org-1", "CRM_VERIFIED", 80, "revised governance basis", "admin-2", 0); !errors.Is(err, ErrSourceTrustVersion) {
		t.Fatalf("expected version conflict, got %v", err)
	}
	updated, err := svc.Upsert(context.Background(), "org-1", "CRM_VERIFIED", 80, "revised governance basis", "admin-2", 1)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 || updated.TrustLevel != 80 {
		t.Fatalf("unexpected update: %+v", updated)
	}
	items, err := svc.List(context.Background(), "org-1")
	if err != nil || len(items) != 1 || items[0].Version != 2 {
		t.Fatalf("unexpected list: %+v %v", items, err)
	}
}

func TestSourceTrustServiceRejectsUnsafeInput(t *testing.T) {
	svc := &SourceTrustService{Repository: NewMemorySourceTrustRepository()}
	if _, err := svc.Upsert(context.Background(), "org-1", "CRM", 101, "valid reason text", "admin-1", 0); err == nil {
		t.Fatal("expected trust range validation")
	}
	if _, err := svc.Upsert(context.Background(), "org-1", "CRM", 50, "short", "admin-1", 0); err == nil {
		t.Fatal("expected reason validation")
	}
}
