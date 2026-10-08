package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"campaign-platform/internal/campaign"
)

func draftWire() map[string]any {
	return map[string]any{
		"expectedVersion": 1, "reason": "Correct campaign draft", "name": "Edited campaign",
		"organisationId":          "20000000-0000-4000-8000-000000000001",
		"purposeId":               "30000000-0000-4000-8000-000000000001",
		"consentReviewId":         "40000000-0000-4000-8000-000000000001",
		"maximumUniqueRecipients": 100, "maximumMessagesPerRecipient": 1, "timezone": "UTC",
		"transport": map[string]any{"channel": "WHATSAPP", "provider": "OPENWA", "engine": "BAILEYS", "routingMode": "SENDER_POOL",
			"gatewayPoolId": "50000000-0000-4000-8000-000000000001", "senderPoolId": "60000000-0000-4000-8000-000000000001",
			"adapterVersion": "v1", "routingPolicyVersion": "r1", "capacityEvidenceVersion": "c1", "fallbackMode": "NONE", "requiredCapabilities": []string{}},
	}
}
func draftRequest(handler http.Handler, id, token, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPut, "/api/v1/campaigns/"+id+"/draft", strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func TestCampaignDraftContract(t *testing.T) {
	cases := []struct {
		name, permission, id, token, code string
		status                            int
		mutate                            func(map[string]any)
		raw                               string
	}{
		{name: "auth", id: "bad", token: "none", status: 401, code: "AUTHENTICATION_REQUIRED"},
		{name: "permission", id: campaignDetailID, status: 403, code: "PERMISSION_DENIED"},
		{name: "path", permission: "campaign.write", id: "bad", status: 400, code: "INVALID_CAMPAIGN_ID"},
		{name: "unknown actor", permission: "campaign.write", id: campaignDetailID, status: 400, code: "INVALID_JSON", mutate: func(v map[string]any) { v["actorId"] = "forged" }},
		{name: "unknown status", permission: "campaign.write", id: campaignDetailID, status: 400, code: "INVALID_JSON", mutate: func(v map[string]any) { v["status"] = "SCHEDULED" }},
		{name: "authority binding", permission: "campaign.write", id: campaignDetailID, status: 400, code: "INVALID_JSON", mutate: func(v map[string]any) { v["transport"].(map[string]any)["providerDefinitionId"] = "forged" }},
		{name: "fractional count", permission: "campaign.write", id: campaignDetailID, status: 400, code: "INVALID_JSON", mutate: func(v map[string]any) { v["maximumUniqueRecipients"] = 1.5 }},
		{name: "date", permission: "campaign.write", id: campaignDetailID, status: 400, code: "INVALID_JSON", mutate: func(v map[string]any) { v["requestedStartAt"] = "yesterday" }},
		{name: "unsafe count", permission: "campaign.write", id: campaignDetailID, status: 422, code: "CAMPAIGN_DRAFT_INVALID", mutate: func(v map[string]any) { v["maximumUniqueRecipients"] = int64(9007199254740992) }},
		{name: "required version", permission: "campaign.write", id: campaignDetailID, status: 422, code: "CAMPAIGN_DRAFT_INVALID", mutate: func(v map[string]any) { delete(v, "expectedVersion") }},
		{name: "required timezone", permission: "campaign.write", id: campaignDetailID, status: 422, code: "CAMPAIGN_DRAFT_INVALID", mutate: func(v map[string]any) { delete(v, "timezone") }},
		{name: "json trailing", permission: "campaign.write", id: campaignDetailID, status: 400, code: "INVALID_JSON", raw: "{} {}"},
		{name: "quiet null", permission: "campaign.write", id: campaignDetailID, status: 400, code: "INVALID_JSON", mutate: func(v map[string]any) { v["quietHoursStart"] = nil }},
		{name: "nil governance", permission: "campaign.write", id: campaignDetailID, status: 503, code: "CAMPAIGN_DRAFT_GOVERNANCE_UNAVAILABLE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := campaign.NewMemoryRepository()
			entity := detailFixture()
			entity.Status = campaign.StatusDraft
			entity.Version = 1
			entity.AudienceSnapshotID = ""
			entity.AudienceSnapshotHash = ""
			entity.MessageVersionID = ""
			entity.MessageContentHash = ""
			entity.FinalApprovedBy = ""
			entity.CommercialApprovalID = ""
			entity.EligibleAudienceCount = 0
			wire := draftWire()
			entity.PurposeID = wire["purposeId"].(string)
			entity.ConsentReviewID = wire["consentReviewId"].(string)
			if err := repo.Create(context.Background(), entity); err != nil {
				t.Fatal(err)
			}
			handler, token := campaignDetailHandler(t, campaign.NewService(repo), tc.permission)
			if tc.token == "none" {
				token = ""
			}
			if tc.mutate != nil {
				tc.mutate(wire)
			}
			raw, _ := json.Marshal(wire)
			body := string(raw)
			if tc.raw != "" {
				body = tc.raw
			}
			response := draftRequest(handler, tc.id, token, body)
			if response.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.status, response.Body.String())
			}
			var failure struct {
				Code string `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
				t.Fatal(err)
			}
			if failure.Code != tc.code {
				t.Fatalf("code=%s want=%s", failure.Code, tc.code)
			}
			current, _ := repo.Get(context.Background(), entity.ID)
			events, _ := repo.ListMaterialChanges(context.Background(), entity.ID)
			if current.Version != 1 || current.Name != entity.Name || len(events) != 0 {
				t.Fatal("rejected request mutated campaign")
			}
		})
	}
}

func (r *campaignMaterialPaginationRepository) SaveDraft(context.Context, campaign.Campaign, campaign.MaterialChangeEvent, int64) (campaign.Campaign, error) {
	return campaign.Campaign{}, nil
}
