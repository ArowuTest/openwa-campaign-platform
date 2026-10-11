package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/commercial"
	"campaign-platform/internal/execution"
	"campaign-platform/internal/identity"
)

func commercialHTTPFixture(t *testing.T) (*Server, *commercial.Service, campaign.Campaign, identity.Principal) {
	t.Helper()
	ctx := context.Background()
	campaignRepo := campaign.NewMemoryRepository()
	entity := campaign.Campaign{
		ID:              "10000000-0000-4000-8000-000000000001",
		OrganisationID:  "20000000-0000-4000-8000-000000000001",
		PurposeID:       "30000000-0000-4000-8000-000000000001",
		ConsentReviewID: "40000000-0000-4000-8000-000000000001",
		Name:            "Commercial workflow", Status: campaign.StatusMessageApproved,
		MaximumUniqueRecipients: 100, MaximumMessagesPerRecipient: 1,
		CreatedBy: "50000000-0000-4000-8000-000000000001",
		CreatedAt: time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC),
		Version:   7,
		Transport: campaign.TransportSelection{
			Channel: "WHATSAPP", Provider: campaign.ProviderOpenWA, Engine: campaign.EngineBaileys,
			RoutingMode:    campaign.RoutingSenderPool,
			GatewayPoolID:  "70000000-0000-4000-8000-000000000001",
			SenderPoolID:   "80000000-0000-4000-8000-000000000001",
			AdapterVersion: "1", FallbackMode: campaign.FallbackNone,
			RoutingPolicyVersion: "route-v1", CapacityEvidenceVersion: "capacity-v1",
		},
	}
	if err := campaignRepo.Create(ctx, entity); err != nil {
		t.Fatal(err)
	}
	commercialService := &commercial.Service{
		Store: commercial.NewMemoryStore(),
		Clock: func() time.Time { return time.Date(2026, 10, 11, 8, 30, 0, 0, time.UTC) },
	}
	principal := identity.Principal{User: identity.User{
		ID: "60000000-0000-4000-8000-000000000001",
		Permissions: map[string]struct{}{
			"finance.read": {}, "finance.write": {}, "finance.approve": {}, "campaign.approve": {},
		},
	}}
	return &Server{deps: Dependencies{
		Campaigns:  campaign.NewService(campaignRepo),
		Commercial: commercialService,
	}}, commercialService, entity, principal
}

