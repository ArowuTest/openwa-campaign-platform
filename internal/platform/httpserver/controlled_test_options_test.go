package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/sender"
	"campaign-platform/internal/testmessage"
)

func TestCampaignControlledTestOptionsAreMaskedAndExactRouteScoped(t *testing.T) {
	ctx := context.Background()
	campaignRepo := campaign.NewMemoryRepository()
	entity := campaign.Campaign{
		ID: "campaign-options", Version: 7, MessageVersionID: "message-v1",
		Transport: campaign.TransportSelection{
			Channel: "WHATSAPP", Provider: campaign.ProviderOpenWA, Engine: campaign.EngineBaileys,
			RoutingMode: campaign.RoutingSenderPool, SenderPoolID: "pool-a", GatewayPoolID: "gateway-a",
			AdapterVersion: "1", RoutingPolicyVersion: "route-v1", CapacityEvidenceVersion: "capacity-v1",
			FallbackMode: campaign.FallbackNone,
		},
	}
	if err := campaignRepo.Create(ctx, entity); err != nil {
		t.Fatal(err)
	}
	campaigns := campaign.NewService(campaignRepo)

	recipientRepo := testmessage.NewMemoryRepository()
	now := time.Date(2026, 10, 9, 19, 0, 0, 0, time.UTC)
	for _, recipient := range []testmessage.Recipient{
		{ID: "recipient-active", Label: "Approved QA", MSISDNLookupHash: []byte("active-hash"), MaskedMSISDN: "+234 *** 1234", Status: testmessage.RecipientActive, Version: 3, CreatedAt: now, UpdatedAt: now},
		{ID: "recipient-draft", Label: "Draft QA", MSISDNLookupHash: []byte("draft-hash"), MaskedMSISDN: "+234 *** 9999", Status: testmessage.RecipientDraft, Version: 1, CreatedAt: now.Add(-time.Second), UpdatedAt: now},
	} {
		if _, err := recipientRepo.CreateRecipient(ctx, recipient); err != nil {
			t.Fatal(err)
		}
	}
	testMessages := &testmessage.Service{Repository: recipientRepo}

	senderStore := sender.NewMemoryGovernanceStore()
	compatible, err := senderStore.RegisterSession(ctx, sender.GovernedSession{
		PoolID: "pool-a", GatewayPoolID: "gateway-a", MaskedMSISDN: "+234 *** 5678",
		OwnerReference: "must-not-leak", RecoveryReference: "vault://must-not-leak",
		ProfileDisplayName: "QA sender", EngineType: "BAILEYS", Status: sender.StatusReady, Version: 1,
	}, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = senderStore.RegisterSession(ctx, sender.GovernedSession{
		PoolID: "pool-b", GatewayPoolID: "gateway-a", MaskedMSISDN: "+234 *** 2222",
		ProfileDisplayName: "Wrong pool", EngineType: "BAILEYS", Status: sender.StatusReady,
	}, nil, "", "")
	_, _ = senderStore.RegisterSession(ctx, sender.GovernedSession{
		PoolID: "pool-a", GatewayPoolID: "gateway-a", MaskedMSISDN: "+234 *** 3333",
		ProfileDisplayName: "Not ready", EngineType: "BAILEYS", Status: sender.StatusDisconnected,
	}, nil, "", "")
	_, _ = senderStore.RegisterSession(ctx, sender.GovernedSession{
		PoolID: "pool-a", GatewayPoolID: "gateway-a", MaskedMSISDN: "+234 *** 4444",
		ProfileDisplayName: "Wrong engine", EngineType: "WHATSAPP_WEB_JS", Status: sender.StatusReady,
	}, nil, "", "")
	senders := &sender.GovernanceService{Store: senderStore}

	server := &Server{deps: Dependencies{Campaigns: campaigns, TestMessages: testMessages, SenderGovernance: senders}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/campaigns/campaign-options/test-message-options", nil)
	request.SetPathValue("id", "campaign-options")
	response := httptest.NewRecorder()
	server.getCampaignTestMessageOptions(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body controlledTestOptionsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Supported || body.CampaignVersion != 7 || body.MessageVersionID != "message-v1" ||
		body.Provider != "OPENWA" || body.Engine != "BAILEYS" ||
		body.SenderPoolID != "pool-a" || body.GatewayPoolID != "gateway-a" {
		t.Fatalf("unexpected route context: %+v", body)
	}
	if len(body.Recipients) != 1 || body.Recipients[0].ID != "recipient-active" || body.Recipients[0].MaskedMSISDN != "+234 *** 1234" {
		t.Fatalf("unexpected recipient options: %+v", body.Recipients)
	}
	if len(body.Sessions) != 1 || body.Sessions[0].ID != compatible.ID || body.Sessions[0].MaskedMSISDN != "+234 *** 5678" {
		t.Fatalf("unexpected session options: %+v", body.Sessions)
	}
	if !body.RecipientInventoryComplete || !body.SessionInventoryComplete {
		t.Fatalf("small inventories should be complete: %+v", body)
	}
	for _, forbidden := range []string{"must-not-leak", "vault://", "recipient-draft", "Wrong pool", "Not ready", "Wrong engine"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("sensitive or incompatible data leaked (%s): %s", forbidden, response.Body.String())
		}
	}
}

func TestCampaignControlledTestOptionsFailClosedForNonOpenWARoute(t *testing.T) {
	ctx := context.Background()
	repo := campaign.NewMemoryRepository()
	entity := campaign.Campaign{
		ID: "campaign-meta", Version: 2,
		Transport: campaign.TransportSelection{
			Channel: "WHATSAPP", Provider: campaign.ProviderMeta, Engine: campaign.EngineMetaCloud,
			RoutingMode: campaign.RoutingSenderPool, SenderPoolID: "meta-pool", MetaSenderID: "meta-sender",
			AdapterVersion: "1", RoutingPolicyVersion: "route-v1", CapacityEvidenceVersion: "capacity-v1",
			FallbackMode: campaign.FallbackNone,
		},
	}
	if err := repo.Create(ctx, entity); err != nil {
		t.Fatal(err)
	}
	server := &Server{deps: Dependencies{
		Campaigns:        campaign.NewService(repo),
		TestMessages:     &testmessage.Service{Repository: testmessage.NewMemoryRepository()},
		SenderGovernance: &sender.GovernanceService{Store: sender.NewMemoryGovernanceStore()},
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/campaigns/campaign-meta/test-message-options", nil)
	request.SetPathValue("id", "campaign-meta")
	response := httptest.NewRecorder()
	server.getCampaignTestMessageOptions(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body controlledTestOptionsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Supported || body.Reason == "" || len(body.Recipients) != 0 || len(body.Sessions) != 0 {
		t.Fatalf("non-OpenWA route was not fail-closed: %+v", body)
	}
}

type controlledTestSessionPageStore struct {
	*sender.MemoryGovernanceStore
	sessions []sender.GovernedSession
}

func (s *controlledTestSessionPageStore) ListSessionPage(_ context.Context, limit int, afterMaskedMSISDN, afterID string) ([]sender.GovernedSession, error) {
	out := make([]sender.GovernedSession, 0, limit)
	for _, value := range s.sessions {
		if afterMaskedMSISDN != "" && (value.MaskedMSISDN < afterMaskedMSISDN || (value.MaskedMSISDN == afterMaskedMSISDN && value.ID <= afterID)) {
			continue
		}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func TestControlledTestOptionScansAreBoundedAndReportTruncation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 9, 19, 30, 0, 0, time.UTC)

	recipientRepo := testmessage.NewMemoryRepository()
	for i := 0; i < controlledTestOptionLimit+1; i++ {
		value := testmessage.Recipient{
			ID: fmt.Sprintf("recipient-%03d", i), Label: fmt.Sprintf("Recipient %03d", i),
			MSISDNLookupHash: []byte{byte(i + 1)}, MaskedMSISDN: fmt.Sprintf("+234 *** %04d", i),
			Status: testmessage.RecipientActive, Version: 1, CreatedAt: now.Add(-time.Duration(i) * time.Second), UpdatedAt: now,
		}
		if _, err := recipientRepo.CreateRecipient(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	recipients, recipientComplete, err := scanControlledTestRecipients(ctx, &testmessage.Service{Repository: recipientRepo})
	if err != nil {
		t.Fatal(err)
	}
	if len(recipients) != controlledTestOptionLimit || recipientComplete {
		t.Fatalf("recipient cap not enforced truthfully: count=%d complete=%v", len(recipients), recipientComplete)
	}

	senderStore := &controlledTestSessionPageStore{MemoryGovernanceStore: sender.NewMemoryGovernanceStore()}
	for i := 0; i < controlledTestOptionLimit+1; i++ {
		senderStore.sessions = append(senderStore.sessions, sender.GovernedSession{
			ID:     fmt.Sprintf("session-%03d", i),
			PoolID: "pool-a", GatewayPoolID: "gateway-a", MaskedMSISDN: fmt.Sprintf("+234 *** %04d", i),
			ProfileDisplayName: fmt.Sprintf("Sender %03d", i), EngineType: "BAILEYS", Status: sender.StatusReady, Version: 1,
		})
	}
	entity := campaign.Campaign{Transport: campaign.TransportSelection{
		Provider: campaign.ProviderOpenWA, Engine: campaign.EngineBaileys, SenderPoolID: "pool-a", GatewayPoolID: "gateway-a",
	}}
	sessions, sessionComplete, err := scanControlledTestSessions(ctx, &sender.GovernanceService{Store: senderStore}, entity)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != controlledTestOptionLimit || sessionComplete {
		t.Fatalf("session cap not enforced truthfully: count=%d complete=%v", len(sessions), sessionComplete)
	}
}
