package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/campaignpreparation"
	"campaign-platform/internal/execution"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/sender"
	"campaign-platform/internal/testmessage"
)

func capacityHoldFixture() (campaign.Campaign, campaignpreparation.Readiness, sender.ResolvedPacing) {
	start := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	required := 1.0
	entity := campaign.Campaign{
		ID:               "10000000-0000-4000-8000-000000000001",
		Status:           campaign.StatusMessageApproved,
		RequestedStartAt: &start, CompletionDeadlineAt: &end,
		MaximumUniqueRecipients: 150, EligibleAudienceCount: 120, Version: 7,
		MessageVersionID:   "50000000-0000-4000-8000-000000000001",
		MessageContentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Transport: campaign.TransportSelection{
			Channel: "WHATSAPP", Provider: campaign.ProviderOpenWA, Engine: campaign.EngineBaileys,
			RoutingMode:               campaign.RoutingSenderPool,
			GatewayPoolID:             "20000000-0000-4000-8000-000000000001",
			GatewayPoolVersion:        3,
			SenderPoolID:              "30000000-0000-4000-8000-000000000001",
			AdapterVersion:            "1",
			ProviderDefinitionID:      "60000000-0000-4000-8000-000000000001",
			ProviderDefinitionVersion: 4,
			FallbackMode:              campaign.FallbackNone,
			RoutingPolicyVersion:      "route-v7", CapacityEvidenceVersion: "capacity-v9",
		},
	}
	readiness := campaignpreparation.Readiness{
		CampaignID: entity.ID, CampaignVersion: entity.Version, CampaignStatus: entity.Status,
		AssessedAt: start.Add(-time.Minute),
		Checks:     []campaignpreparation.ReadinessCheck{{Key: "capacity", Status: "PASS", Code: "CAPACITY_GROSS_PROJECTION_VALID"}},
		Capacity: &campaignpreparation.CapacityPreview{
			SenderPoolID: entity.Transport.SenderPoolID, GatewayPoolID: entity.Transport.GatewayPoolID,
			HealthySessions: 3, HealthyNodes: 2, MinimumHealthyNodes: 1,
			AvailableMessagesPerMinute: 20, AvailableHourlyUnits: 1200, AvailableDailyUnits: 5000,
			SafetyMarginPercent: 15, BasisRecipientCount: 120, Basis: "SNAPSHOT",
			RequiredMessagesPerMinute: &required, MeasurementAsOf: start.Add(-time.Minute),
			Classification: "ADVISORY_ONLY", Reasons: []string{campaignpreparation.ReservationReason},
		},
	}
	pacing := sender.ResolvedPacing{Policy: sender.PacingPolicy{
		ID: "40000000-0000-4000-8000-000000000001", Scope: sender.PacingSenderPool,
		ScopeID: entity.Transport.SenderPoolID, Status: sender.PacingActive, Version: 4,
		MessagesPerMinute: 10, HourlyAllowance: 600, DailyAllowance: 2000,
	}, ResolvedAt: start.Add(-time.Minute)}
	return entity, readiness, pacing
}

func TestBuildCampaignCapacityHoldProposalDerivesAuthorityServerSide(t *testing.T) {
	entity, readiness, pacing := capacityHoldFixture()
	proposal, err := buildCampaignCapacityHoldProposal(entity, readiness, pacing)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.ReservedMessagesPerMinute != 2 {
		t.Fatalf("reserved MPM=%d want 2", proposal.ReservedMessagesPerMinute)
	}
	if proposal.ReservedHourlyUnits != 120 || proposal.ReservedDailyUnits != 120 {
		t.Fatalf("unexpected derived units hourly=%d daily=%d", proposal.ReservedHourlyUnits, proposal.ReservedDailyUnits)
	}
	if proposal.PacingPolicyID != pacing.Policy.ID || proposal.PacingPolicyVersion != pacing.Policy.Version {
		t.Fatalf("pacing evidence not frozen: %+v", proposal)
	}
	if proposal.RoutingPolicyVersion != entity.Transport.RoutingPolicyVersion ||
		proposal.CapacityEvidenceVersion != entity.Transport.CapacityEvidenceVersion {
		t.Fatalf("saved route evidence not preserved: %+v", proposal)
	}
}

