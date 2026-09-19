package operations

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/audit"
	"campaign-platform/internal/delivery"
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
	page, err := svc.SearchAudit(ctx, audit.Query{Limit: 10})
	events := page.Items
	if err != nil || len(events) != 2 {
		t.Fatalf("audit events=%d err=%v", len(events), err)
	}
}
func TestExportMakerChecker(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	repo.reports["00000000-0000-4000-8000-000000000010"] = CampaignReport{CampaignID: "00000000-0000-4000-8000-000000000010", Audience: map[string]int64{}, Delivery: map[string]int64{}, Engagement: map[string]int64{}, Exceptions: map[string]int64{}}
	svc := &Service{Repo: repo, Clock: func() time.Time { return time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC) }}
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

func TestResolveUnknownDeliveryWithProviderEvidence(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "00000000-0000-4000-8000-000000000100", CampaignID: "00000000-0000-4000-8000-000000000200", Status: delivery.StatusUnknown, ProviderMessageID: "provider-1", ReconciliationRequired: true, UpdatedAt: now.Add(-time.Minute)}
	deliveryService := delivery.NewService(delivery.NewMemoryRepository(recipient))
	auditRepo := audit.NewMemoryRepository()
	svc := &Service{Repo: NewMemoryRepository(), Deliveries: deliveryService, Audit: audit.NewRecorder(auditRepo), Clock: func() time.Time { return now }}
	resolution, err := svc.ResolveDeliveryException(ctx, recipient.ID, ResolutionConfirmDelivered, "provider-console-7788", "provider console confirms delivery", "00000000-0000-4000-8000-000000000001", "req-resolution")
	if err != nil {
		t.Fatal(err)
	}
	if resolution.ResultStatus != delivery.StatusDelivered {
		t.Fatalf("unexpected resolution: %+v", resolution)
	}
	stored, _ := deliveryService.Get(ctx, recipient.ID)
	if stored.Status != delivery.StatusDelivered || stored.ReconciliationRequired {
		t.Fatalf("exception not resolved: %+v", stored)
	}
	events, _ := auditRepo.List(ctx, 0, 10)
	if len(events) != 1 || events[0].Action != "DELIVERY_EXCEPTION_RESOLVED" {
		t.Fatalf("missing resolution audit: %+v", events)
	}
}

func TestConfirmNotSubmittedRequiresExplicitDuplicateRiskAcknowledgement(t *testing.T) {
	ctx := context.Background()
	recipient := delivery.Recipient{ID: "recipient-unknown-risk", Status: delivery.StatusUnknown, ReconciliationRequired: true, UpdatedAt: time.Now().UTC()}
	auditRepo := audit.NewMemoryRepository()
	svc := &Service{Repo: NewMemoryRepository(), Deliveries: delivery.NewService(delivery.NewMemoryRepository(recipient)), Audit: audit.NewRecorder(auditRepo)}
	_, err := svc.ResolveDeliveryException(ctx, recipient.ID, ResolutionConfirmNotSubmitted, "gateway-log-456", "provider checked; safe to retry", "actor", "req")
	if !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("expected duplicate-risk approval requirement, got %v", err)
	}
	resolution, err := svc.ResolveDeliveryException(ctx, recipient.ID, ResolutionConfirmNotSubmitted, "gateway-log-456", "provider checked; safe to retry", "actor", "req-approved", true)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.ResultStatus != delivery.StatusFailedRetryable {
		t.Fatalf("approved not-submitted evidence must create governed retryable state, got=%s", resolution.ResultStatus)
	}
	stored, _ := svc.Deliveries.Get(ctx, recipient.ID)
	if stored.Status != delivery.StatusFailedRetryable || stored.ReconciliationRequired {
		t.Fatalf("approved not-submitted evidence did not create retryable state: %+v", stored)
	}
	queued, changed, err := svc.Deliveries.ApplyEvent(ctx, recipient.ID, delivery.Event{
		DeduplicationKey: "operator-approved-retry-queued",
		Type:             delivery.EventQueued,
		OccurredAt:       time.Now().UTC().Add(time.Second),
	})
	if err != nil || !changed || queued.Status != delivery.StatusQueued {
		t.Fatalf("approved retry could not re-enter governed dispatch progression: changed=%v value=%+v err=%v", changed, queued, err)
	}
	events, err := auditRepo.List(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "DELIVERY_EXCEPTION_RESOLVED" {
		t.Fatalf("missing governed retry audit: %+v", events)
	}
	var after map[string]any
	if err := json.Unmarshal(events[0].After, &after); err != nil {
		t.Fatal(err)
	}
	if accepted, ok := after["duplicateRiskAccepted"].(bool); !ok || !accepted {
		t.Fatalf("duplicate-risk acceptance missing from audit evidence: %s", events[0].After)
	}
}

