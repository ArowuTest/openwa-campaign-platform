package httpserver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
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
