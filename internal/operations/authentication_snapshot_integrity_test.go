package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/identity"
)

func TestLoadAuthenticationEventSnapshotRejectsHeadMismatch(t *testing.T) {
	capturedAt := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	criteria := auditExportCriteria{}
	criteriaHash, err := authenticationCriteriaHash(criteria)
	if err != nil {
		t.Fatalf("hash criteria: %v", err)
	}

	tests := []struct {
		name   string
		events []AuthenticationEventExportRow
		headID string
	}{
		{
			name: "head differs from first frozen row",
			events: []AuthenticationEventExportRow{{
				Source: "authentication", ID: "00000000-0000-4000-8000-000000000001",
				EventType: "LOGIN_SUCCEEDED", OccurredAt: capturedAt,
			}},
			headID: "00000000-0000-4000-8000-000000000002",
		},
		{
			name:   "empty snapshot has a head",
			events: []AuthenticationEventExportRow{},
			headID: "00000000-0000-4000-8000-000000000001",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frozen, err := json.Marshal(frozenAuthenticationSnapshot{
				Version: authenticationSnapshotVersion, CriteriaHash: criteriaHash,
				CapturedAt: capturedAt, Events: test.events,
			})
			if err != nil {
				t.Fatalf("marshal snapshot: %v", err)
			}
			job := ExportRequest{
				TemplateVersion: auditExportTemplateVersion, FrozenPayload: frozen,
				AuthenticationEventAsOf: &capturedAt, AuthenticationEventHeadID: test.headID,
			}
			if _, err := loadAuthenticationEventSnapshot(job, criteria); err == nil {
				t.Fatal("expected mismatched authentication event head to be rejected")
			}
		})
	}
}

func TestAuditExportCriteriaRejectsUnknownFieldsAndTrailingValues(t *testing.T) {
	tests := []json.RawMessage{
		json.RawMessage(`{"organisation":"tenant-a"}`),
		json.RawMessage(`{} {}`),
	}
	for _, raw := range tests {
		if _, err := decodeAuditExportCriteria(raw); err == nil {
			t.Fatalf("expected criteria %s to be rejected", raw)
		}
	}

	service := &Service{Repo: NewMemoryRepository()}
	_, err := service.RequestExportWithOptions(
		context.Background(), "AUDIT_LOG", "", "JSON", "controlled evidence", "maker", "request",
		ExportOptions{Criteria: map[string]any{"organisation": "tenant-a"}},
	)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("request with unknown criteria field error = %v, want %v", err, ErrInvalid)
	}
}

func TestAuthenticationEventSnapshotPayloadSizeBound(t *testing.T) {
	ctx := context.Background()
	capturedAt := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	recorder := identity.NewMemoryEventRecorder()
	if err := recorder.Append(ctx, identity.AuthenticationEvent{
		ID: "00000000-0000-4000-8000-000000000001", Type: identity.EventLoginSucceeded,
		CorrelationID: strings.Repeat("x", maxAuthenticationSnapshotBytes), OccurredAt: capturedAt,
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{AuthenticationEvents: recorder}
	if _, _, _, err := service.freezeAuthenticationEventSnapshot(ctx, auditExportCriteria{}, capturedAt); !errors.Is(err, errAuthenticationEventSnapshotPayloadTooLarge) {
		t.Fatalf("oversized captured snapshot error = %v, want %v", err, errAuthenticationEventSnapshotPayloadTooLarge)
	}

	criteria := auditExportCriteria{}
	criteriaHash, err := authenticationCriteriaHash(criteria)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := json.Marshal(frozenAuthenticationSnapshot{
		Version: authenticationSnapshotVersion, CriteriaHash: criteriaHash,
		CapturedAt: capturedAt, Events: []AuthenticationEventExportRow{},
	})
	if err != nil {
		t.Fatal(err)
	}
	frozen = append(frozen, bytes.Repeat([]byte(" "), maxAuthenticationSnapshotBytes-len(frozen))...)
	job := ExportRequest{
		TemplateVersion: auditExportTemplateVersion, FrozenPayload: frozen,
		AuthenticationEventAsOf: &capturedAt,
	}
	if _, err := loadAuthenticationEventSnapshot(job, criteria); err != nil {
		t.Fatalf("snapshot at encoded size limit was rejected: %v", err)
	}
	job.FrozenPayload = append(job.FrozenPayload, ' ')
	if _, err := loadAuthenticationEventSnapshot(job, criteria); !errors.Is(err, errAuthenticationEventSnapshotPayloadTooLarge) {
		t.Fatalf("oversized stored snapshot error = %v, want %v", err, errAuthenticationEventSnapshotPayloadTooLarge)
	}
}
