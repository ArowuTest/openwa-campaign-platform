package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/audit"
	"campaign-platform/internal/identity"
)

func TestAuditExportIncludesRedactedAuthenticationStream(t *testing.T) {
	ctx := context.Background()
	auditRepository := audit.NewMemoryRepository()
	auditRecorder := audit.NewRecorder(auditRepository)
	if _, err := auditRecorder.Record(ctx, audit.Input{
		ActorType: "USER", ActorID: "operator-1", Action: "EXPORT_REQUESTED",
		ObjectType: "EXPORT_REQUEST", ObjectID: "export-1", CorrelationID: "corr-business",
		OccurredAt: time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	sequence, hash, err := auditRepository.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}

	authRepository := identity.NewMemoryEventRecorder()
	boundary := time.Date(2026, 10, 12, 3, 5, 0, 0, time.UTC)
	headID := "00000000-0000-4000-8000-000000000010"
	if err := authRepository.Append(ctx, identity.AuthenticationEvent{
		ID: headID, UserID: "00000000-0000-4000-8000-000000000001",
		EmailHash: "email-secret", SourceIP: "203.0.113.10", UserAgent: "private-agent",
		Type: identity.EventMFASucceeded, CorrelationID: "corr-auth",
		OccurredAt: boundary.Add(789 * time.Nanosecond), Metadata: map[string]string{"secret": "metadata-secret"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := authRepository.Append(ctx, identity.AuthenticationEvent{
		ID:     "00000000-0000-4000-8000-000000000009",
		UserID: "00000000-0000-4000-8000-000000000003",
		Type:   identity.EventLoginFailed, CorrelationID: "corr-old",
		OccurredAt: boundary.Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	service := &Service{AuthenticationEvents: authRepository}
	frozen, capturedAt, capturedHead, err := service.freezeAuthenticationEventSnapshot(ctx, auditExportCriteria{}, boundary)
	if err != nil {
		t.Fatal(err)
	}
	if err := authRepository.Append(ctx, identity.AuthenticationEvent{
		ID:     "00000000-0000-4000-8000-000000000011",
		UserID: "00000000-0000-4000-8000-000000000002",
		Type:   identity.EventLoginSucceeded, CorrelationID: "after-head", OccurredAt: boundary,
	}); err != nil {
		t.Fatal(err)
	}
	// Source access is no longer necessary after approval: rendering must use
	// only the immutable redacted rows captured by the approval read.
	worker := &ExportWorker{AuditRepository: auditRepository}
	job := ExportRequest{
		Kind: "AUDIT_LOG", Criteria: json.RawMessage("{}"), TemplateVersion: auditExportTemplateVersion,
		AuditHeadSequence: sequence, AuditHeadHash: hash, FrozenPayload: frozen,
		AuthenticationEventAsOf: capturedAt, AuthenticationEventHeadID: capturedHead,
	}
	payload, err := worker.loadFrozenAudit(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.BusinessEvents) != 1 || len(payload.AuthenticationEvents) != 2 {
		t.Fatalf("unexpected export payload: %+v", payload)
	}
	if payload.AuthenticationEvents[0].ID != headID {
		t.Fatalf("expected frozen head event first, got %+v", payload.AuthenticationEvents)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"email-secret", "203.0.113.10", "private-agent", "metadata-secret"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("authentication export leaked %q: %s", secret, raw)
		}
	}
	csvPayload, err := renderCSV(payload)
	if err != nil {
		t.Fatal(err)
	}
	csvText := string(csvPayload)
	if !strings.Contains(csvText, "authentication") || !strings.Contains(csvText, "MFA_SUCCEEDED") {
		t.Fatalf("CSV omitted redacted authentication row: %s", csvText)
	}
	if strings.Contains(csvText, "email-secret") || strings.Contains(csvText, "private-agent") {
		t.Fatalf("CSV leaked authentication secret: %s", csvText)
	}
}

func TestAuditExportApprovalFreezesAuthenticationWatermark(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 12, 3, 10, 0, 123456789, time.UTC)
	auditRepository := audit.NewMemoryRepository()
	if _, err := audit.NewRecorder(auditRepository).Record(ctx, audit.Input{
		ActorType: "USER", ActorID: "operator-1", Action: "AUDIT_LOG_SEARCHED",
		ObjectType: "AUDIT_LOG", ObjectID: "AUDIT_SEARCH", CorrelationID: "corr-1", OccurredAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	authRepository := identity.NewMemoryEventRecorder()
	authID := "00000000-0000-4000-8000-000000000020"
	if err := authRepository.Append(ctx, identity.AuthenticationEvent{
		ID: authID, UserID: "00000000-0000-4000-8000-000000000001",
		Type: identity.EventLoginSucceeded, CorrelationID: "corr-auth", OccurredAt: now.Add(-time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	repository := NewMemoryRepository()
	service := &Service{
		Repo: repository, AuditRepository: auditRepository, AuthenticationEvents: authRepository,
		Clock: func() time.Time { return now },
	}
	request, err := service.RequestExport(ctx, "AUDIT_LOG", "", "JSON", "controlled evidence", "maker", "corr-request")
	if err != nil {
		t.Fatal(err)
	}
	if request.TemplateVersion != auditExportTemplateVersion {
		t.Fatalf("unexpected audit export template version: %q", request.TemplateVersion)
	}
	approved, err := service.DecideExport(ctx, request.ID, request.Version, true, "", "approver", "corr-approval")
	if err != nil {
		t.Fatal(err)
	}
	if approved.AuthenticationEventAsOf == nil || approved.AuthenticationEventAsOf.IsZero() {
		t.Fatalf("approval did not freeze authentication as-of: %+v", approved)
	}
	if approved.AuthenticationEventHeadID != authID {
		t.Fatalf("approval froze head %q, want %q", approved.AuthenticationEventHeadID, authID)
	}

	missing := &Service{Repo: repository, AuditRepository: auditRepository, Clock: func() time.Time { return now }}
	second, err := missing.RequestExport(ctx, "AUDIT_LOG", "", "JSON", "controlled evidence", "maker-2", "corr-request-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := missing.DecideExport(ctx, second.ID, second.Version, true, "", "approver-2", "corr-approval-2"); err == nil {
		t.Fatal("expected approval to fail closed without authentication event repository")
	}
}

func TestAuthenticationExportCriteriaDoesNotBroadenTheAuthStream(t *testing.T) {
	cases := []struct {
		name      string
		criteria  auditExportCriteria
		include   bool
		wantUser  string
		wantError bool
	}{
		{name: "business-only filter is isolated", criteria: auditExportCriteria{Action: "CAMPAIGN_VIEW"}, include: false},
		{name: "uuid actor shorthand scopes auth", criteria: auditExportCriteria{ActorID: "00000000-0000-4000-8000-000000000001"}, include: true, wantUser: "00000000-0000-4000-8000-000000000001"},
		{name: "explicit auth scope wins", criteria: auditExportCriteria{UserID: "00000000-0000-4000-8000-000000000001", Action: "CAMPAIGN_VIEW"}, include: true, wantUser: "00000000-0000-4000-8000-000000000001"},
		{name: "invalid auth scope fails closed", criteria: auditExportCriteria{EventType: "NOT_A_REAL_EVENT"}, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			query, include, err := authenticationExportQuery(tc.criteria)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v wantError=%v", err, tc.wantError)
			}
			if err != nil {
				return
			}
			if include != tc.include || query.UserID != tc.wantUser {
				t.Fatalf("query=%+v include=%v", query, include)
			}
		})
	}
}

func TestAuditExportApprovalExcludesLateLowerUUIDAndBackdatedEvents(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 12, 3, 10, 0, 123456000, time.UTC)
	authRepository := identity.NewMemoryEventRecorder()
	original := identity.AuthenticationEvent{
		ID:   "00000000-0000-4000-8000-000000000020",
		Type: identity.EventLoginSucceeded, CorrelationID: "before-approval", OccurredAt: now,
	}
	if err := authRepository.Append(ctx, original); err != nil {
		t.Fatal(err)
	}
	auditRepository := audit.NewMemoryRepository()
	service := &Service{
		Repo: NewMemoryRepository(), AuditRepository: auditRepository,
		AuthenticationEvents: authRepository, Clock: func() time.Time { return now },
	}
	request, err := service.RequestExport(ctx, "AUDIT_LOG", "", "JSON", "controlled evidence", "maker", "request")
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.DecideExport(ctx, request.ID, request.Version, true, "", "checker", "approval")
	if err != nil {
		t.Fatal(err)
	}
	for _, late := range []identity.AuthenticationEvent{
		{ID: "00000000-0000-4000-8000-000000000019", Type: identity.EventLoginFailed, CorrelationID: "late-lower-uuid", OccurredAt: now},
		{ID: "00000000-0000-4000-8000-000000000018", Type: identity.EventLoginFailed, CorrelationID: "late-backdated", OccurredAt: now.Add(-time.Hour)},
	} {
		if err := authRepository.Append(ctx, late); err != nil {
			t.Fatal(err)
		}
	}
	worker := &ExportWorker{AuditRepository: auditRepository, AuthenticationEvents: authRepository}
	payload, err := worker.loadFrozenAudit(ctx, approved)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.AuthenticationEvents) != 1 || payload.AuthenticationEvents[0].ID != original.ID {
		t.Fatalf("approval snapshot admitted later events: %+v", payload.AuthenticationEvents)
	}
}

func TestAuditExportNormalizesPendingLegacyTemplateAtApproval(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 13, 3, 0, 0, 0, time.UTC)
	service := &Service{Repo: NewMemoryRepository(), AuditRepository: audit.NewMemoryRepository(), AuthenticationEvents: identity.NewMemoryEventRecorder(), Clock: func() time.Time { return now }}
	request, err := service.RequestExportWithOptions(ctx, "AUDIT_LOG", "", "JSON", "controlled evidence", "maker", "request", ExportOptions{TemplateVersion: "AUDIT-EXPORT-V2"})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.DecideExport(ctx, request.ID, request.Version, true, "", "checker", "approval")
	if err != nil {
		t.Fatal(err)
	}
	if approved.TemplateVersion != auditExportTemplateVersion || len(approved.FrozenPayload) == 0 {
		t.Fatalf("legacy pending approval did not produce immutable snapshot: %+v", approved)
	}
}

func TestBusinessOnlyExportFreezesEmptyAuthenticationStreamWithoutSource(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 13, 3, 0, 0, 0, time.UTC)
	repository := audit.NewMemoryRepository()
	service := &Service{Repo: NewMemoryRepository(), AuditRepository: repository, Clock: func() time.Time { return now }}
	request, err := service.RequestExportWithOptions(ctx, "AUDIT_LOG", "", "JSON", "controlled evidence", "maker", "request", ExportOptions{Criteria: map[string]any{"action": "CAMPAIGN_VIEW"}})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.DecideExport(ctx, request.ID, request.Version, true, "", "checker", "approval")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := (&ExportWorker{AuditRepository: repository}).loadFrozenAudit(ctx, approved)
	if err != nil {
		t.Fatal(err)
	}
	if payload.AuthenticationEvents == nil || len(payload.AuthenticationEvents) != 0 {
		t.Fatalf("business-only export broadened auth scope: %+v", payload.AuthenticationEvents)
	}
}

func TestAuthenticationExportRejectsMissingLegacyOrMismatchedSnapshot(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 13, 3, 0, 0, 0, time.UTC)
	service := &Service{AuthenticationEvents: identity.NewMemoryEventRecorder()}
	frozen, captured, head, err := service.freezeAuthenticationEventSnapshot(ctx, auditExportCriteria{}, now)
	if err != nil {
		t.Fatal(err)
	}
	valid := ExportRequest{Kind: "AUDIT_LOG", Criteria: json.RawMessage("{}"), TemplateVersion: auditExportTemplateVersion, FrozenPayload: frozen, AuthenticationEventAsOf: captured, AuthenticationEventHeadID: head}
	cases := []struct {
		name   string
		mutate func(*ExportRequest)
	}{
		{"missing", func(job *ExportRequest) { job.FrozenPayload = nil }},
		{"legacy", func(job *ExportRequest) { job.TemplateVersion = "AUDIT-EXPORT-V2" }},
		{"malformed", func(job *ExportRequest) { job.FrozenPayload = json.RawMessage("{}") }},
		{"different criteria", func(job *ExportRequest) { job.Criteria = json.RawMessage("{\"eventType\":\"LOGIN_FAILED\"}") }},
		{"different boundary", func(job *ExportRequest) { later := now.Add(time.Second); job.AuthenticationEventAsOf = &later }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			job := valid
			tc.mutate(&job)
			criteria, err := decodeAuditExportCriteria(job.Criteria)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := (&ExportWorker{}).loadFrozenAuthenticationEvents(ctx, job, criteria); err == nil {
				t.Fatal("expected fail-closed frozen snapshot rejection")
			}
		})
	}
}