func TestBuildCampaignCapacityHoldProposalFailsClosedWhenPacingCannotSupportDerivedHold(t *testing.T) {
	entity, readiness, pacing := capacityHoldFixture()
	pacing.Policy.MessagesPerMinute = 1
	if _, err := buildCampaignCapacityHoldProposal(entity, readiness, pacing); err == nil {
		t.Fatal("expected insufficient pacing policy to block capacity hold")
	}
	pacing.Policy.MessagesPerMinute = 10
	pacing.Policy.HourlyAllowance = 100
	if _, err := buildCampaignCapacityHoldProposal(entity, readiness, pacing); err == nil {
		t.Fatal("expected insufficient hourly allowance to block capacity hold")
	}
}

func TestBuildCampaignCapacityHoldProposalRejectsPreMessageAndNonOpenWAPilotStates(t *testing.T) {
	entity, readiness, pacing := capacityHoldFixture()
	entity.Status = campaign.StatusAudienceValidated
	readiness.CampaignStatus = entity.Status
	if _, err := buildCampaignCapacityHoldProposal(entity, readiness, pacing); err == nil {
		t.Fatal("pre-message campaign unexpectedly received a capacity hold proposal")
	}
	entity, readiness, pacing = capacityHoldFixture()
	entity.Transport.Provider = campaign.ProviderMeta
	entity.Transport.Engine = campaign.EngineMetaCloud
	entity.Transport.GatewayPoolID = ""
	entity.Transport.MetaSenderID = "50000000-0000-4000-8000-000000000001"
	if _, err := buildCampaignCapacityHoldProposal(entity, readiness, pacing); err == nil {
		t.Fatal("Meta campaign unexpectedly entered the OpenWA Day-1 pilot hold path")
	}
}

func acceptedCapacityHoldSend(entity campaign.Campaign) testmessage.Send {
	now := time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC)
	return testmessage.Send{
		ID:                        "70000000-0000-4000-8000-000000000001",
		CampaignID:                entity.ID,
		MessageVersionID:          entity.MessageVersionID,
		MessageContentHash:        entity.MessageContentHash,
		GatewayPoolID:             entity.Transport.GatewayPoolID,
		GatewayPoolVersion:        entity.Transport.GatewayPoolVersion,
		SenderPoolID:              entity.Transport.SenderPoolID,
		Provider:                  string(entity.Transport.Provider),
		Engine:                    string(entity.Transport.Engine),
		ProviderAdapterVersion:    entity.Transport.AdapterVersion,
		ProviderDefinitionID:      entity.Transport.ProviderDefinitionID,
		ProviderDefinitionVersion: entity.Transport.ProviderDefinitionVersion,
		Status:                    testmessage.SendAccepted,
		IdempotencyKey:            "capacity-hold-test-send-0001",
		CreatedAt:                 now,
		UpdatedAt:                 now,
	}
}

func TestAcceptedControlledTestForCapacityHoldRequiresExactCurrentMessageAndRouteEvidence(t *testing.T) {
	entity, _, _ := capacityHoldFixture()
	ctx := context.Background()

	assertAccepted := func(t *testing.T, send testmessage.Send, want bool) {
		t.Helper()
		repository := testmessage.NewMemoryRepository()
		if _, err := repository.CreateSend(ctx, send); err != nil {
			t.Fatal(err)
		}
		service := &testmessage.Service{Repository: repository}
		got, err := acceptedControlledTestForCapacityHold(ctx, service, entity)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("accepted=%v want=%v send=%+v", got, want, send)
		}
	}

	base := acceptedCapacityHoldSend(entity)
	assertAccepted(t, base, true)

	staleHash := base
	staleHash.ID = "70000000-0000-4000-8000-000000000002"
	staleHash.IdempotencyKey = "capacity-hold-test-send-0002"
	staleHash.MessageContentHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	assertAccepted(t, staleHash, false)

	staleGateway := base
	staleGateway.ID = "70000000-0000-4000-8000-000000000003"
	staleGateway.IdempotencyKey = "capacity-hold-test-send-0003"
	staleGateway.GatewayPoolVersion++
	assertAccepted(t, staleGateway, false)

	staleAdapter := base
	staleAdapter.ID = "70000000-0000-4000-8000-000000000004"
	staleAdapter.IdempotencyKey = "capacity-hold-test-send-0004"
	staleAdapter.ProviderAdapterVersion = "0.9.0"
	assertAccepted(t, staleAdapter, false)

	staleProviderDefinition := base
	staleProviderDefinition.ID = "70000000-0000-4000-8000-000000000005"
	staleProviderDefinition.IdempotencyKey = "capacity-hold-test-send-0005"
	staleProviderDefinition.ProviderDefinitionVersion++
	assertAccepted(t, staleProviderDefinition, false)
}

