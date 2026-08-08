package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/privacy"
	sharedcrypto "campaign-platform/internal/shared/crypto"
	"campaign-platform/internal/shared/httpx"
)

func TestPrivacyLegalHoldsReturnCursorContinuation(t *testing.T) {
	protector, err := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := privacy.NewMemoryRepository()
	service := &privacy.Service{Repository: repo, Protector: protector}
	base := time.Date(2026, 8, 8, 7, 30, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		service.Clock = func() time.Time { return at }
		if _, err := service.CreateLegalHold(context.Background(), "+2348012345678", "", "CONTACT", "pagination evidence hold", "legal-maker", "corr", nil); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{PrivacyCases: service}}
	verified := time.Now().UTC()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/privacy/legal-holds?msisdn=%2B2348012345678&limit=2", nil)
	request = request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{User: identity.User{ID: "legal-reviewer"}, Session: identity.Session{MFAVerifiedAt: &verified}}))
	response := httptest.NewRecorder()
	server.listPrivacyLegalHolds(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected legal-hold page: %+v body=%s", page, response.Body.String())
	}
}
