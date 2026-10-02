package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/audit"
	"campaign-platform/internal/campaign"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/delivery"
	"campaign-platform/internal/dispatch"
	"campaign-platform/internal/jobs"
	"campaign-platform/internal/operations"
	"campaign-platform/internal/orchestration"
	"campaign-platform/internal/outbox"
	"campaign-platform/internal/segment"
)

type materialLoader struct{}

func (materialLoader) Load(_ context.Context, recipient delivery.Recipient) (dispatch.Material, error) {
	now := time.Now().UTC()
	return dispatch.Material{
		Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", GatewayPoolID: "gateway-pool-1", GatewayPoolVersion: 1,
		GatewayAdapterVersion: "0.13.0", GatewayNodeID: "gateway-node-1", GatewayNodeVersion: 1,
		SessionID: "session-healthy-1", SessionLeaseVersion: 1, SessionConfigurationVersion: 1,
		AuthorityExpiresAt: now.Add(10 * time.Minute), RouteReference: recipient.CampaignID + ":" + recipient.ID,
		RecipientE164: "+2348012345678", MessageType: "text", Body: "approved campaign message",
		ClientReference: recipient.ID,
	}, nil
}

type liveEligibility struct{}

func (liveEligibility) Check(_ context.Context, recipient delivery.Recipient, _ time.Time) (dispatch.EligibilityDecision, error) {
	if recipient.ContactID == "contact-withdrawn" {
		return dispatch.EligibilityDecision{Eligible: false, Reason: "CONSENT_WITHDRAWN"}, nil
	}
	return dispatch.EligibilityDecision{Eligible: true}, nil
}

type countingGateway struct {
	mu      sync.Mutex
	calls   int
	byKey   map[string]dispatch.GatewayResult
	request []dispatch.GatewayRequest
}

func (g *countingGateway) Preflight(context.Context, dispatch.GatewayRequest) error {
	return nil
}

func (g *countingGateway) SendPrepared(ctx context.Context, request dispatch.GatewayRequest) (dispatch.GatewayResult, error) {
	return g.Send(ctx, request)
}
func (g *countingGateway) Send(_ context.Context, request dispatch.GatewayRequest) (dispatch.GatewayResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if result, ok := g.byKey[request.IdempotencyKey]; ok {
		return result, nil
	}
	g.calls++
	g.request = append(g.request, request)
	result := dispatch.GatewayResult{
		Accepted:          true,
		ProviderMessageID: fmt.Sprintf("provider-%d", g.calls),
		AcceptedAt:        time.Now().UTC(),
	}
	g.byKey[request.IdempotencyKey] = result
	return result, nil
}

func (g *countingGateway) Calls() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

type workflowOperationsRepository struct {
	*operations.MemoryRepository
	report operations.CampaignReport
}

func (r *workflowOperationsRepository) CampaignReport(_ context.Context, id string, now time.Time) (operations.CampaignReport, error) {
	if id != r.report.CampaignID {
		return operations.CampaignReport{}, operations.ErrNotFound
	}
	out := r.report
	out.GeneratedAt = now.UTC()
	return out, nil
}

type workflowOptOutMetrics struct {
	report *operations.CampaignReport
	seen   map[string]struct{}
}

func (m *workflowOptOutMetrics) RecordOptOut(_ context.Context, campaignID, suppressionID string, _ time.Time) error {
	if m.report == nil || campaignID != m.report.CampaignID {
		return fmt.Errorf("unexpected opt-out campaign %s", campaignID)
	}
	if m.seen == nil {
		m.seen = map[string]struct{}{}
	}
	if _, ok := m.seen[suppressionID]; ok {
		return nil
	}
	m.seen[suppressionID] = struct{}{}
	if m.report.Engagement == nil {
		m.report.Engagement = map[string]int64{}
	}
	m.report.Engagement["optOuts"]++
	return nil
}

