package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/campaign"
	"campaign-platform/internal/delivery"
	"campaign-platform/internal/dispatch"
	"campaign-platform/internal/jobs"
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
