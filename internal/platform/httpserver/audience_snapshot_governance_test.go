package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/campaign"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/segment"
)

func TestLegacyAudienceSnapshotRouteDoesNotTrustCallerSuppliedMembers(t *testing.T) {
	ctx := context.Background()
	registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	campaigns := campaign.NewService(campaign.NewMemoryRepository())
	entity, err := campaigns.Create(ctx, campaign.CreateInput{
		OrganisationID: "org-1", Name: "governed snapshot", PurposeID: "purpose-1", ConsentReviewID: "review-1",
		MaximumUniqueRecipients: 10, MaximumMessagesPerRecipient: 1, CreatedBy: "maker",
		Transport: campaign.TransportSelection{Channel: "WHATSAPP", Provider: campaign.ProviderOpenWA, Engine: campaign.EngineBaileys,
			RoutingMode: campaign.RoutingSenderPool, GatewayPoolID: "gw-baileys", SenderPoolID: "11111111-1111-1111-1111-111111111111",
			AdapterVersion: "0.13.0", FallbackMode: campaign.FallbackNone, RoutingPolicyVersion: "route-v1", CapacityEvidenceVersion: "cap-v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []campaign.Action{campaign.ActionSubmitConsentReview, campaign.ActionApproveConsent, campaign.ActionStartAudienceBuild} {
		entity, err = campaigns.Transition(ctx, entity.ID, campaign.TransitionInput{Action: action, ActorID: "operator", ExpectedVersion: entity.Version})
		if err != nil {
			t.Fatalf("transition %s: %v", action, err)
		}
	}

	hash, err := identity.HashPassword("governed-snapshot-test-password")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{ID: "operator", Email: "snapshot@example.test", DisplayName: "Snapshot Operator", Status: identity.StatusActive,
		PasswordHash: hash, Permissions: map[string]struct{}{"audience.write": {}}}
	identities := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := identities.Login(ctx, user.Email, "governed-snapshot-test-password")
	if err != nil {
		t.Fatal(err)
	}
	definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{
		DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"NG"},
	}}}
	payload, err := json.Marshal(segment.CreateInput{
		Definition: definition, DefinitionVersion: 1, ConsentPolicyVersion: "consent-v1", ConfigurationVersion: "config-v1",
		Members: []segment.Member{{ContactID: "00000000-0000-4000-8000-000000000999", EligibilityEvidenceHash: "forged-evidence"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cohorts := cohort.NewExecutionService(cohort.NewCompiler(registry), &cohort.MemoryQueryRepository{})
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Registry: registry, Campaigns: campaigns, Cohorts: cohorts, Snapshots: segment.NewService(segment.NewMemoryRepository()), Identity: identities,
	}).Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/"+entity.ID+"/audience-snapshots", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+login.SessionToken)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("caller-supplied snapshot membership was trusted: status=%d body=%s", res.Code, res.Body.String())
	}
}
