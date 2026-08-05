package operations

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/audit"
)

func TestIncidentLifecycleAndAudit(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	ar := audit.NewMemoryRepository()
	svc := &Service{Repo: repo, Audit: audit.NewRecorder(ar), AuditRepository: ar, Clock: func() time.Time { return time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC) }}
	in, err := svc.CreateIncident(ctx, Incident{Category: "SENDER_HEALTH", Severity: SeverityCritical, Summary: "Gateway pool degraded"}, "00000000-0000-4000-8000-000000000001", "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if in.Status != IncidentOpen || in.Version != 1 {
		t.Fatalf("unexpected incident: %+v", in)
	}
	out, err := svc.UpdateIncident(ctx, in.ID, 1, IncidentResolved, "00000000-0000-4000-8000-000000000001", "Node replaced", "00000000-0000-4000-8000-000000000001", "req-2")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != IncidentResolved || out.ResolvedAt == nil || out.Version != 2 {
		t.Fatalf("unexpected resolved: %+v", out)
	}
	events, err := svc.SearchAudit(ctx, 0, 10)
	if err != nil || len(events) != 2 {
		t.Fatalf("audit events=%d err=%v", len(events), err)
	}
}
func TestExportMakerChecker(t *testing.T) {
	ctx := context.Background()
	svc := &Service{Repo: NewMemoryRepository(), Clock: func() time.Time { return time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC) }}
	req, err := svc.RequestExport(ctx, "CAMPAIGN_REPORT", "00000000-0000-4000-8000-000000000010", "PDF", "client report", "00000000-0000-4000-8000-000000000001", "r1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.DecideExport(ctx, req.ID, 1, true, "", "00000000-0000-4000-8000-000000000001", "r2"); err == nil {
		t.Fatal("expected maker-checker failure")
	}
	ready, err := svc.DecideExport(ctx, req.ID, 1, true, "", "00000000-0000-4000-8000-000000000002", "r3")
	if err != nil {
		t.Fatal(err)
	}
	if ready.Status != ExportApproved || ready.ExpiresAt != nil {
		t.Fatalf("unexpected ready export: %+v", ready)
	}
	if _, err = svc.DecideExport(ctx, req.ID, 1, true, "", "00000000-0000-4000-8000-000000000003", "r4"); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict got %v", err)
	}
}
