package campaign

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func baseCampaign(t *testing.T) Campaign {
	t.Helper()
	start := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	deadline := time.Date(2026, 8, 5, 20, 0, 0, 0, time.UTC)
	entity, err := New(CreateInput{
		Transport:      validTransport(),
		OrganisationID: "org-1", Name: "Festival reminder", PurposeID: "purpose-1",
		ConsentReviewID: "review-1", RequestedStartAt: &start, CompletionDeadlineAt: &deadline,
		MaximumUniqueRecipients: 50_000, MaximumMessagesPerRecipient: 1, SenderPool: "EVENTS-NG", CreatedBy: "operator-1",
	}, time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return entity
}

func transition(t *testing.T, entity Campaign, input TransitionInput, now time.Time) Campaign {
	t.Helper()
	input.ExpectedVersion = entity.Version
	next, err := entity.Transition(input, now)
	if err != nil {
		t.Fatalf("transition %s: %v", input.Action, err)
	}
	return next
}

func TestCampaignFourEyesFinalApproval(t *testing.T) {
	entity := baseCampaign(t)
	now := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	steps := []TransitionInput{
		{Action: ActionSubmitConsentReview, ActorID: "operator-1"},
		{Action: ActionApproveConsent, ActorID: "compliance-1"},
		{Action: ActionStartAudienceBuild, ActorID: "operator-1"},
		{Action: ActionValidateAudience, ActorID: "operator-1", AudienceSnapshotID: "snap-1", AudienceSnapshotHash: "hash", EligibleAudienceCount: 49_000},
		{Action: ActionSubmitMessage, ActorID: "operator-1", MessageVersionID: "msg-1", MessageContentHash: HashMessage("Approved message")},
		{Action: ActionApproveMessage, ActorID: "approver-1"},
		{Action: ActionApproveCommercial, ActorID: "finance-1"},
		{Action: ActionRequestFinalApproval, ActorID: "operator-1"},
	}
	for _, step := range steps {
		entity = transition(t, entity, step, now)
	}
	if _, err := entity.Transition(TransitionInput{Action: ActionApproveFinal, ActorID: "operator-1", ExpectedVersion: entity.Version}, now); err == nil {
		t.Fatal("creator should not be able to provide final approval")
	}
	entity = transition(t, entity, TransitionInput{Action: ActionApproveFinal, ActorID: "approver-2"}, now)
	if entity.Status != StatusScheduled {
		t.Fatalf("unexpected status %s", entity.Status)
	}
}

func TestAudienceCannotExceedAuthorisedMaximum(t *testing.T) {
	entity := baseCampaign(t)
	now := time.Now()
	entity = transition(t, entity, TransitionInput{Action: ActionSubmitConsentReview, ActorID: "operator"}, now)
	entity = transition(t, entity, TransitionInput{Action: ActionApproveConsent, ActorID: "reviewer"}, now)
	entity = transition(t, entity, TransitionInput{Action: ActionStartAudienceBuild, ActorID: "operator"}, now)
	_, err := entity.Transition(TransitionInput{
		Action: ActionValidateAudience, ActorID: "operator", AudienceSnapshotID: "snap",
		AudienceSnapshotHash: "hash", EligibleAudienceCount: 50_001, ExpectedVersion: entity.Version,
	}, now)
	if err == nil {
		t.Fatal("expected entitlement limit rejection")
	}
}

func TestConcurrentTransitionsUseOptimisticConcurrency(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo)
	entity := baseCampaign(t)
	entity.Status = StatusScheduled
	entity.Version = 9
	if err := repo.Create(context.Background(), entity); err != nil {
		t.Fatal(err)
	}

	inputs := []TransitionInput{
		{Action: ActionStartDispatch, ActorID: "operator-a", ExpectedVersion: 9},
		{Action: ActionCancel, ActorID: "operator-b", Reason: "client cancelled", ExpectedVersion: 9},
	}
	var wg sync.WaitGroup
	errorsSeen := make(chan error, len(inputs))
	for _, input := range inputs {
		wg.Add(1)
		go func(in TransitionInput) {
			defer wg.Done()
			_, err := service.Transition(context.Background(), entity.ID, in)
			errorsSeen <- err
		}(input)
	}
	wg.Wait()
	close(errorsSeen)

	successes, conflicts := 0, 0
	for err := range errorsSeen {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected one success and one conflict, got successes=%d conflicts=%d", successes, conflicts)
	}
}

