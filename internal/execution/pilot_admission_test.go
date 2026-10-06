package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/testmessage"
)

func pilotFixture(now time.Time) (campaign.Campaign, testmessage.Send, RoutingPlan, []CapacityReservation) {
	start := now.Add(time.Hour)
	end := start.Add(2 * time.Hour)
	entity := campaign.Campaign{
		ID: "campaign-1", Status: campaign.StatusFinalApprovalPending, Version: 9,
		RequestedStartAt: &start, CompletionDeadlineAt: &end, MaximumUniqueRecipients: 100,
		MessageVersionID: "message-1", MessageContentHash: "content-hash",
		Transport: campaign.TransportSelection{
			Channel: "WHATSAPP", Provider: campaign.ProviderOpenWA, Engine: campaign.EngineWhatsAppWebJS,
			RoutingMode: campaign.RoutingSenderPool, GatewayPoolID: "gateway-1", GatewayPoolVersion: 7,
			SenderPoolID: "sender-1", AdapterVersion: "0.13.0+platform.1",
			ProviderDefinitionID: "provider-def-1", ProviderDefinitionVersion: 4,
			FallbackMode: campaign.FallbackNone, RoutingPolicyVersion: "route-v3", CapacityEvidenceVersion: "capacity-v5",
		},
	}
	send := testmessage.Send{
		ID: "test-1", CampaignID: entity.ID, MessageVersionID: entity.MessageVersionID,
		MessageContentHash: entity.MessageContentHash, GatewayPoolID: entity.Transport.GatewayPoolID,
		GatewayPoolVersion: entity.Transport.GatewayPoolVersion, SenderPoolID: entity.Transport.SenderPoolID,
		Provider: string(entity.Transport.Provider), Engine: string(entity.Transport.Engine),
		ProviderAdapterVersion:    entity.Transport.AdapterVersion,
		ProviderDefinitionID:      entity.Transport.ProviderDefinitionID,
		ProviderDefinitionVersion: entity.Transport.ProviderDefinitionVersion,
		SenderSessionID:           "session-1", RouteReference: "route:test-1", Status: testmessage.SendAccepted,
		CreatedAt: now.Add(-time.Minute),
	}
	plan := RoutingPlan{
		ID: "plan-1", CampaignID: entity.ID, Version: 2, DistributionMode: DistributionWeighted,
		RoutingPolicyVersion:    entity.Transport.RoutingPolicyVersion,
		CapacityEvidenceVersion: entity.Transport.CapacityEvidenceVersion,
		PacingPolicyVersion:     "pacing-v1", FallbackMode: "NONE", ApprovedAt: now,
		Routes: []PoolRoute{{
			SenderPoolID: entity.Transport.SenderPoolID, GatewayPoolID: entity.Transport.GatewayPoolID,
			Provider: string(entity.Transport.Provider), Engine: string(entity.Transport.Engine),
			ProviderAdapterVersion:    entity.Transport.AdapterVersion,
			ProviderDefinitionID:      entity.Transport.ProviderDefinitionID,
			ProviderDefinitionVersion: entity.Transport.ProviderDefinitionVersion,
			GatewayPoolVersion:        entity.Transport.GatewayPoolVersion,
			AllocationWeight:          100, MaximumRecipients: entity.MaximumUniqueRecipients,
			ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 100,
		}},
	}
	reservations := []CapacityReservation{{
		ID: "reservation-1", CampaignID: entity.ID, RoutingPlanID: plan.ID,
		SenderPoolID: entity.Transport.SenderPoolID, ReservationStart: start, ReservationEnd: end,
		ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 100,
		Status: "HELD", FencingVersion: 1,
	}}
	return entity, send, plan, reservations
}

func TestEvaluateDayOnePilotAdmissionAcceptsExactHeldRoute(t *testing.T) {
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	entity, send, plan, reservations := pilotFixture(now)

	evidence, err := EvaluateDayOnePilotAdmission(entity, []testmessage.Send{send}, plan, reservations)
	if err != nil {
		t.Fatalf("expected admission, got %v", err)
	}
	if evidence.AcceptedTestMessageID != send.ID || evidence.RoutingPlanID != plan.ID || evidence.ReservationID != reservations[0].ID {
		t.Fatalf("unexpected evidence: %#v", evidence)
	}
}

func TestEvaluateDayOnePilotAdmissionRejectsMissingAcceptedTest(t *testing.T) {
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	entity, send, plan, reservations := pilotFixture(now)
	send.Status = testmessage.SendUnknown

	if _, err := EvaluateDayOnePilotAdmission(entity, []testmessage.Send{send}, plan, reservations); err == nil {
		t.Fatal("missing accepted controlled-test evidence was admitted")
	}
}