func TestDryRunReleaseRecoveryDispatchAndFinalWithdrawal(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)

	members := []segment.Member{
		{ContactID: "contact-eligible", EligibilityEvidenceHash: "evidence-1"},
		{ContactID: "contact-withdrawn", EligibilityEvidenceHash: "evidence-2"},
	}
	definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorEquals, Values: []any{"NG"}}}}
	builder, err := segment.NewBuilder("campaign-1", "", definition, 1, "consent-v1", "config-v1", "operator-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		if err := builder.Add(member.ContactID, member.EligibilityEvidenceHash); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := builder.Finalise(now)
	if err != nil {
		t.Fatal(err)
	}

	campaignRepo := campaign.NewMemoryRepository()
	campaignEntity := campaign.Campaign{
		ID: "campaign-1", OrganisationID: "org-1", PurposeID: "purpose-1",
		Status: campaign.StatusScheduled, MaximumUniqueRecipients: 2,
		AudienceSnapshotID: snapshot.ID, AudienceSnapshotHash: snapshot.SnapshotHash,
		EligibleAudienceCount: 2, MessageVersionID: "message-1",
		MessageContentHash: "message-hash", Version: 1,
	}
	if err := campaignRepo.Create(ctx, campaignEntity); err != nil {
		t.Fatal(err)
	}

	snapshotRepo := segment.NewMemoryRepository()
	if err := snapshotRepo.CreateWithMembers(ctx, snapshot, members); err != nil {
		t.Fatal(err)
	}

	releaseStore := orchestration.NewMemoryStore()
	release := orchestration.ReleaseService{
		Campaigns: campaign.NewService(campaignRepo), Snapshots: segment.NewService(snapshotRepo), Store: releaseStore,
		Eligibility: orchestration.EligibilityFunc(func(context.Context, string, string, string, string, time.Time) (orchestration.EligibilityDecision, error) {
			// Snapshot release confirms the recorded eligibility evidence. The later
			// dispatch check deliberately detects a withdrawal for one member.
			return orchestration.EligibilityDecision{Eligible: true}, nil
		}),
		BatchSize: 1, Clock: func() time.Time { return now },
	}
	first, err := release.Release(ctx, campaignEntity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Authorised != 2 || first.OutboxCreated != 2 || first.BatchesProcessed != 2 {
		t.Fatalf("unexpected release summary: %+v", first)
	}
	second, err := release.Release(ctx, campaignEntity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Authorised != 0 || second.OutboxCreated != 0 || second.Existing != 2 {
		t.Fatalf("release replay was not idempotent: %+v", second)
	}

	recipients := mustRecipients(t, releaseStore, campaignEntity.ID)
	ledgerRepo := delivery.NewMemoryRepository(recipients...)
	ledger := delivery.NewService(ledgerRepo)

	outboxRecords := make([]outbox.Record, 0, 2)
	for _, value := range mustOutbox(t, releaseStore) {
		outboxRecords = append(outboxRecords, outbox.Record{
			ID: value.ID, DedupKey: value.DedupKey, AggregateType: "CAMPAIGN_RECIPIENT",
			AggregateID: value.AggregateID, EventType: value.EventType,
			Payload: append(json.RawMessage(nil), value.Payload...), Status: outbox.StatusPending,
			AvailableAt: now, MaxAttempts: 5, CreatedAt: now, UpdatedAt: now,
		})
	}
	outboxRepo := outbox.NewMemoryRepository(outboxRecords...)
	jobRepo := jobs.NewMemoryRepository()
	jobService := jobs.NewService(jobRepo)
	publisher := outbox.Publisher{Outbox: outboxRepo, Jobs: jobService}

	// Simulate a publisher crash after claim. A second owner must reclaim after
	// lease expiry, and replayed publication must still produce one job per record.
	deadClaim, err := outboxRepo.Claim(ctx, "publisher-dead", now, time.Second, 10)
	if err != nil || len(deadClaim) != 2 {
		t.Fatalf("dead publisher claim: count=%d err=%v", len(deadClaim), err)
	}
	recovered, err := outboxRepo.Claim(ctx, "publisher-live", now.Add(2*time.Second), time.Minute, 10)
	if err != nil || len(recovered) != 2 {
		t.Fatalf("recovery claim: count=%d err=%v", len(recovered), err)
	}
	for _, record := range recovered {
		if err := publisher.Publish(ctx, record); err != nil {
			t.Fatal(err)
		}
		if err := publisher.Publish(ctx, record); err != nil {
			t.Fatal(err)
		}
		if err := outboxRepo.Complete(ctx, record.ID, "publisher-live", record.LeaseVersion, now.Add(3*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := jobRepo.PendingCount(ctx, []string{dispatch.JobType}, now.Add(4*time.Second))
	if err != nil || pending != 2 {
		t.Fatalf("expected two deduplicated dispatch jobs, pending=%d err=%v", pending, err)
	}

	gateway := &countingGateway{byKey: map[string]dispatch.GatewayResult{}}
	handler := dispatch.Handler{Ledger: ledger, Materials: materialLoader{}, Eligibility: liveEligibility{}, Gateway: gateway}
	runnerCtx, cancel := context.WithCancel(context.Background())
	runnerResult := make(chan error, 1)
	runner := jobs.Runner{
		Repository: jobRepo, Owner: "campaign-worker-1", Types: []string{dispatch.JobType},
		Concurrency: 2, ClaimBatch: 2, Lease: 500 * time.Millisecond, PollInterval: 5 * time.Millisecond,
		Handler: handler.Handle,
	}
	go func() { runnerResult <- runner.Run(runnerCtx) }()

	deadline := time.Now().Add(3 * time.Second)
	for {
		accepted, suppressed := false, false
		for _, recipient := range recipients {
			current, getErr := ledger.Get(ctx, recipient.ID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			switch current.ContactID {
			case "contact-eligible":
				accepted = current.Status == delivery.StatusGatewayAccepted
			case "contact-withdrawn":
				suppressed = current.Status == delivery.StatusSuppressedBeforeSend
			}
		}
		if accepted && suppressed && runner.Active() == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dispatch pipeline did not reach terminal dry-run states")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-runnerResult; err != nil {
		t.Fatal(err)
	}
	if gateway.Calls() != 1 {
		t.Fatalf("expected one gateway send after final withdrawal, got %d", gateway.Calls())
	}

	// Replaying the completed obligation cannot call the gateway again.
	for _, recipient := range recipients {
		if recipient.ContactID != "contact-eligible" {
			continue
		}
		job, err := jobs.NewJob(jobs.EnqueueInput{Type: dispatch.JobType, DedupKey: "manual-replay", Payload: dispatch.JobPayload{CampaignRecipientID: recipient.ID}}, now)
		if err != nil {
			t.Fatal(err)
		}
		job.AttemptCount = 1
		if err := handler.Handle(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	if gateway.Calls() != 1 {
		t.Fatalf("gateway was called again during replay: %d", gateway.Calls())
	}

	// Continue the same accepted obligation through an ambiguous provider outcome,
	// governed operator reconciliation, opt-out reporting, report generation, audit
	// evidence and maker-checker export. This is the release-one composed backend
	// workflow acceptance rather than a collection of unrelated package checks.
	var eligible delivery.Recipient
	for _, recipient := range recipients {
		if recipient.ContactID == "contact-eligible" {
			eligible, err = ledger.Get(ctx, recipient.ID)
			if err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if eligible.ID == "" || eligible.Status != delivery.StatusGatewayAccepted {
		t.Fatalf("eligible recipient missing accepted state: %+v", eligible)
	}
	unknownAt := now.Add(5 * time.Minute)
	unknown, changed, err := ledger.ApplyEvent(ctx, eligible.ID, delivery.Event{
		DeduplicationKey: "workflow-unknown:" + eligible.ID,
		Type:             delivery.EventUnknown, ErrorCode: "GATEWAY_OUTCOME_UNKNOWN",
		ErrorDetail: "provider outcome was ambiguous", OccurredAt: unknownAt,
	})
	if err != nil || !changed || unknown.Status != delivery.StatusUnknown || !unknown.ReconciliationRequired {
		t.Fatalf("ambiguous outcome was not quarantined: recipient=%+v changed=%v err=%v", unknown, changed, err)
	}
	if gateway.Calls() != 1 {
		t.Fatalf("UNKNOWN triggered an unsafe resend: %d", gateway.Calls())
	}

	opRepo := &workflowOperationsRepository{MemoryRepository: operations.NewMemoryRepository(), report: operations.CampaignReport{
		CampaignID: campaignEntity.ID, OrganisationID: campaignEntity.OrganisationID, Name: "backend-freeze-campaign",
		Status: "DISPATCHING", Audience: map[string]int64{"authorised": 2, "suppressed": 1},
		Delivery:   map[string]int64{"queued": 0, "submitted": 0, "gatewayAccepted": 0, "sent": 1, "delivered": 0, "read": 0},
		Engagement: map[string]int64{"optOuts": 0}, Exceptions: map[string]int64{"failed": 0, "unknown": 0},
	}}
	auditRepo := audit.NewMemoryRepository()
	opService := &operations.Service{
		Repo: opRepo, Audit: audit.NewRecorder(auditRepo), AuditRepository: auditRepo, Deliveries: ledger,
		Clock: func() time.Time { return now.Add(6 * time.Minute) },
	}
	resolution, err := opService.ResolveDeliveryException(ctx, eligible.ID, operations.ResolutionConfirmSent,
		"evidence://provider-confirmed-sent", "provider evidence confirms submission", "operator-reconciler", "workflow-reconcile")
	if err != nil || resolution.ResultStatus != delivery.StatusSent {
		t.Fatalf("UNKNOWN reconciliation failed: resolution=%+v err=%v", resolution, err)
	}
	if gateway.Calls() != 1 {
		t.Fatalf("operator reconciliation triggered provider resend: %d", gateway.Calls())
	}

	optOutMetrics := &workflowOptOutMetrics{report: &opRepo.report}
	optOut := &consent.OptOutProcessor{
		Deliveries: ledger, Ledger: consent.NewLedgerService(consent.NewMemoryLedgerRepository()), Metrics: optOutMetrics,
		Clock: func() time.Time { return now.Add(7 * time.Minute) },
	}
	firstOptOut, err := optOut.Process(ctx, eligible.ID, "workflow-stop-1", "STOP", "gateway:workflow-stop-1")
	if err != nil || !firstOptOut.Recognised || firstOptOut.Replayed {
		t.Fatalf("opt-out was not recorded: result=%+v err=%v", firstOptOut, err)
	}
	replayedOptOut, err := optOut.Process(ctx, eligible.ID, "workflow-stop-1", "STOP", "gateway:workflow-stop-1")
	if err != nil || !replayedOptOut.Replayed || opRepo.report.Engagement["optOuts"] != 1 {
		t.Fatalf("opt-out replay was not idempotent: result=%+v metric=%d err=%v", replayedOptOut, opRepo.report.Engagement["optOuts"], err)
	}

	report, err := opService.CampaignReport(ctx, campaignEntity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.Delivery["sent"] != 1 || report.Engagement["optOuts"] != 1 || report.Audience["suppressed"] != 1 {
		t.Fatalf("campaign report lost workflow truth: %+v", report)
	}

	requested, err := opService.RequestExport(ctx, "CAMPAIGN_REPORT", campaignEntity.ID, "JSON", "release-one campaign evidence", "operator-maker", "workflow-export-request")
	if err != nil {
		t.Fatal(err)
	}
	approved, err := opService.DecideExport(ctx, requested.ID, requested.Version, true, "independent approval", "operator-checker", "workflow-export-approve")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != operations.ExportApproved || len(approved.FrozenPayload) == 0 {
		t.Fatalf("approved campaign export was not frozen: %+v", approved)
	}
	auditPage, err := auditRepo.Search(ctx, audit.Query{ObjectType: "EXPORT_REQUEST", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]bool{}
	for _, event := range auditPage.Items {
		actions[event.Action] = true
	}
	if !actions["EXPORT_REQUESTED"] || !actions["EXPORT_APPROVED"] {
		t.Fatalf("export audit evidence incomplete: %+v", actions)
	}
	reconciliationAudit, err := auditRepo.Search(ctx, audit.Query{Action: "DELIVERY_EXCEPTION_RESOLVED", ObjectID: eligible.ID, Limit: 10})
	if err != nil || len(reconciliationAudit.Items) != 1 {
		t.Fatalf("reconciliation audit evidence missing: count=%d err=%v", len(reconciliationAudit.Items), err)
	}
}

func mustRecipients(t *testing.T, store orchestration.EvidenceStore, campaignID string) []delivery.Recipient {
	t.Helper()
	items, err := store.ListRecipients(context.Background(), campaignID, "", "", 5_000)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func mustOutbox(t *testing.T, store orchestration.EvidenceStore) []orchestration.Outbox {
	t.Helper()
	items, err := store.ListOutbox(context.Background(), time.Time{}, "", 5_000)
	if err != nil {
		t.Fatal(err)
	}
	return items
}
