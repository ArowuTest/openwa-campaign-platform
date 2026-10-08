package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/organisation"
	"campaign-platform/internal/provider"
	"campaign-platform/internal/sender"
)

type httpDraftPurpose struct{ value consent.Purpose }

func (p httpDraftPurpose) Get(_ context.Context, id string) (consent.Purpose, error) {
	if p.value.ID != id {
		return consent.Purpose{}, consent.ErrPurposeNotFound
	}
	return p.value, nil
}

type httpDraftReview struct{ value consent.Review }

func (p httpDraftReview) Get(_ context.Context, id string) (consent.Review, error) {
	return p.value, nil
}

type httpDraftPool struct{ value sender.Pool }

func (p httpDraftPool) GetPool(_ context.Context, id string) (sender.Pool, error) {
	if p.value.ID != id {
		return sender.Pool{}, sender.ErrSenderNotFound
	}
	return p.value, nil
}

type httpDraftPolicy struct{}

func (httpDraftPolicy) ValidatePurpose(_ context.Context, org, purpose string) error {
	if org != "20000000-0000-4000-8000-000000000001" || purpose != "30000000-0000-4000-8000-000000000001" {
		return errors.New("policy denied")
	}
	return nil
}

type httpDraftGateway struct{}

func (httpDraftGateway) RequireCapabilities(_ context.Context, id string, p sender.GatewayProvider, e sender.GatewayEngine, caps []sender.Capability) (sender.GatewayPool, error) {
	if id != "50000000-0000-4000-8000-000000000001" || p != sender.GatewayProviderOpenWA || e != sender.GatewayEngineBaileys {
		return sender.GatewayPool{}, errors.New("gateway mismatch")
	}
	return sender.GatewayPool{ID: id, Provider: p, Engine: e, Status: sender.GatewayPoolActive, AdapterVersion: "v1", Version: 1}, nil
}

type httpDraftRepository struct {
	*campaign.MemoryRepository
	failure error
}

