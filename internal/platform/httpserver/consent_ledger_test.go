package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	first := post(http.MethodGet, "/api/v1/consent-events?contactId=00000000-0000-4000-8000-000000000010&limit=1", "", "")
	var firstPage struct {
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"nextCursor"`
		HasMore    bool             `json:"hasMore"`
	}
	if first.Code != http.StatusOK || json.Unmarshal(first.Body.Bytes(), &firstPage) != nil || len(firstPage.Items) != 1 || firstPage.NextCursor == "" || !firstPage.HasMore {
		t.Fatalf("first consent page: %d %s", first.Code, first.Body.String())
	}
	second := post(http.MethodGet, "/api/v1/consent-events?contactId=00000000-0000-4000-8000-000000000010&limit=1&cursor="+url.QueryEscape(firstPage.NextCursor), "", "")
	var secondPage struct {
		Items []map[string]any `json:"items"`
	}
	if second.Code != http.StatusOK || json.Unmarshal(second.Body.Bytes(), &secondPage) != nil || len(secondPage.Items) != 1 || firstPage.Items[0]["id"] == secondPage.Items[0]["id"] {
		t.Fatalf("second consent page: %d %s", second.Code, second.Body.String())
	}
}
