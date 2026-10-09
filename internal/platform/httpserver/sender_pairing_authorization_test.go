package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/sender"
)

func TestSenderPairingPermissionBlocksNonAdministratorBeforeSensitiveHandler(t *testing.T) {
	hash, err := identity.HashPassword("Correct-Horse-Battery-17")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{
		ID: "pairing-reader", Email: "pairing-reader@example.test", DisplayName: "Pairing Reader",
		Status: identity.StatusActive, PasswordHash: hash, Permissions: map[string]struct{}{"sender.read": {}},
	}
	idService := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := idService.Login(context.Background(), user.Email, "Correct-Horse-Battery-17")
	if err != nil {
		t.Fatal(err)
	}

	s := &Server{deps: Dependencies{Identity: idService}}
	innerCalled := false
	protected := s.require("sender.admin", func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true
		_, _ = w.Write([]byte("SENSITIVE-PAIRING-PAYLOAD"))
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sender-sessions/session-1/pairing/qr", nil)
	request.Header.Set("Authorization", "Bearer "+login.SessionToken)
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if innerCalled {
		t.Fatal("non-administrator reached pairing payload handler")
	}
	if strings.Contains(response.Body.String(), "SENSITIVE-PAIRING-PAYLOAD") {
		t.Fatalf("pairing payload leaked to unauthorised role: %s", response.Body.String())
	}
}

func TestSenderPairingRequiresRecentMFAStepUpForAdministrator(t *testing.T) {
	hash, err := identity.HashPassword("Correct-Horse-Battery-18")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{
		ID: "pairing-admin", Email: "pairing-admin@example.test", DisplayName: "Pairing Admin",
		Status: identity.StatusActive, PasswordHash: hash, MFARequired: false,
		Permissions: map[string]struct{}{"sender.admin": {}},
	}
	idService := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := idService.Login(context.Background(), user.Email, "Correct-Horse-Battery-18")
	if err != nil {
		t.Fatal(err)
	}

	s := &Server{deps: Dependencies{Identity: idService}}
	protected := s.require("sender.admin", s.getGatewaySenderSessionQR)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sender-sessions/session-1/pairing/qr", nil)
	request.Header.Set("Authorization", "Bearer "+login.SessionToken)
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "STEP_UP_REQUIRED") {
		t.Fatalf("pairing request was not stopped by step-up gate: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "SENDER_LIFECYCLE_UNAVAILABLE") {
		t.Fatalf("pairing request reached lifecycle before step-up: %s", response.Body.String())
	}
}

func TestPairingCodeHandlerHonoursVersionReasonAndSerializesBeforeGateway(t *testing.T) {
	ctx := context.Background()
	store := sender.NewMemoryGovernanceStore()
	governance := &sender.GovernanceService{Store: store}
	pool, err := governance.CreatePool(ctx, sender.Pool{
		Name: "pairing-http-pool", Status: "ACTIVE", MaxMessagesPerMinute: 10, DailyCapacity: 100,
	}, "operator", "create pairing test pool")
	if err != nil {
		t.Fatal(err)
	}

	gateway := &pairingAuthorizationGateway{}

	node, err := governance.RegisterNode(ctx, sender.Node{
		Name: "pairing-http-node", Status: "READY", InternalURL: "https://gateway.internal", GatewayPoolID: "gateway-pool",
		Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "1", Capacity: 1,
	}, "operator", "register pairing test node")
	if err != nil {
		t.Fatal(err)
	}
	session, err := governance.RegisterSession(ctx, sender.GovernedSession{
		ID: "session-pairing", NodeID: node.ID, PoolID: pool.ID, GatewayPoolID: "gateway-pool",
		MaskedMSISDN: "+234 ***", OwnerReference: "operations", RegistrationCountryISO2: "NG",
		ProfileDisplayName: "Pairing sender", RecoveryReference: "vault://pairing/recovery",
		EngineType: "BAILEYS", Status: sender.StatusNew, SafeMessagesPerMinute: 1, SafeDailyCapacity: 10, InFlightLimit: 1,
	}, []byte("cipher"), "operator", "register pairing sender")
	if err != nil {
		t.Fatal(err)
	}
	session, err = governance.TransitionSession(ctx, session.ID, session.Version, sender.StatusPairing, "operator", "move sender into pairing state")
	if err != nil {
		t.Fatal(err)
	}

	lifecycle := &sender.SessionLifecycleService{Governance: governance, Gateway: gateway}
	now := time.Now().UTC()
	principal := identity.Principal{
		User:    identity.User{ID: "11111111-1111-4111-8111-111111111111", Permissions: map[string]struct{}{"sender.admin": {}}},
		Session: identity.Session{MFAVerifiedAt: &now},
	}
	server := &Server{deps: Dependencies{SenderSessionLifecycle: lifecycle}}

	stale := httptest.NewRequest(http.MethodPost, "/api/v1/sender-sessions/"+session.ID+"/pairing/code",
		strings.NewReader(`{"expectedVersion":1,"phoneNumber":"+2348012345678","reason":"approved pairing operation"}`))
	stale.SetPathValue("id", session.ID)
	stale.Header.Set("Content-Type", "application/json")
	stale = stale.WithContext(identity.WithPrincipal(stale.Context(), principal))
	staleResponse := httptest.NewRecorder()
	server.getGatewaySenderSessionPairingCode(staleResponse, stale)
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf("stale status=%d body=%s", staleResponse.Code, staleResponse.Body.String())
	}
	if gateway.calls != 0 {
		t.Fatalf("stale request reached gateway: calls=%d", gateway.calls)
	}

	valid := httptest.NewRequest(http.MethodPost, "/api/v1/sender-sessions/"+session.ID+"/pairing/code",
		strings.NewReader(`{"expectedVersion":2,"phoneNumber":"+2348012345678","reason":"approved pairing operation"}`))
	valid.SetPathValue("id", session.ID)
	valid.Header.Set("Content-Type", "application/json")
	valid = valid.WithContext(identity.WithPrincipal(valid.Context(), principal))
	validResponse := httptest.NewRecorder()
	server.getGatewaySenderSessionPairingCode(validResponse, valid)
	if validResponse.Code != http.StatusOK {
		t.Fatalf("valid status=%d body=%s", validResponse.Code, validResponse.Body.String())
	}
	if gateway.calls != 1 || !strings.Contains(validResponse.Body.String(), "ABCD-EFGH") {
		t.Fatalf("pairing gateway response not preserved: calls=%d body=%s", gateway.calls, validResponse.Body.String())
	}
	current, err := store.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 3 || current.Status != sender.StatusPairing {
		t.Fatalf("pairing governance reservation incorrect: %+v", current)
	}
}