func TestEvaluateDayOnePilotAdmissionRejectsMixedOrMismatchedRoute(t *testing.T) {
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	entity, send, plan, reservations := pilotFixture(now)
	plan.Routes = append(plan.Routes, PoolRoute{
		SenderPoolID: "sender-2", GatewayPoolID: "gateway-2", Provider: "OPENWA", Engine: "BAILEYS",
		AllocationWeight: 1, MaximumRecipients: 1, ReservedMessagesPerMinute: 1, ReservedHourlyUnits: 1, ReservedDailyUnits: 1,
	})
	reservations = append(reservations, CapacityReservation{
		ID: "reservation-2", CampaignID: entity.ID, RoutingPlanID: plan.ID, SenderPoolID: "sender-2",
		ReservationStart: *entity.RequestedStartAt, ReservationEnd: *entity.CompletionDeadlineAt,
		ReservedMessagesPerMinute: 1, ReservedHourlyUnits: 1, ReservedDailyUnits: 1, Status: "HELD", FencingVersion: 1,
	})

	if _, err := EvaluateDayOnePilotAdmission(entity, []testmessage.Send{send}, plan, reservations); err == nil {
		t.Fatal("mixed-route day-one campaign was admitted")
	}
}

func TestEvaluateDayOnePilotAdmissionRejectsReleasedHold(t *testing.T) {
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	entity, send, plan, reservations := pilotFixture(now)
	reservations[0].Status = "RELEASED"

	if _, err := EvaluateDayOnePilotAdmission(entity, []testmessage.Send{send}, plan, reservations); err == nil {
		t.Fatal("released pilot hold was admitted")
	}
}

func TestEvaluateDayOnePilotAdmissionRequiresActiveReservationForPausedCampaign(t *testing.T) {
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	entity, send, plan, reservations := pilotFixture(now)
	entity.Status = campaign.StatusPaused

	if _, err := EvaluateDayOnePilotAdmission(entity, []testmessage.Send{send}, plan, reservations); err == nil {
		t.Fatal("paused campaign with only a held reservation was admitted")
	}
	reservations[0].Status = "ACTIVE"
	if _, err := EvaluateDayOnePilotAdmission(entity, []testmessage.Send{send}, plan, reservations); err != nil {
		t.Fatalf("paused campaign with active reservation should remain resumable: %v", err)
	}
}

type pilotSendReader struct {
	sends []testmessage.Send
	err   error
}

func (r pilotSendReader) ListSends(context.Context, string) ([]testmessage.Send, error) {
	return append([]testmessage.Send(nil), r.sends...), r.err
}

func TestCoordinatorApproveFinalAtomicallyValidatesHeldPilotRoute(t *testing.T) {
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	entity, send, plan, reservations := pilotFixture(now)
	entity.AudienceSnapshotID = "snapshot-1"
	entity.AudienceSnapshotHash = "snapshot-hash"
	entity.EligibleAudienceCount = 10
	entity.CreatedBy = "creator-1"
	entity.UpdatedAt = now

	store := NewMemoryRoutingPlanStore()
	createdPlan, err := store.Create(context.Background(), plan, reservations)
	if err != nil {
		t.Fatal(err)
	}
	committer := &recordingLifecycleCommitter{}
	campaigns := &atomicCampaigns{current: entity}
	coordinator := &Coordinator{
		Campaigns:    campaigns,
		Committer:    committer,
		RoutingPlans: &RoutingAdministration{Store: store},
		TestMessages: pilotSendReader{sends: []testmessage.Send{send}},
		Clock:        func() time.Time { return now },
	}

	got, err := coordinator.ApproveFinal(context.Background(), entity.ID, campaign.TransitionInput{
		Action: campaign.ActionApproveFinal, ActorID: "approver-2", ExpectedVersion: entity.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != campaign.StatusScheduled {
		t.Fatalf("status=%s", got.Status)
	}
	if len(committer.commits) != 1 {
		t.Fatalf("commits=%d", len(committer.commits))
	}
	commit := committer.commits[0]
	if commit.ReservationOperation != ReservationValidateHeld || commit.RoutingPlanID != createdPlan.ID {
		t.Fatalf("unexpected pilot lifecycle commit: %+v", commit)
	}
	if commit.Details["acceptedTestMessageId"] != send.ID {
		t.Fatalf("accepted test not bound to lifecycle evidence: %+v", commit.Details)
	}
}

func TestCoordinatorApproveFinalRejectsDirectBypassWithoutAcceptedTest(t *testing.T) {
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	entity, _, plan, reservations := pilotFixture(now)
	entity.AudienceSnapshotID = "snapshot-1"
	entity.AudienceSnapshotHash = "snapshot-hash"
	entity.EligibleAudienceCount = 10
	entity.CreatedBy = "creator-1"
	entity.UpdatedAt = now

	store := NewMemoryRoutingPlanStore()
	if _, err := store.Create(context.Background(), plan, reservations); err != nil {
		t.Fatal(err)
	}
	committer := &recordingLifecycleCommitter{}
	coordinator := &Coordinator{
		Campaigns:    &atomicCampaigns{current: entity},
		Committer:    committer,
		RoutingPlans: &RoutingAdministration{Store: store},
		TestMessages: pilotSendReader{},
		Clock:        func() time.Time { return now },
	}

	_, err := coordinator.ApproveFinal(context.Background(), entity.ID, campaign.TransitionInput{
		Action: campaign.ActionApproveFinal, ActorID: "approver-2", ExpectedVersion: entity.Version,
	})
	if err == nil || !errors.Is(err, ErrPilotAdmission) {
		t.Fatalf("expected pilot-admission rejection, got %v", err)
	}
	if len(committer.commits) != 0 {
		t.Fatalf("bypass reached lifecycle commit: %+v", committer.commits)
	}
}
