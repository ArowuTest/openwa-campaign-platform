package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/organisation"
)

const campaignDetailID = "10000000-0000-4000-8000-000000000001"

type campaignDetailRepository struct {
	*campaign.MemoryRepository
	getCalls, listCalls int
	failure             error
}

func (r *campaignDetailRepository) Get(ctx context.Context, id string) (campaign.Campaign, error) {
	r.getCalls++
	if r.failure != nil {
		return campaign.Campaign{}, r.failure
	}
	return r.MemoryRepository.Get(ctx, id)
}
func (r *campaignDetailRepository) ListPage(context.Context, int, *time.Time, string) ([]campaign.Campaign, error) {
	r.listCalls++
	return nil, errors.New("inventory must not be used for detail")
}

func campaignDetailHandler(t *testing.T, svc *campaign.Service, permission string) (http.Handler, string) {
	t.Helper()
	hash, err := identity.HashPassword("detail-test-long-password")
	if err != nil {
		t.Fatal(err)
	}
	permissions := map[string]struct{}{}
	if permission != "" {
		permissions[permission] = struct{}{}
	}
	user := identity.User{ID: "platform-operator", Email: "detail@example.test", Status: identity.StatusActive, PasswordHash: hash, Permissions: permissions}
	auth := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := auth.Login(context.Background(), user.Email, "detail-test-long-password")
	if err != nil {
		t.Fatal(err)
	}
	return New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{Campaigns: svc, Identity: auth}).Handler(), login.SessionToken
}
func campaignDetailRequest(handler http.Handler, id, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/campaigns/"+id, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func detailFixture() campaign.Campaign {
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	start, deadline := now.Add(time.Hour), now.Add(2*time.Hour)
	return campaign.Campaign{
		ID: campaignDetailID, OrganisationID: "20000000-0000-4000-8000-000000000001",
		Name: "Historical campaign beyond page 20", PurposeID: "purpose", ConsentReviewID: "consent",
		Status: campaign.StatusPaused, RequestedStartAt: &start, CompletionDeadlineAt: &deadline, Timezone: "Africa/Lagos",
		QuietHoursStart: "22:00", QuietHoursEnd: "06:00", MaximumUniqueRecipients: 10000, MaximumMessagesPerRecipient: 1,
		AudienceSnapshotID: "snapshot", AudienceSnapshotHash: "audience-hash", EligibleAudienceCount: 2500,
		MessageVersionID: "message", MessageContentHash: "message-hash", SenderPool: "saved-pool",
		Transport: campaign.TransportSelection{Channel: "WHATSAPP", Provider: campaign.ProviderOpenWA, Engine: campaign.EngineBaileys,
			RoutingMode: campaign.RoutingSenderPool, GatewayPoolID: "retired-gateway", GatewayPoolVersion: 5, SessionID: "saved-session",
			SenderPoolID: "saved-pool", AdapterVersion: "1.2.3", ProviderDefinitionID: "retired-provider", ProviderDefinitionVersion: 9,
			RequiredCapabilities: []string{"TEXT"}, FallbackMode: campaign.FallbackNone, RoutingPolicyVersion: "policy-v1", CapacityEvidenceVersion: "capacity-v2"},
		CreatedBy: "creator", FinalApprovedBy: "checker", CommercialApprovalID: "approval", PauseReason: "operator pause",
		CreatedAt: now, UpdatedAt: now.Add(time.Minute), Version: 42,
	}
}
func TestCampaignDetailDirectLookup(t *testing.T) {
	repo := &campaignDetailRepository{MemoryRepository: campaign.NewMemoryRepository()}
	target := detailFixture()
	if err := repo.Create(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2001; n++ {
		entity := target
		entity.ID = fmt.Sprintf("30000000-0000-4000-8000-%012d", n)
		entity.CreatedAt = target.CreatedAt.Add(time.Duration(n+1) * time.Second)
		if err := repo.Create(context.Background(), entity); err != nil {
			t.Fatal(err)
		}
	}
	orgs := organisation.NewMemoryRepository()
	if err := orgs.Create(context.Background(), organisation.Organisation{ID: target.OrganisationID, LegalName: "Suspended historical client", Status: organisation.StatusSuspended, PrimaryContactName: "PRIVATE_CONTACT", PrimaryContactEmail: "private@example.test", InternalNotes: "PRIVATE_ORG_NOTES"}); err != nil {
		t.Fatal(err)
	}
	service := campaign.NewService(repo).WithOrganisationReader(organisation.NewService(orgs))
	handler, token := campaignDetailHandler(t, service, "campaign.read")
	response := campaignDetailRequest(handler, campaignDetailID, token)
	if response.Code != 200 {
		t.Fatalf("direct detail status=%d body=%s", response.Code, response.Body.String())
	}
	if repo.getCalls != 1 || repo.listCalls != 0 {
		t.Fatalf("Get=%d ListPage=%d", repo.getCalls, repo.listCalls)
	}
	var got campaign.Campaign
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, target) {
		t.Fatalf("saved aggregate changed: got=%+v want=%+v", got, target)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("detail may be cached")
	}
}

