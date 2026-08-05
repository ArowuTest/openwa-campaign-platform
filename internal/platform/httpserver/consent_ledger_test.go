package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsentLedgerHTTPWorkflow(t *testing.T) {
	handler, token := testServer(t)
	post := func(method, path, body, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	grantBody := `{"contactId":"00000000-0000-4000-8000-000000000010","organisationId":"00000000-0000-4000-8000-000000000020","purposeId":"PROMOTION","channel":"WHATSAPP","consentReviewId":"00000000-0000-4000-8000-000000000030","wordingVersion":"v1","sourceType":"WEB_FORM","evidenceChecksum":"abc123"}`
	created := post(http.MethodPost, "/api/v1/consent-grants", grantBody, "grant-request-0001")
	if created.Code != http.StatusCreated {
		t.Fatalf("create grant: %d %s", created.Code, created.Body.String())
	}
	replay := post(http.MethodPost, "/api/v1/consent-grants", grantBody, "grant-request-0001")
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), `"created":false`) {
		t.Fatalf("grant replay: %d %s", replay.Code, replay.Body.String())
	}
	suppression := post(http.MethodPost, "/api/v1/suppressions", `{"contactId":"00000000-0000-4000-8000-000000000010","scope":"GLOBAL","reason":"STOP reply"}`, "suppression-request-0001")
	if suppression.Code != http.StatusCreated {
		t.Fatalf("create suppression: %d %s", suppression.Code, suppression.Body.String())
	}
	events := post(http.MethodGet, "/api/v1/consent-events?contactId=00000000-0000-4000-8000-000000000010", "", "")
	if events.Code != http.StatusOK || !strings.Contains(events.Body.String(), "SUPPRESSION_CREATED") || !strings.Contains(events.Body.String(), "GRANT_CREATED") {
		t.Fatalf("events: %d %s", events.Code, events.Body.String())
	}
}
