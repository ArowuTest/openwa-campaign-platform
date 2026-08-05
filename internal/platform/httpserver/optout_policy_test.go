package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOptOutPolicyMakerCheckerHTTPWorkflow(t *testing.T) {
	h, token := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/opt-out-policies", strings.NewReader(`{"keywords":["stop","arrêter"],"reason":"support French opt-outs"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.NewDecoder(bytes.NewReader(w.Body.Bytes())).Decode(&created); err != nil {
		t.Fatal(err)
	}
	id := created["id"].(string)
	version := int64(created["version"].(float64))
	body := `{"expectedVersion":` + jsonNumber(version) + `,"reason":"ready for independent approval"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/opt-out-policies/"+id+"/submit", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("submit %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PENDING_APPROVAL") {
		t.Fatal(w.Body.String())
	}
}
func jsonNumber(v int64) string { b, _ := json.Marshal(v); return string(b) }