func TestCampaignDetailContract(t *testing.T) {
	cases := []struct {
		name, id, permission, token, code string
		status, lookups                   int
		unavailable                       bool
		failure                           error
	}{
		{name: "no session before invalid path", id: "invalid", permission: "campaign.read", token: "none", status: 401, code: "AUTHENTICATION_REQUIRED"},
		{name: "invalid session", id: campaignDetailID, permission: "campaign.read", token: "invalid", status: 401, code: "SESSION_INVALID"},
		{name: "platform permission denied", id: campaignDetailID, token: "valid", status: 403, code: "PERMISSION_DENIED"},
		{name: "invalid UUID", id: "not-a-uuid", permission: "campaign.read", token: "valid", status: 400, code: "INVALID_CAMPAIGN_ID"},
		{name: "UUID compact", id: "10000000000040008000000000000001", permission: "campaign.read", token: "valid", status: 400, code: "INVALID_CAMPAIGN_ID"},
		{name: "UUID nonhex", id: "z0000000-0000-4000-8000-000000000001", permission: "campaign.read", token: "valid", status: 400, code: "INVALID_CAMPAIGN_ID"},
		{name: "missing", id: "10000000-0000-4000-8000-000000000099", permission: "campaign.read", token: "valid", status: 404, code: "CAMPAIGN_NOT_FOUND", lookups: 1},
		{name: "unavailable", id: campaignDetailID, permission: "campaign.read", token: "valid", status: 503, code: "CAMPAIGNS_UNAVAILABLE", unavailable: true},
		{name: "repository error masked", id: campaignDetailID, permission: "campaign.read", token: "valid", status: 500, code: "INTERNAL_ERROR", lookups: 1, failure: errors.New("SELECT * FROM campaign postgres://secret@host stack credential recovery private/object")},
		{name: "safe aggregate", id: campaignDetailID, permission: "campaign.read", token: "valid", status: 200, lookups: 1},
		{name: "historical Meta", id: campaignDetailID, permission: "campaign.read", token: "valid", status: 200, lookups: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &campaignDetailRepository{MemoryRepository: campaign.NewMemoryRepository(), failure: tc.failure}
			entity := detailFixture()
			if tc.name == "historical Meta" {
				entity.Transport.Provider = campaign.ProviderMeta
				entity.Transport.Engine = campaign.EngineMetaCloud
				entity.Transport.GatewayPoolID = ""
				entity.Transport.GatewayPoolVersion = 0
				entity.Transport.MetaSenderID = "retired-meta"
			}
			if err := repo.Create(context.Background(), entity); err != nil {
				t.Fatal(err)
			}
			service := campaign.NewService(repo)
			if tc.unavailable {
				service = nil
			}
			handler, token := campaignDetailHandler(t, service, tc.permission)
			if tc.token == "none" {
				token = ""
			}
			if tc.token == "invalid" {
				token = "bad-session"
			}
			response := campaignDetailRequest(handler, tc.id, token)
			if response.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.status, response.Body.String())
			}
			if repo.getCalls != tc.lookups || repo.listCalls != 0 {
				t.Fatalf("Get=%d want=%d ListPage=%d", repo.getCalls, tc.lookups, repo.listCalls)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("detail may be cached")
			}
			if tc.code != "" {
				var body struct {
					Code string `json:"error"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Code != tc.code {
					t.Fatalf("code=%s want=%s", body.Code, tc.code)
				}
			}
			for _, secret := range []string{"SELECT", "postgres://", "secret@host", "stack", "credential", "recovery", "private/object", "msisdn", "contacts", "qrCode", "notes", "financial"} {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatalf("unsafe detail contains %q", secret)
				}
			}
			if tc.status == 200 {
				var got campaign.Campaign
				if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, entity) {
					t.Fatal("aggregate/transport not preserved")
				}
			} else if strings.Contains(response.Body.String(), entity.Name) {
				t.Fatal("error exposes campaign body")
			}
		})
	}
}