func (r *httpDraftRepository) SaveDraft(ctx context.Context, c campaign.Campaign, e campaign.MaterialChangeEvent, v int64) (campaign.Campaign, error) {
	if r.failure != nil {
		return campaign.Campaign{}, r.failure
	}
	return r.MemoryRepository.SaveDraft(ctx, c, e, v)
}
func httpDraftFixture(t *testing.T) (*campaign.Service, *httpDraftRepository, campaign.Campaign) {
	t.Helper()
	wire := draftWire()
	tr := wire["transport"].(map[string]any)
	now := time.Now().UTC()
	c := campaign.Campaign{ID: campaignDetailID, OrganisationID: wire["organisationId"].(string), PurposeID: wire["purposeId"].(string), ConsentReviewID: wire["consentReviewId"].(string), Name: "Saved draft", Status: campaign.StatusDraft, Timezone: "UTC",
		MaximumUniqueRecipients: 100, MaximumMessagesPerRecipient: 1, CreatedBy: "original-maker", CreatedAt: now, UpdatedAt: now, Version: 1,
		Transport: campaign.TransportSelection{Channel: "WHATSAPP", Provider: campaign.ProviderOpenWA, Engine: campaign.EngineBaileys, RoutingMode: campaign.RoutingSenderPool, GatewayPoolID: tr["gatewayPoolId"].(string), SenderPoolID: tr["senderPoolId"].(string), AdapterVersion: "v1", RoutingPolicyVersion: "r1", CapacityEvidenceVersion: "c1", FallbackMode: campaign.FallbackNone, RequiredCapabilities: []string{}, ProviderDefinitionID: "70000000-0000-4000-8000-000000000001", ProviderDefinitionVersion: 1, GatewayPoolVersion: 1}}
	c.SenderPool = c.Transport.SenderPoolID
	repo := &httpDraftRepository{MemoryRepository: campaign.NewMemoryRepository()}
	if err := repo.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	orgs := organisation.NewMemoryRepository()
	if err := orgs.Create(context.Background(), organisation.Organisation{ID: c.OrganisationID, Status: organisation.StatusActive}); err != nil {
		t.Fatal(err)
	}
	definitions := provider.NewMemoryStore()
	if _, err := definitions.Create(context.Background(), provider.Definition{ID: c.Transport.ProviderDefinitionID, Provider: "OPENWA", Engine: "BAILEYS", Channel: provider.ChannelWhatsApp, Status: provider.StatusActive, AdapterVersion: "v1", Version: 1, EffectiveFrom: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	svc := campaign.NewService(repo).WithOrganisationReader(organisation.NewService(orgs)).WithOrganisationPolicies(httpDraftPolicy{}).WithConsentPurposeReader(httpDraftPurpose{consent.Purpose{ID: c.PurposeID, OrganisationID: c.OrganisationID, ConsentReviewID: c.ConsentReviewID, Active: true, Channel: "WHATSAPP", WordingVersion: "w1"}}).WithConsentReviewReader(httpDraftReview{consent.Review{ID: c.ConsentReviewID, OrganisationID: c.OrganisationID, Channel: "WHATSAPP", WordingVersion: "w1", Scope: consent.ReviewScopeOrganisation, Status: consent.StatusPending}}).WithSenderPoolReader(httpDraftPool{sender.Pool{ID: c.Transport.SenderPoolID, Status: "ACTIVE"}}).WithProviderCapabilities(&provider.Service{Store: definitions}).WithGatewayPools(httpDraftGateway{})
	return svc, repo, c
}
func draftCookieHandler(t *testing.T, svc *campaign.Service) (http.Handler, string, string) {
	t.Helper()
	hash, err := identity.HashPassword("draft-test-long-password")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{ID: "authenticated-operator", Email: "draft@example.test", Status: identity.StatusActive, PasswordHash: hash, Permissions: map[string]struct{}{"campaign.write": {}}}
	auth := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := auth.Login(context.Background(), user.Email, "draft-test-long-password")
	if err != nil {
		t.Fatal(err)
	}
	return New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{Campaigns: svc, Identity: auth}).Handler(), login.SessionToken, login.CSRFToken
}
func TestCampaignDraftAuthenticatedActorAndCookieCSRF(t *testing.T) {
	svc, repo, c := httpDraftFixture(t)
	handler, token, csrf := draftCookieHandler(t, svc)
	body, _ := json.Marshal(draftWire())
	for _, value := range []string{"", "invalid", csrf} {
		request := httptest.NewRequest(http.MethodPut, "/api/v1/campaigns/"+c.ID+"/draft", strings.NewReader(string(body)))
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		if value != "" {
			request.Header.Set("X-CSRF-Token", value)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		expected := 403
		if value == csrf {
			expected = 200
		}
		if response.Code != expected {
			t.Fatalf("cookie PUT=%d want=%d %s", response.Code, expected, response.Body.String())
		}
	}
	got, _ := repo.Get(context.Background(), c.ID)
	events, _ := repo.ListMaterialChanges(context.Background(), c.ID)
	if got.CreatedBy != "original-maker" || got.Status != campaign.StatusDraft || got.Version != 2 || len(events) != 1 || events[0].ActorID != "authenticated-operator" {
		t.Fatalf("server actor/save=%+v events=%+v", got, events)
	}
}
func TestCampaignDraftErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"stale", campaign.ErrConflict, 409, "CAMPAIGN_VERSION_CONFLICT"}, {"locked state", campaign.ErrDraftNotEditable, 409, "CAMPAIGN_DRAFT_NOT_EDITABLE"},
		{"locked references", campaign.ErrDraftReferencesLocked, 409, "CAMPAIGN_DRAFT_REFERENCES_LOCKED"}, {"governance", campaign.ErrDraftGovernanceUnavailable, 503, "CAMPAIGN_DRAFT_GOVERNANCE_UNAVAILABLE"},
		{"invalid", campaign.ErrDraftInvalid, 422, "CAMPAIGN_DRAFT_INVALID"}, {"missing", campaign.ErrNotFound, 404, "CAMPAIGN_NOT_FOUND"},
		{"persistence", errors.New("SELECT postgres://secret@host private/object credential"), 500, "INTERNAL_ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, c := httpDraftFixture(t)
			repo.failure = tc.err
			handler, token := campaignDetailHandler(t, svc, "campaign.write")
			body, _ := json.Marshal(draftWire())
			response := draftRequest(handler, c.ID, token, string(body))
			if response.Code != tc.status {
				t.Fatalf("status=%d want=%d %s", response.Code, tc.status, response.Body.String())
			}
			var got struct {
				Code string `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Code != tc.code {
				t.Fatalf("error=%s", got.Code)
			}
			for _, secret := range []string{"SELECT", "secret@host", "postgres://", "private/object", "credential"} {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatal("unsafe error detail")
				}
			}
			current, _ := repo.Get(context.Background(), c.ID)
			events, _ := repo.ListMaterialChanges(context.Background(), c.ID)
			if current.Version != 1 || current.Name != c.Name || len(events) != 0 {
				t.Fatal("failed HTTP save mutated state")
			}
		})
	}
}
func TestCampaignDraftRejectsOversizedBody(t *testing.T) {
	svc, _, c := httpDraftFixture(t)
	handler, token := campaignDetailHandler(t, svc, "campaign.write")
	wire := draftWire()
	wire["reason"] = strings.Repeat("x", 256<<10)
	body, _ := json.Marshal(wire)
	response := draftRequest(handler, c.ID, token, string(body))
	if response.Code != 400 || !strings.Contains(response.Body.String(), "INVALID_JSON") {
		t.Fatalf("oversize=%d %s", response.Code, response.Body.String())
	}
}
