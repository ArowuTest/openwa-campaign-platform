package operations

import (
	"context"
	"testing"
	"time"

	"campaign-platform/internal/audit"
	"campaign-platform/internal/identity"
)

func TestAuthenticationEventSearchRecordsAuditEvidence(t *testing.T) {
	ctx := context.Background()
	events := identity.NewMemoryEventRecorder()
	if err := events.Append(ctx, identity.AuthenticationEvent{
		ID:            "00000000-0000-4000-8000-000000000201",
		UserID:        "00000000-0000-4000-8000-000000000101",
		Type:          identity.EventLoginFailed,
		CorrelationID: "auth-correlation",
		OccurredAt:    time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		Metadata:      map[string]string{"reason": "invalid credentials"},
	}); err != nil {
		t.Fatal(err)
	}
	auditRepo := audit.NewMemoryRepository()
	svc := &Service{
		AuthenticationEvents: events,
		Audit:                audit.NewRecorder(auditRepo),
		Clock:                func() time.Time { return time.Date(2026, 10, 12, 0, 1, 0, 0, time.UTC) },
	}
	page, err := svc.SearchAuthenticationEventsWithAccess(ctx, identity.AuthenticationEventQuery{Limit: 10}, "00000000-0000-4000-8000-000000000001", "request-auth-search")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Type != identity.EventLoginFailed {
		t.Fatalf("unexpected authentication page: %+v", page)
	}
	audited, err := auditRepo.List(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(audited) != 1 || audited[0].Action != "AUDIT_LOG_SEARCHED" || audited[0].ObjectType != "AUTHENTICATION_EVENTS" {
		t.Fatalf("authentication search was not audited: %+v", audited)
	}
}
