package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/sender"
	"campaign-platform/internal/shared/httpx"
)

func requireInventoryContinuation(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("expected inventory continuation: %+v body=%s", page, rr.Body.String())
	}
}

func TestInternalUsersReturnCursorContinuation(t *testing.T) {
	repo := identity.NewMemoryAdministrationRepository("ANALYST")
	base := time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC)
	for i, id := range []string{"user-a", "user-b", "user-c"} {
		a := identity.Account{ID: id, Email: id + "@example.com", DisplayName: id, Status: identity.StatusActive, MFARequired: true, RoleCodes: []string{"ANALYST"}, Version: 1, CreatedAt: base.Add(time.Duration(i) * time.Minute), UpdatedAt: base.Add(time.Duration(i) * time.Minute)}
		if err := repo.CreateAccount(context.Background(), a, "hash", "totp", "actor", "pagination seed"); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{IdentityAdministration: identity.NewAdministrationService(repo, nil)}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?limit=2", nil)
	rr := httptest.NewRecorder()
	server.listInternalUsers(rr, req)
	requireInventoryContinuation(t, rr)
}

func TestGatewayPoolsReturnCursorContinuation(t *testing.T) {
	store := sender.NewMemoryGovernanceStore()
	admin := &sender.GatewayPoolAdministration{Store: store}
	for _, name := range []string{"Gateway A", "Gateway B", "Gateway C"} {
		if _, err := admin.Create(context.Background(), sender.GatewayPool{Name: name, Provider: sender.GatewayProviderOpenWA, Engine: sender.GatewayEngineBaileys, AdapterVersion: "test", Capabilities: []sender.Capability{sender.CapabilitySendText}, MinimumHealthyNodes: 1}, "actor", "pagination seed"); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{GatewayPools: admin}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-pools?limit=2", nil)
	rr := httptest.NewRecorder()
	server.listGatewayPools(rr, req)
	requireInventoryContinuation(t, rr)
}

func TestSenderPoolsReturnCursorContinuation(t *testing.T) {
	store := sender.NewMemoryGovernanceStore()
	gov := &sender.GovernanceService{Store: store}
	for _, name := range []string{"Pool A", "Pool B", "Pool C"} {
		if _, err := gov.CreatePool(context.Background(), sender.Pool{Name: name, Status: "ACTIVE", MaxMessagesPerMinute: 10, DailyCapacity: 1000}, "actor", "pagination seed"); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{SenderGovernance: gov}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sender-pools?limit=2", nil)
	rr := httptest.NewRecorder()
	server.listSenderPools(rr, req)
	requireInventoryContinuation(t, rr)
}

func TestSenderNodesReturnCursorContinuation(t *testing.T) {
	store := sender.NewMemoryGovernanceStore()
	gov := &sender.GovernanceService{Store: store}
	for _, name := range []string{"Node A", "Node B", "Node C"} {
		if _, err := gov.RegisterNode(context.Background(), sender.Node{Name: name, Status: "OFFLINE"}, "actor", "pagination seed"); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{SenderGovernance: gov}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sender-nodes?limit=2", nil)
	rr := httptest.NewRecorder()
	server.listSenderNodes(rr, req)
	requireInventoryContinuation(t, rr)
}

func TestSenderPacingPoliciesReturnCursorContinuation(t *testing.T) {
	store := sender.NewMemoryPacingStore()
	admin := &sender.PacingAdministration{Store: store}
	base := time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC)
	for i, id := range []string{"pace-a", "pace-b", "pace-c"} {
		p := sender.PacingPolicy{ID: id, Scope: sender.PacingPlatform, MinimumDelayMS: 0, MaximumDelayMS: 1000, JitterMode: sender.JitterNone, MaxInFlight: 1, MessagesPerMinute: 10, HourlyAllowance: 100, DailyAllowance: 1000, MaxActiveCampaigns: 1, BurstSize: 1, Status: sender.PacingDraft, Version: 1, CreatedAt: base.Add(time.Duration(i) * time.Minute), UpdatedAt: base.Add(time.Duration(i) * time.Minute)}
		if _, err := store.Create(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{PacingPolicies: admin}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sender-pacing-policies?limit=2", nil)
	rr := httptest.NewRecorder()
	server.listSenderPacingPolicies(rr, req)
	requireInventoryContinuation(t, rr)
}
