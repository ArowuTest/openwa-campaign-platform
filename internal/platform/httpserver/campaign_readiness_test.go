package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCampaignReadinessContract(t *testing.T) {
	for _, tc := range []struct {
		name, permission, token string
		status                  int
		code                    string
	}{
		{"unauthenticated", "campaign.read", "", 401, "AUTHENTICATION_REQUIRED"},
		{"forbidden", "", "valid", 403, "PERMISSION_DENIED"},
		{"unwired", "campaign.read", "valid", 503, "CAMPAIGN_READINESS_UNAVAILABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, token := campaignDetailHandler(t, nil, tc.permission)
			r := httptest.NewRequest(http.MethodGet, "/api/v1/campaigns/"+campaignDetailID+"/readiness", nil)
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+token)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("got %d %s; want %d %s", w.Code, w.Body, tc.status, tc.code)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("readiness can be cached")
			}
		})
	}
}
