package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/identity"
)

func TestGenericCampaignTransitionRejectsExecutionLifecycleActions(t *testing.T) {
	repository := campaign.NewMemoryRepository()
	now := time.Now().UTC()
	initial := campaign.Campaign{
		ID: "campaign-transition-boundary", Status: campaign.StatusDraft,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.Create(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	campaigns := campaign.NewService(repository)
	hash, err := identity.HashPassword("a-very-long-development-password")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{
		ID: "campaign-operator", Email: "operator@example.test",
		DisplayName: "Campaign Operator", Status: identity.StatusActive,
		PasswordHash: hash, Permissions: map[string]struct{}{"*": {}},
	}
	identities := identity.NewService(
		identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour,
	)
	login, err := identities.Login(
		context.Background(), user.Email, "a-very-long-development-password",
	)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Campaigns: campaigns, Identity: identities,
	}).Handler()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/campaigns/"+initial.ID+"/transition",
		strings.NewReader(`{"action":"CANCEL","reason":"operator cancellation","expectedVersion":1}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+login.SessionToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("generic execution transition status=%d body=%s", response.Code, response.Body.String())
	}
	stored, err := repository.Get(context.Background(), initial.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != campaign.StatusDraft || stored.Version != initial.Version {
		t.Fatalf("generic route mutated campaign lifecycle: %+v", stored)
	}
}

func TestGenericCampaignTransitionOpenAPIExcludesExecutionLifecycleActions(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/openapi/control-api.yaml")
	if err != nil {
		t.Fatal(err)
	}
	document := string(raw)
	start := strings.Index(document, "    CampaignTransition:\n")
	if start < 0 {
		t.Fatal("CampaignTransition schema is missing")
	}
	schema := document[start:]
	if !strings.Contains(schema, "required: [action, expectedVersion]") {
		t.Fatal("CampaignTransition must require the optimistic expectedVersion")
	}
	for _, forbidden := range []string{
		"actorId:", "START_DISPATCH", "PAUSE", "RESUME", "COMPLETE", "CANCEL",
	} {
		if strings.Contains(schema, forbidden) {
			t.Fatalf("CampaignTransition exposes server-derived or execution-only field %q", forbidden)
		}
	}
}
