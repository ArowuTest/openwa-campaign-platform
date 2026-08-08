package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/sender"
)

func TestUpdateSenderSessionMetadataIsStepUpGovernedAndHidesRecoveryReference(t *testing.T) {
	store := sender.NewMemoryGovernanceStore()
	governance := &sender.GovernanceService{Store: store}
	created, err := governance.RegisterSession(context.Background(), sender.GovernedSession{
		MaskedMSISDN: "+234 ***", OwnerReference: "team-a", RegistrationCountryISO2: "NG",
		ProfileDisplayName: "Primary sender", RecoveryReference: "vault://sender/recovery/old",
		EngineType: "WHATSAPP_WEB_JS", Status: sender.StatusNew,
		SafeMessagesPerMinute: 10, SafeDailyCapacity: 100, InFlightLimit: 1,
	}, []byte("cipher"), "actor", "approved sender")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/sender-sessions/"+created.ID+"/metadata", bytes.NewBufferString(`{"ownerReference":"team-b","registrationCountryIso2":"GH","profileDisplayName":"Ghana sender","recoveryReference":"vault://sender/recovery/new","expectedVersion":1,"reason":"approved ownership change"}`))
	request.SetPathValue("id", created.ID)
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{
		User:    identity.User{ID: "actor", Permissions: map[string]struct{}{"sender.admin": {}}},
		Session: identity.Session{MFAVerifiedAt: &now},
	}))
	response := httptest.NewRecorder()
	server := &Server{deps: Dependencies{SenderGovernance: governance}}
	server.updateSenderSessionMetadata(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte("vault://sender/recovery/new")) {
		t.Fatalf("recovery reference leaked in response: %s", response.Body.String())
	}
	stored, err := store.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RecoveryReference != "vault://sender/recovery/new" || stored.OwnerReference != "team-b" || stored.Version != 2 {
		t.Fatalf("metadata not updated: %+v", stored)
	}
}

func TestListSenderSessionsReturnsCursorContinuation(t *testing.T) {
	store := sender.NewMemoryGovernanceStore()
	governance := &sender.GovernanceService{Store: store}
	for i, masked := range []string{"+234 *** 01", "+234 *** 02", "+234 *** 03"} {
		_, err := governance.RegisterSession(context.Background(), sender.GovernedSession{
			MaskedMSISDN: masked, OwnerReference: "team-a", RegistrationCountryISO2: "NG",
			ProfileDisplayName: masked, RecoveryReference: "vault://sender/recovery/ref",
			EngineType: "WHATSAPP_WEB_JS", Status: sender.StatusNew,
			SafeMessagesPerMinute: 10 + i, SafeDailyCapacity: 100, InFlightLimit: 1,
		}, []byte("cipher"), "actor", "approved sender")
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sender-sessions?limit=2", nil)
	response := httptest.NewRecorder()
	server := &Server{deps: Dependencies{SenderGovernance: governance}}
	server.listSenderSessions(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page struct {
		Items      []sender.GovernedSession `json:"items"`
		Count      int                      `json:"count"`
		NextCursor string                   `json:"nextCursor"`
		HasMore    bool                     `json:"hasMore"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected list page: %+v body=%s", page, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/sender-sessions?limit=2&cursor="+url.QueryEscape(page.NextCursor), nil)
	response = httptest.NewRecorder()
	server.listSenderSessions(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", response.Code, response.Body.String())
	}
	var second struct {
		Items      []sender.GovernedSession `json:"items"`
		NextCursor string                   `json:"nextCursor"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatalf("unexpected second page: %+v body=%s", second, response.Body.String())
	}
}