func TestFindCapacityHoldReplayConvergesExactRequestAndRejectsSemanticReuse(t *testing.T) {
	ctx := context.Background()
	store := execution.NewMemoryRoutingPlanStore()
	created, err := store.Create(ctx, execution.RoutingPlan{
		ID:                      "80000000-0000-4000-8000-000000000001",
		CampaignID:              "10000000-0000-4000-8000-000000000001",
		RoutingPolicyVersion:    "route-v7",
		CapacityEvidenceVersion: "capacity-v9",
		FallbackMode:            "NONE",
		ApprovedBy:              "90000000-0000-4000-8000-000000000001",
		ApprovalReason:          "approved exact-route capacity hold",
		ApprovedCampaignVersion: 7,
		IdempotencyKey:          "capacity-hold-client-request-0001",
		RequestHash:             "request-hash-1",
		Routes: []execution.PoolRoute{{
			SenderPoolID:  "30000000-0000-4000-8000-000000000001",
			GatewayPoolID: "20000000-0000-4000-8000-000000000001",
			Provider:      string(campaign.ProviderOpenWA),
			Engine:        string(campaign.EngineBaileys),
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1005; i++ {
		_, err := store.Create(ctx, execution.RoutingPlan{
			ID:             fmt.Sprintf("noise-plan-%04d", i),
			CampaignID:     created.CampaignID,
			IdempotencyKey: fmt.Sprintf("noise-request-%04d", i),
			RequestHash:    fmt.Sprintf("noise-hash-%04d", i),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	admin := &execution.RoutingAdministration{Store: store}

	replayed, ok, err := findCapacityHoldReplay(
		ctx, admin, created.CampaignID, created.IdempotencyKey, created.ApprovedBy, created.ApprovalReason,
		created.ApprovedCampaignVersion,
	)
	if err != nil || !ok || replayed.ID != created.ID {
		t.Fatalf("exact replay did not converge: plan=%+v ok=%v err=%v", replayed, ok, err)
	}

	if _, ok, err := findCapacityHoldReplay(ctx, admin, created.CampaignID, created.IdempotencyKey, created.ApprovedBy, "different approval reason", created.ApprovedCampaignVersion); !errors.Is(err, errCapacityHoldConflict) || ok {
		t.Fatalf("semantic request-id reuse was not rejected: ok=%v err=%v", ok, err)
	}
	if _, ok, err := findCapacityHoldReplay(ctx, admin, created.CampaignID, created.IdempotencyKey, "different-approver", created.ApprovalReason, created.ApprovedCampaignVersion); !errors.Is(err, errCapacityHoldConflict) || ok {
		t.Fatalf("cross-approver request-id reuse was not rejected: ok=%v err=%v", ok, err)
	}
	if _, ok, err := findCapacityHoldReplay(ctx, admin, created.CampaignID, created.IdempotencyKey, created.ApprovedBy, created.ApprovalReason, created.ApprovedCampaignVersion+1); !errors.Is(err, errCapacityHoldConflict) || ok {
		t.Fatalf("cross-campaign-version request-id reuse was not rejected: ok=%v err=%v", ok, err)
	}

	advanced, err := store.Create(ctx, execution.RoutingPlan{
		ID:                      "80000000-0000-4000-8000-000000000002",
		CampaignID:              created.CampaignID,
		RoutingPolicyVersion:    "meta-route-v1",
		CapacityEvidenceVersion: "meta-capacity-v1",
		FallbackMode:            "NONE",
		ApprovedBy:              created.ApprovedBy,
		ApprovalReason:          created.ApprovalReason,
		ApprovedCampaignVersion: created.ApprovedCampaignVersion,
		IdempotencyKey:          "capacity-hold-client-request-0002",
		RequestHash:             "request-hash-2",
		Routes: []execution.PoolRoute{{
			SenderPoolID: "30000000-0000-4000-8000-000000000002",
			MetaSenderID: "40000000-0000-4000-8000-000000000001",
			Provider:     string(campaign.ProviderMeta),
			Engine:       string(campaign.EngineMetaCloud),
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := findCapacityHoldReplay(ctx, admin, advanced.CampaignID, advanced.IdempotencyKey, advanced.ApprovedBy, advanced.ApprovalReason, advanced.ApprovedCampaignVersion); !errors.Is(err, errCapacityHoldConflict) || ok {
		t.Fatalf("non-Day-1 plan was accepted as a capacity-hold replay: ok=%v err=%v", ok, err)
	}
}

func TestCreateCampaignCapacityHoldExactReplaySurvivesLaterCampaignVersionChange(t *testing.T) {
	ctx := context.Background()
	const (
		campaignID = "10000000-0000-4000-8000-000000000001"
		approverID = "90000000-0000-4000-8000-000000000001"
		requestID  = "capacity-hold-client-request-0099"
		reason     = "approved exact-route capacity hold"
	)
	store := execution.NewMemoryRoutingPlanStore()
	created, err := store.Create(ctx, execution.RoutingPlan{
		ID:                      "80000000-0000-4000-8000-000000000099",
		CampaignID:              campaignID,
		RoutingPolicyVersion:    "route-v7",
		CapacityEvidenceVersion: "capacity-v9",
		PacingPolicyVersion:     "pacing/v4",
		FallbackMode:            "NONE",
		ApprovedBy:              approverID,
		ApprovalReason:          reason,
		ApprovedCampaignVersion: 7,
		IdempotencyKey:          requestID,
		RequestHash:             "request-hash-replay",
		Routes: []execution.PoolRoute{{
			SenderPoolID:  "30000000-0000-4000-8000-000000000001",
			GatewayPoolID: "20000000-0000-4000-8000-000000000001",
			Provider:      string(campaign.ProviderOpenWA),
			Engine:        string(campaign.EngineBaileys),
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	campaignRepository := campaign.NewMemoryRepository()
	if err := campaignRepository.Create(ctx, campaign.Campaign{
		ID: campaignID, Version: 8,
		Transport: campaign.TransportSelection{
			Provider:     campaign.ProviderOpenWA,
			Engine:       campaign.EngineBaileys,
			RoutingMode:  campaign.RoutingSenderPool,
			FallbackMode: campaign.FallbackNone,
		},
	}); err != nil {
		t.Fatal(err)
	}

	server := &Server{deps: Dependencies{
		Campaigns:    campaign.NewService(campaignRepository),
		RoutingPlans: &execution.RoutingAdministration{Store: store},
	}}
	verified := time.Now().UTC()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/campaigns/"+campaignID+"/capacity-hold",
		strings.NewReader("{\"expectedCampaignVersion\":7,\"reason\":\"approved exact-route capacity hold\",\"clientRequestId\":\"capacity-hold-client-request-0099\"}"),
	)
	request.SetPathValue("id", campaignID)
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{
		User:    identity.User{ID: approverID},
		Session: identity.Session{MFAVerifiedAt: &verified},
	}))
	response := httptest.NewRecorder()
	server.createCampaignCapacityHold(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), created.ID) || !strings.Contains(response.Body.String(), "\"replayed\":true") {
		t.Fatalf("exact replay did not return committed hold: %s", response.Body.String())
	}
}

func TestBuildCampaignCapacityHoldProposalAccountsForPartialFinalDay(t *testing.T) {
	entity, readiness, pacing := capacityHoldFixture()
	start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	end := start.Add(217 * time.Hour) // nine full days plus one hour.
	entity.RequestedStartAt = &start
	entity.CompletionDeadlineAt = &end
	entity.MaximumUniqueRecipients = 10_000
	entity.EligibleAudienceCount = 10_000
	readiness.CampaignVersion = entity.Version
	readiness.Capacity.BasisRecipientCount = 10_000
	required := float64(10_000) / end.Sub(start).Minutes()
	readiness.Capacity.RequiredMessagesPerMinute = &required
	readiness.Capacity.AvailableMessagesPerMinute = 10
	readiness.Capacity.AvailableHourlyUnits = 600
	readiness.Capacity.AvailableDailyUnits = 5_000
	pacing.Policy.MessagesPerMinute = 10
	pacing.Policy.HourlyAllowance = 600
	pacing.Policy.DailyAllowance = 5_000

	proposal, err := buildCampaignCapacityHoldProposal(entity, readiness, pacing)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.ReservedMessagesPerMinute != 1 || proposal.ReservedHourlyUnits != 60 {
		t.Fatalf("unexpected rate reservation: %+v", proposal)
	}
	if proposal.ReservedDailyUnits <= 1000 {
		t.Fatalf("partial final day was treated as a full day: daily=%d", proposal.ReservedDailyUnits)
	}

	readiness.Capacity.AvailableDailyUnits = 1000
	if _, err := buildCampaignCapacityHoldProposal(entity, readiness, pacing); err == nil {
		t.Fatal("old optimistic daily capacity unexpectedly admitted partial-final-day campaign")
	}
}