func TestMarkFailedPermanentResolvesUnknownToPermanentFailure(t *testing.T) {
	ctx := context.Background()
	recipient := delivery.Recipient{ID: "recipient-unknown-permanent", Status: delivery.StatusUnknown, ReconciliationRequired: true, UpdatedAt: time.Now().UTC()}
	svc := &Service{Repo: NewMemoryRepository(), Deliveries: delivery.NewService(delivery.NewMemoryRepository(recipient))}
	resolution, err := svc.ResolveDeliveryException(ctx, recipient.ID, ResolutionMarkFailedPermanent, "provider-proof-789", "provider confirms permanent failure", "actor", "req-permanent")
	if err != nil {
		t.Fatal(err)
	}
	if resolution.ResultStatus != delivery.StatusFailedPermanent {
		t.Fatalf("permanent resolution status=%s want %s", resolution.ResultStatus, delivery.StatusFailedPermanent)
	}
	stored, _ := svc.Deliveries.Get(ctx, recipient.ID)
	if stored.Status != delivery.StatusFailedPermanent || stored.ReconciliationRequired {
		t.Fatalf("permanent resolution did not close UNKNOWN: %+v", stored)
	}
}

func TestConfirmNotSubmittedRejectsProviderCorrelation(t *testing.T) {
	ctx := context.Background()
	recipient := delivery.Recipient{ID: "recipient-unknown", Status: delivery.StatusUnknown, ProviderMessageID: "provider-present", ReconciliationRequired: true, UpdatedAt: time.Now().UTC()}
	svc := &Service{Repo: NewMemoryRepository(), Deliveries: delivery.NewService(delivery.NewMemoryRepository(recipient))}
	_, err := svc.ResolveDeliveryException(ctx, recipient.ID, ResolutionConfirmNotSubmitted, "gateway-log-123", "gateway evidence checked", "actor", "req")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected duplicate-risk conflict, got %v", err)
	}
}

func TestCampaignFinancialReconciliationReturnsCanonicalSnapshot(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 5, 19, 30, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	repo.financial["campaign-1"] = CampaignFinancialReconciliation{
		CampaignID:           "campaign-1",
		OrganisationID:       "organisation-1",
		CampaignName:         "Festival reminder",
		CampaignStatus:       "COMPLETED",
		CommercialStatus:     "APPROVED",
		ApprovedRecipients:   100,
		RecipientObligations: 100,
		Delivered:            92,
		Failed:               8,
		ReconciliationStatus: FinancialReconciliationBalanced,
	}
	svc := &Service{Repo: repo, Clock: func() time.Time { return now }}
	got, err := svc.CampaignFinancialReconciliation(ctx, "campaign-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReconciliationStatus != FinancialReconciliationBalanced || got.GeneratedAt != now {
		t.Fatalf("unexpected reconciliation: %+v", got)
	}
}

func TestOrganisationPerformanceReportReturnsCanonicalSnapshot(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 5, 20, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	repo.organisationReports["organisation-1"] = OrganisationPerformanceReport{
		OrganisationID:   "organisation-1",
		OrganisationName: "ABC Events",
		Campaigns:        map[string]int64{"COMPLETED": 2},
		Recipients:       map[string]int64{"authorised": 1000},
		Delivery:         map[string]int64{"delivered": 900},
		Commercial:       []CurrencyCommercialSummary{{Currency: "NGN", Campaigns: 2, ApprovedRecipients: 1000, ApprovedAmountMinor: 25000000}},
	}
	svc := &Service{Repo: repo, Clock: func() time.Time { return now }}
	got, err := svc.OrganisationPerformanceReport(ctx, "organisation-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.OrganisationName != "ABC Events" || got.GeneratedAt != now || len(got.Commercial) != 1 {
		t.Fatalf("unexpected organisation report: %+v", got)
	}
}