func TestCommercialCreateRejectsClientLifecycleAuthorityFields(t *testing.T) {
	server, _, entity, principal := commercialHTTPFixture(t)
	body := "{\"quotationReference\":\"Q-1\",\"invoiceReference\":\"I-1\",\"currency\":\"NGN\",\"approvedRecipients\":100,\"unitPriceMinor\":50,\"managementFeeMinor\":0,\"reason\":\"record offline terms\",\"status\":\"APPROVED\"}"
	request := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/"+entity.ID+"/commercial-record", strings.NewReader(body))
	request.SetPathValue("id", entity.ID)
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(identity.WithPrincipal(request.Context(), principal))
	response := httptest.NewRecorder()
	server.createCommercialRecord(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "INVALID_JSON") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCommercialCampaignReadCorrectionConflictAndEventHistory(t *testing.T) {
	server, service, entity, principal := commercialHTTPFixture(t)
	paid := time.Date(2026, 10, 11, 7, 45, 0, 0, time.UTC)
	record, err := service.CreateDraft(context.Background(), commercial.Record{
		CampaignID: entity.ID, OrganisationID: entity.OrganisationID,
		QuotationReference: "Q-OLD", InvoiceReference: "I-OLD", Currency: "NGN",
		ApprovedRecipients: 100, UnitPriceMinor: 50, ManagementFeeMinor: 1000, TotalAmountMinor: 6000,
		PaymentReference: "PAY-OLD", PaymentReceivedAt: &paid,
	}, principal.User.ID, "initial commercial evidence")
	if err != nil {
		t.Fatal(err)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/v1/campaigns/"+entity.ID+"/commercial-record", nil)
	get.SetPathValue("id", entity.ID)
	getResponse := httptest.NewRecorder()
	server.getCampaignCommercialRecord(getResponse, get)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), "Q-OLD") {
		t.Fatalf("get status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}

	updatePayload := map[string]any{
		"expectedVersion":    record.Version,
		"quotationReference": "Q-NEW", "invoiceReference": "I-NEW", "currency": "NGN",
		"approvedRecipients": int64(120), "unitPriceMinor": int64(60),
		"managementFeeMinor": int64(1200),
		"paymentReference":   "PAY-NEW", "paymentReceivedAt": paid.Format(time.RFC3339),
		"reason": "correct rejected commercial evidence",
	}
	raw, _ := json.Marshal(updatePayload)
	update := httptest.NewRequest(http.MethodPut, "/api/v1/commercial-records/"+record.ID+"/draft", strings.NewReader(string(raw)))
	update.SetPathValue("id", record.ID)
	update.Header.Set("Content-Type", "application/json")
	update = update.WithContext(identity.WithPrincipal(update.Context(), principal))
	updateResponse := httptest.NewRecorder()
	server.updateCommercialRecordDraft(updateResponse, update)
	if updateResponse.Code != http.StatusOK || !strings.Contains(updateResponse.Body.String(), "Q-NEW") {
		t.Fatalf("update status=%d body=%s", updateResponse.Code, updateResponse.Body.String())
	}

	stale := httptest.NewRequest(http.MethodPut, "/api/v1/commercial-records/"+record.ID+"/draft", strings.NewReader(string(raw)))
	stale.SetPathValue("id", record.ID)
	stale.Header.Set("Content-Type", "application/json")
	stale = stale.WithContext(identity.WithPrincipal(stale.Context(), principal))
	staleResponse := httptest.NewRecorder()
	server.updateCommercialRecordDraft(staleResponse, stale)
	if staleResponse.Code != http.StatusConflict || !strings.Contains(staleResponse.Body.String(), "COMMERCIAL_CONFLICT") {
		t.Fatalf("stale status=%d body=%s", staleResponse.Code, staleResponse.Body.String())
	}

	events := httptest.NewRequest(http.MethodGet, "/api/v1/commercial-records/"+record.ID+"/events", nil)
	events.SetPathValue("id", record.ID)
	eventsResponse := httptest.NewRecorder()
	server.listCommercialRecordEvents(eventsResponse, events)
	if eventsResponse.Code != http.StatusOK {
		t.Fatalf("events status=%d body=%s", eventsResponse.Code, eventsResponse.Body.String())
	}
	historyBody := eventsResponse.Body.String()
	if strings.Index(historyBody, "\"eventType\":\"CREATED\"") > strings.Index(historyBody, "\"eventType\":\"UPDATED\"") ||
		!strings.Contains(historyBody, "\"quotationReference\":\"Q-NEW\"") ||
		!strings.Contains(historyBody, "\"totalAmountMinor\":8400") {
		t.Fatalf("commercial history is incomplete or out of order: %s", historyBody)
	}
}

func TestCommercialCreateDerivesTotalServerSide(t *testing.T) {
	server, _, entity, principal := commercialHTTPFixture(t)
	body := `{"quotationReference":"Q-DERIVED","invoiceReference":"I-DERIVED","currency":"NGN","approvedRecipients":100,"unitPriceMinor":50,"managementFeeMinor":1000,"paymentReference":"PAY-DERIVED","reason":"record derived commercial evidence"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/"+entity.ID+"/commercial-record", strings.NewReader(body))
	request.SetPathValue("id", entity.ID)
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(identity.WithPrincipal(request.Context(), principal))
	response := httptest.NewRecorder()
	server.createCommercialRecord(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "\"totalAmountMinor\":6000") {
		t.Fatalf("server-derived commercial total missing: %s", response.Body.String())
	}
}

func TestCommercialCreateRequiresMessageApprovedCampaignStage(t *testing.T) {
	ctx := context.Background()
	repository := campaign.NewMemoryRepository()
	entity := campaign.Campaign{
		ID:              "11000000-0000-4000-8000-000000000001",
		OrganisationID:  "21000000-0000-4000-8000-000000000001",
		PurposeID:       "31000000-0000-4000-8000-000000000001",
		ConsentReviewID: "41000000-0000-4000-8000-000000000001",
		Name:            "Too early commercial", Status: campaign.StatusAudienceValidated,
		MaximumUniqueRecipients: 100, MaximumMessagesPerRecipient: 1,
		CreatedBy: "51000000-0000-4000-8000-000000000001",
		CreatedAt: time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC), Version: 3,
	}
	if err := repository.Create(ctx, entity); err != nil {
		t.Fatal(err)
	}
	server := &Server{deps: Dependencies{
		Campaigns:  campaign.NewService(repository),
		Commercial: &commercial.Service{Store: commercial.NewMemoryStore()},
	}}
	principal := identity.Principal{User: identity.User{
		ID:          "61000000-0000-4000-8000-000000000001",
		Permissions: map[string]struct{}{"finance.write": {}},
	}}
	body := `{"quotationReference":"Q-EARLY","invoiceReference":"I-EARLY","currency":"NGN","approvedRecipients":100,"unitPriceMinor":50,"managementFeeMinor":0,"reason":"attempt early commercial evidence"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/"+entity.ID+"/commercial-record", strings.NewReader(body))
	request.SetPathValue("id", entity.ID)
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(identity.WithPrincipal(request.Context(), principal))
	response := httptest.NewRecorder()
	server.createCommercialRecord(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "COMMERCIAL_STAGE_INVALID") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestApproveCommercialTransitionRequiresDayOnePilotEvidence(t *testing.T) {
	server, _, entity, principal := commercialHTTPFixture(t)
	server.deps.Execution = &execution.Coordinator{}
	body := `{"action":"APPROVE_COMMERCIAL","expectedVersion":7,"reason":"approve after finance review"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/"+entity.ID+"/transition", strings.NewReader(body))
	request.SetPathValue("id", entity.ID)
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(identity.WithPrincipal(request.Context(), principal))
	response := httptest.NewRecorder()
	server.transitionCampaign(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "PILOT_EVIDENCE_INCOMPLETE") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCommercialCreateRejectsUndocumentedExpectedVersion(t *testing.T) {
	server, _, entity, principal := commercialHTTPFixture(t)
	body := "{\"expectedVersion\":7,\"quotationReference\":\"Q-STRICT\",\"invoiceReference\":\"I-STRICT\",\"currency\":\"NGN\",\"approvedRecipients\":100,\"unitPriceMinor\":50,\"managementFeeMinor\":0,\"reason\":\"reject create version authority\"}"
	request := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/"+entity.ID+"/commercial-record", strings.NewReader(body))
	request.SetPathValue("id", entity.ID)
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(identity.WithPrincipal(request.Context(), principal))
	response := httptest.NewRecorder()
	server.createCommercialRecord(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "INVALID_JSON") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCommercialDraftCorrectionDoesNotLeakDomainErrorDetail(t *testing.T) {
	server, service, entity, principal := commercialHTTPFixture(t)
	draft, err := service.CreateDraft(context.Background(), commercial.Record{
		CampaignID: entity.ID, OrganisationID: entity.OrganisationID,
		QuotationReference: "Q-NOLEAK", InvoiceReference: "I-NOLEAK", Currency: "NGN",
		ApprovedRecipients: 100, UnitPriceMinor: 50, ManagementFeeMinor: 0, TotalAmountMinor: 5000,
	}, principal.User.ID, "create commercial leak test")
	if err != nil {
		t.Fatal(err)
	}
	body := "{\"expectedVersion\":1,\"quotationReference\":\"Q-NOLEAK\",\"invoiceReference\":\"I-NOLEAK\",\"currency\":\"N1\",\"approvedRecipients\":100,\"unitPriceMinor\":50,\"managementFeeMinor\":0,\"reason\":\"reject invalid currency without details\"}"
	request := httptest.NewRequest(http.MethodPut, "/api/v1/commercial-records/"+draft.ID, strings.NewReader(body))
	request.SetPathValue("id", draft.ID)
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(identity.WithPrincipal(request.Context(), principal))
	response := httptest.NewRecorder()
	server.updateCommercialRecordDraft(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "\"detail\"") || strings.Contains(response.Body.String(), commercial.ErrInvalid.Error()) {
		t.Fatalf("commercial domain error leaked to client: %s", response.Body.String())
	}
}
