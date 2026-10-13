package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/inbound"
	"campaign-platform/internal/shared/httpx"
)

type inboundPaginationRepository struct {
	*inbound.MemoryRepository
	items []inbound.Reply
}

func (r *inboundPaginationRepository) ListReplyPage(_ context.Context, limit int, _ *time.Time, _ string) ([]inbound.Reply, error) {
	if len(r.items) > limit {
		return append([]inbound.Reply(nil), r.items[:limit]...), nil
	}
	return append([]inbound.Reply(nil), r.items...), nil
}

type rotationPaginationRepository struct {
	*inbound.MemoryRotationRepository
	items []inbound.RotationRun
}

func (r *rotationPaginationRepository) ListRunPage(_ context.Context, limit int, _ *time.Time, _ string) ([]inbound.RotationRun, error) {
	if len(r.items) > limit {
		return append([]inbound.RotationRun(nil), r.items[:limit]...), nil
	}
	return append([]inbound.RotationRun(nil), r.items...), nil
}

func TestInboundRepliesReturnCursorContinuationWithoutContentLeak(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := &inboundPaginationRepository{MemoryRepository: inbound.NewMemoryRepository(), items: []inbound.Reply{
		{ID: "reply-c", MessageText: "secret-c", CreatedAt: base.Add(2 * time.Minute)},
		{ID: "reply-b", MessageText: "secret-b", CreatedAt: base.Add(time.Minute)},
		{ID: "reply-a", MessageText: "secret-a", CreatedAt: base},
	}}
	server := &Server{deps: Dependencies{InboundReplies: &inbound.Service{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/inbound-replies?limit=2", nil)
	request = request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{User: identity.User{ID: "reader", Permissions: map[string]struct{}{"inbound.read": {}}}}))
	response := httptest.NewRecorder()
	server.listInboundReplies(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected inbound-reply page: %+v body=%s", page, response.Body.String())
	}
	if json.Valid(response.Body.Bytes()) && (containsJSONText(response.Body.String(), "secret-c") || containsJSONText(response.Body.String(), "secret-b")) {
		t.Fatalf("message content leaked in paged list: %s", response.Body.String())
	}
}

func TestInboundRepliesNeverExposePlaintextThroughListEvenToContentReaders(t *testing.T) {
	repo := &inboundPaginationRepository{
		MemoryRepository: inbound.NewMemoryRepository(),
		items: []inbound.Reply{{
			ID: "reply-c", MessageText: "SENSITIVE_LIST_BODY_MUST_NEVER_LEAK",
			MessageFingerprint:  "SENSITIVE_GUESSABLE_CONTENT_FINGERPRINT",
			LegalHoldReason:     "CONFIDENTIAL_LEGAL_CASE_REASON",
			LegalHoldAppliedBy:  "CONFIDENTIAL_LEGAL_STAFF_ACTOR",
			LegalHoldReleasedBy: "CONFIDENTIAL_RELEASE_STAFF_ACTOR",
			EscalationReason:    "ESCALATION_REASON_QUOTES_PRIVATE_REPLY",
			Classification:      inbound.ClassificationUnreviewed,
			CreatedAt:           time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC),
		}},
	}
	server := &Server{deps: Dependencies{InboundReplies: &inbound.Service{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/inbound-replies?limit=50", nil)
	request = request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{User: identity.User{
		ID: "audited-content-reader", Permissions: map[string]struct{}{
			"inbound.read": {}, "inbound.content.read": {},
		},
	}}))
	response := httptest.NewRecorder()
	server.listInboundReplies(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d response=%s", response.Code, response.Body.String())
	}
	for _, forbidden := range []string{
		"SENSITIVE_LIST_BODY_MUST_NEVER_LEAK",
		"SENSITIVE_GUESSABLE_CONTENT_FINGERPRINT",
		"CONFIDENTIAL_LEGAL_CASE_REASON",
		"CONFIDENTIAL_LEGAL_STAFF_ACTOR",
		"CONFIDENTIAL_RELEASE_STAFF_ACTOR",
		"ESCALATION_REASON_QUOTES_PRIVATE_REPLY",
		"messageText",
		"legalHoldReason",
		"escalationReason",
	} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("inbound metadata list leaked confidential %q: %s", forbidden, response.Body.String())
		}
	}
}

