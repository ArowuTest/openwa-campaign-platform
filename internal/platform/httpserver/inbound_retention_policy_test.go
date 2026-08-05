package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInboundRetentionPolicyHTTPWorkflow(t *testing.T) {
	h, token := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/inbound-retention-policies", strings.NewReader(`{"retentionDays":30,"reason":"reduce personal data exposure"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	var item map[string]any
	if err := json.NewDecoder(bytes.NewReader(w.Body.Bytes())).Decode(&item); err != nil {
		t.Fatal(err)
	}
	id := item["id"].(string)
	version := int64(item["version"].(float64))
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/inbound-retention-policies/"+id+"/submit", strings.NewReader(`{"expectedVersion":`+jsonNumber(version)+`,"reason":"ready for independent review"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "PENDING_APPROVAL") {
		t.Fatalf("submit %d %s", w.Code, w.Body.String())
	}
}