func approvedCampaign(t *testing.T) Campaign {
	t.Helper()
	entity := baseCampaign(t)
	now := time.Date(2026, 8, 4, 7, 0, 0, 0, time.UTC)
	steps := []TransitionInput{
		{Action: ActionSubmitConsentReview, ActorID: "operator-1"},
		{Action: ActionApproveConsent, ActorID: "compliance-1"},
		{Action: ActionStartAudienceBuild, ActorID: "operator-1"},
		{Action: ActionValidateAudience, ActorID: "operator-1", AudienceSnapshotID: "snap-1", AudienceSnapshotHash: "aud-hash", EligibleAudienceCount: 40_000},
		{Action: ActionSubmitMessage, ActorID: "operator-1", MessageVersionID: "msg-1", MessageContentHash: "msg-hash"},
		{Action: ActionApproveMessage, ActorID: "approver-1"},
		{Action: ActionApproveCommercial, ActorID: "finance-1", CommercialApprovalID: "commercial-1"},
		{Action: ActionRequestFinalApproval, ActorID: "operator-1"},
		{Action: ActionApproveFinal, ActorID: "approver-2"},
	}
	for _, step := range steps {
		entity = transition(t, entity, step, now)
	}
	return entity
}

func TestMaterialTransportChangeInvalidatesFinalApproval(t *testing.T) {
	entity := approvedCampaign(t)
	transport := entity.Transport
	transport.GatewayPoolID = "pool-2"
	amended, event, err := entity.AmendMaterial(MaterialAmendmentInput{
		ActorID: "operator-2", Reason: "move to recovered gateway pool", ExpectedVersion: entity.Version,
		ChangeTransport: true, Transport: transport,
	}, time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if amended.Status != StatusCommercialApproved {
		t.Fatalf("expected commercial approved regression, got %s", amended.Status)
	}
	if amended.FinalApprovedBy != "" {
		t.Fatal("final approval was not invalidated")
	}
	if amended.CommercialApprovalID == "" {
		t.Fatal("transport-only change should retain commercial approval")
	}
	if len(event.ChangedFields) != 1 || event.ChangedFields[0] != "TRANSPORT" {
		t.Fatalf("unexpected event fields %#v", event.ChangedFields)
	}
}

func TestMaterialMessageChangeInvalidatesCommercialApproval(t *testing.T) {
	entity := approvedCampaign(t)
	amended, _, err := entity.AmendMaterial(MaterialAmendmentInput{
		ActorID: "operator-2", Reason: "approved correction", ExpectedVersion: entity.Version,
		ChangeMessage: true, MessageVersionID: "msg-2", MessageContentHash: "msg-hash-2",
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if amended.Status != StatusMessageReviewPending {
		t.Fatalf("expected message review regression, got %s", amended.Status)
	}
	if amended.CommercialApprovalID != "" || amended.FinalApprovedBy != "" {
		t.Fatal("downstream approvals were not invalidated")
	}
}

func TestMaterialAmendmentRejectedAfterDispatchStarts(t *testing.T) {
	entity := approvedCampaign(t)
	entity = transition(t, entity, TransitionInput{Action: ActionStartDispatch, ActorID: "operator-2"}, time.Now())
	_, _, err := entity.AmendMaterial(MaterialAmendmentInput{ActorID: "operator-2", Reason: "late change", ExpectedVersion: entity.Version, ChangeSchedule: true, RequestedStartAt: entity.RequestedStartAt, CompletionDeadlineAt: entity.CompletionDeadlineAt}, time.Now())
	if err == nil {
		t.Fatal("expected amendment rejection after dispatch")
	}
}

func TestMaterialAmendmentPersistsAppendOnlyEvent(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo)
	entity := approvedCampaign(t)
	if err := repo.Create(context.Background(), entity); err != nil {
		t.Fatal(err)
	}
	start := entity.RequestedStartAt.Add(time.Hour)
	deadline := entity.CompletionDeadlineAt.Add(time.Hour)
	amended, err := service.AmendMaterial(context.Background(), entity.ID, MaterialAmendmentInput{ActorID: "operator-2", Reason: "client changed window", ExpectedVersion: entity.Version, ChangeSchedule: true, RequestedStartAt: &start, CompletionDeadlineAt: &deadline})
	if err != nil {
		t.Fatal(err)
	}
	items, err := service.ListMaterialChanges(context.Background(), entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].NewVersion != amended.Version || items[0].Sequence != 1 {
		t.Fatalf("unexpected events %#v", items)
	}
}

func TestCampaignDispatchWindowHonoursTimezoneQuietHoursAndDeadline(t *testing.T) {
	start := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	deadline := time.Date(2026, 8, 5, 20, 0, 0, 0, time.UTC)
	entity, err := New(CreateInput{
		OrganisationID: "org", Name: "window", PurposeID: "purpose", ConsentReviewID: "review",
		RequestedStartAt: &start, CompletionDeadlineAt: &deadline, Timezone: "Africa/Lagos",
		QuietHoursStart: "22:00", QuietHoursEnd: "07:00", MaximumUniqueRecipients: 10,
		Transport: validTransport(), CreatedBy: "maker",
	}, start.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	before, err := entity.EvaluateDispatchWindow(start.Add(-time.Minute))
	if err != nil || before.Allowed || before.Reason != "CAMPAIGN_NOT_STARTED" {
		t.Fatalf("unexpected before-start decision %+v %v", before, err)
	}
	quiet, err := entity.EvaluateDispatchWindow(time.Date(2026, 8, 5, 22, 30, 0, 0, time.UTC)) // 23:30 Lagos
	if err != nil || quiet.Allowed || quiet.Reason != "CAMPAIGN_DEADLINE_PASSED" {
		t.Fatalf("deadline must take precedence %+v %v", quiet, err)
	}
	entity.CompletionDeadlineAt = ptrTime(time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC))
	quiet, err = entity.EvaluateDispatchWindow(time.Date(2026, 8, 5, 22, 30, 0, 0, time.UTC))
	if err != nil || quiet.Allowed || quiet.Reason != "CAMPAIGN_QUIET_HOURS" {
		t.Fatalf("unexpected quiet decision %+v %v", quiet, err)
	}
	allowed, err := entity.EvaluateDispatchWindow(time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC))
	if err != nil || !allowed.Allowed {
		t.Fatalf("expected allowed window %+v %v", allowed, err)
	}
}

func TestCampaignRejectsInvalidTimezoneAndPartialQuietHours(t *testing.T) {
	_, err := New(CreateInput{OrganisationID: "org", Name: "bad", PurposeID: "purpose", ConsentReviewID: "review", Timezone: "Mars/Olympus", MaximumUniqueRecipients: 1, Transport: validTransport(), CreatedBy: "maker"}, time.Now())
	if err == nil {
		t.Fatal("invalid timezone accepted")
	}
	_, err = New(CreateInput{OrganisationID: "org", Name: "bad", PurposeID: "purpose", ConsentReviewID: "review", Timezone: "UTC", QuietHoursStart: "22:00", MaximumUniqueRecipients: 1, Transport: validTransport(), CreatedBy: "maker"}, time.Now())
	if err == nil {
		t.Fatal("partial quiet hours accepted")
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