// This allowlist is deliberately separate from inbound.Reply so newly added
// database/audit fields cannot silently become available to inbound.read.
func TestInboundReplyListIsExplicitMinimalMetadataProjection(t *testing.T) {
	repo := &inboundPaginationRepository{
		MemoryRepository: inbound.NewMemoryRepository(),
		items: []inbound.Reply{{
			ID:                 "reply-opaque",
			EventID:            "event-opaque",
			ContactID:          "contact-opaque",
			RecipientID:        "SECRET_RECIPIENT_INTERNAL_IDENTIFIER",
			ProviderMessageID:  "SECRET_PROVIDER_MESSAGE_IDENTIFIER",
			ReviewedBy:         "SECRET_REVIEW_OPERATOR_IDENTIFIER",
			MessageText:        "SECRET_MESSAGE_TEXT",
			MessageFingerprint: "SECRET_MESSAGE_DIGEST",
			LegalHoldReason:    "SECRET_LEGAL_CASE_NOTES",
			EscalationReason:   "SECRET_ESCALATION_NOTE",
			CreatedAt:          time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC),
		}},
	}
	server := &Server{deps: Dependencies{InboundReplies: &inbound.Service{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/inbound-replies?limit=50", nil)
	request = request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{User: identity.User{
		ID:          "reader",
		Permissions: map[string]struct{}{"inbound.read": {}, "inbound.content.read": {}},
	}}))
	response := httptest.NewRecorder()
	server.listInboundReplies(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d response=%s", response.Code, response.Body.String())
	}
	var page struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected one reply item, got %d", len(page.Items))
	}
	// Only fields needed for the operations workbench may leave this endpoint.
	// ContactID stays available for the separately authorised consent.write
	// suppression workflow; it is an internal contact ID, not an MSISDN.
	allowed := map[string]bool{
		"id": true, "eventId": true, "contactId": true,
		"campaignId": true, "sessionId": true,
		"classification": true, "escalated": true,
		"occurredAt": true, "createdAt": true, "contentRetainUntil": true,
		"contentRedactedAt": true, "legalHold": true, "version": true,
		"reviewedAt": true,
	}
	for field := range page.Items[0] {
		if !allowed[field] {
			t.Fatalf("inbound.read returned non-allowlisted field %q", field)
		}
	}
	for _, required := range []string{"id", "eventId", "contactId", "classification", "version", "legalHold"} {
		if _, ok := page.Items[0][required]; !ok {
			t.Fatalf("missing required metadata field %q", required)
		}
	}
	var contactID string
	if err := json.Unmarshal(page.Items[0]["contactId"], &contactID); err != nil || contactID != "contact-opaque" {
		t.Fatalf("internal contact reference was not retained: %q (%v)", contactID, err)
	}
}

func TestInboundReplyListPublicRouteRequiresAuthenticatedPermission(t *testing.T) {
	handler, _ := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/inbound-replies?limit=5", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated inbound list status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "AUTHENTICATION_REQUIRED") {
		t.Fatalf("inbound list did not enforce the shared authentication boundary: %s", response.Body.String())
	}
}

func TestInboundMutationResponsesNeverDiscloseContentOrOtherCaseNarratives(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		body       string
		permission string
		handler    func(*Server, http.ResponseWriter, *http.Request)
	}{
		{
			name:       "review",
			path:       "/api/v1/inbound-replies/reply-1/review",
			body:       `{"expectedVersion":1,"classification":"COMPLAINT","escalated":true,"escalationReason":"SECRET_NEW_ESCALATION_CASE_NARRATIVE"}`,
			permission: "inbound.review",
			handler:    (*Server).reviewInboundReply,
		},
		{
			name:       "legal-hold",
			path:       "/api/v1/inbound-replies/reply-1/legal-hold",
			body:       `{"expectedVersion":1,"legalHold":true,"reason":"SECRET_NEW_LEGAL_CASE_NARRATIVE"}`,
			permission: "privacy.admin",
			handler:    (*Server).setInboundReplyLegalHold,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := inbound.NewMemoryRepository()
			reply := inbound.Reply{
				ID: "reply-1", EventID: "event-1", ContactID: "contact-1",
				RecipientID:        "SECRET_RECIPIENT_KEY",
				ProviderMessageID:  "SECRET_PROVIDER_KEY",
				MessageText:        "SECRET_RETAINED_MESSAGE_TEXT",
				MessageFingerprint: "SECRET_MESSAGE_DIGEST",
				EscalationReason:   "SECRET_PREVIOUS_ESCALATION",
				LegalHoldReason:    "SECRET_PRIOR_LEGAL_CASE",
				LegalHoldAppliedBy: "SECRET_PRIOR_STAFF_ACTOR",
				Version:            1,
			}
			if _, _, err := repo.Create(context.Background(), reply); err != nil {
				t.Fatal(err)
			}
			server := &Server{deps: Dependencies{
				InboundReplies: &inbound.Service{Repository: repo, Audit: &testInboundAudit{}},
			}}
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.SetPathValue("id", reply.ID)
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(identity.WithPrincipal(req.Context(), identity.Principal{
				User: identity.User{ID: "authorised-operator", Permissions: map[string]struct{}{tc.permission: {}}},
			}))
			response := httptest.NewRecorder()
			tc.handler(server, response, req)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			for _, forbidden := range []string{
				"SECRET_", "messageText", "messageFingerprint", "legalHoldReason",
				"legalHoldAppliedBy", "escalationReason", "reviewedBy", "recipientId", "providerMessageId",
			} {
				if strings.Contains(response.Body.String(), forbidden) {
					t.Fatalf("sensitive mutation response leaked %q: %s", forbidden, response.Body.String())
				}
			}
			var result map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result["id"] != reply.ID || result["version"] != float64(2) {
				t.Fatalf("mutation did not preserve safe identity and updated version: %v", result)
			}
		})
	}
}

func containsJSONText(body, value string) bool {
	return len(value) > 0 && strings.Contains(body, value)
}

func TestInboundReencryptionRunsReturnCursorContinuation(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := &rotationPaginationRepository{MemoryRotationRepository: inbound.NewMemoryRotationRepository(), items: []inbound.RotationRun{
		{ID: "run-c", RequestedAt: base.Add(2 * time.Minute)},
		{ID: "run-b", RequestedAt: base.Add(time.Minute)},
		{ID: "run-a", RequestedAt: base},
	}}
	server := &Server{deps: Dependencies{InboundRotation: &inbound.RotationService{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/inbound-reencryption-runs?limit=2", nil)
	response := httptest.NewRecorder()
	server.listInboundReencryptionRuns(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected re-encryption page: %+v body=%s", page, response.Body.String())
	}
}