type pairingAuthorizationGateway struct {
	calls int
}

func (g *pairingAuthorizationGateway) Create(context.Context, sender.Node, sender.GovernedSession) (sender.SessionGatewayResult, error) {
	return sender.SessionGatewayResult{}, nil
}
func (g *pairingAuthorizationGateway) Start(context.Context, sender.Node, sender.GovernedSession, *sender.SessionProxyConfiguration, *sender.SessionTransportRuntimeConfiguration) (sender.SessionGatewayResult, error) {
	return sender.SessionGatewayResult{}, nil
}
func (g *pairingAuthorizationGateway) Stop(context.Context, sender.Node, sender.GovernedSession) (sender.SessionGatewayResult, error) {
	return sender.SessionGatewayResult{}, nil
}
func (g *pairingAuthorizationGateway) Logout(context.Context, sender.Node, sender.GovernedSession) (sender.SessionGatewayResult, error) {
	return sender.SessionGatewayResult{}, nil
}
func (g *pairingAuthorizationGateway) Delete(context.Context, sender.Node, sender.GovernedSession) error {
	return nil
}
func (g *pairingAuthorizationGateway) QR(context.Context, sender.Node, sender.GovernedSession) (sender.SessionGatewayResult, error) {
	return sender.SessionGatewayResult{}, nil
}
func (g *pairingAuthorizationGateway) PairingCode(_ context.Context, _ sender.Node, session sender.GovernedSession, _ string) (sender.SessionGatewayResult, error) {
	g.calls++
	return sender.SessionGatewayResult{
		SessionID: session.ID,
		Status:    "authenticating",
		Payload:   map[string]any{"sessionId": session.ID, "pairingCode": "ABCD-EFGH", "status": "authenticating"},
	}, nil
}
func (g *pairingAuthorizationGateway) Drain(context.Context, sender.Node, sender.GovernedSession) error {
	return nil
}
func (g *pairingAuthorizationGateway) Resume(context.Context, sender.Node, sender.GovernedSession) error {
	return nil
}
func (g *pairingAuthorizationGateway) Health(context.Context, sender.Node, sender.GovernedSession) (sender.SessionGatewayResult, error) {
	return sender.SessionGatewayResult{}, nil
}
