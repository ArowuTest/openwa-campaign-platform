package consent

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
)

func TestOptOutPolicyUsesExactNormalisedCommands(t *testing.T) {
	policy := DefaultOptOutPolicy()
	for _, value := range []string{"stop", " STOP! ", "opt   out", "unsubscribe."} {
		if !policy.Recognises(value) {
			t.Fatalf("expected %q to be recognised", value)
		}
	}
	for _, value := range []string{"do not stop", "please stop sending eventually", "stopping", ""} {
		if policy.Recognises(value) {
			t.Fatalf("did not expect %q to be recognised", value)
		}
	}
}

type optOutMetricStub struct {
	calls         int
	recorded      int
	campaignID    string
	suppressionID string
	failFirst     bool
	seen          map[string]bool
}

func (s *optOutMetricStub) RecordOptOut(_ context.Context, campaignID, suppressionID string, _ time.Time) error {
	s.calls++
	s.campaignID = campaignID
	s.suppressionID = suppressionID
	if s.failFirst && s.calls == 1 {
		return errors.New("metric write unavailable")
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if !s.seen[suppressionID] {
		s.seen[suppressionID] = true
		s.recorded++
	}
	return nil
}

type atomicOptOutLedgerStub struct {
	*MemoryLedgerRepository
	calls      int
	campaignID string
}

func (s *atomicOptOutLedgerStub) CreateSuppressionWithOptOutMetric(ctx context.Context, v Suppression, campaignID string, _ time.Time) (Suppression, bool, error) {
	s.calls++
	s.campaignID = campaignID
	return s.MemoryLedgerRepository.CreateSuppression(ctx, v)
}

func TestOptOutProcessorPrefersAtomicMetricLedger(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "recipient-atomic", CampaignID: "00000000-0000-4000-8000-000000000022", ContactID: "00000000-0000-4000-8000-000000000012", Status: delivery.StatusDelivered, UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	repository := &atomicOptOutLedgerStub{MemoryLedgerRepository: NewMemoryLedgerRepository()}
	ledger := NewLedgerService(repository)
	metrics := &optOutMetricStub{failFirst: true}
	processor := &OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Metrics: metrics, Clock: func() time.Time { return now }}

	first, err := processor.Process(context.Background(), recipient.ID, "evt-stop-atomic", "STOP", "gateway:evt-stop-atomic")
	if err != nil || !first.Recognised || first.Replayed {
		t.Fatalf("atomic result=%+v err=%v", first, err)
	}
	replay, err := processor.Process(context.Background(), recipient.ID, "evt-stop-atomic", "STOP", "gateway:evt-stop-atomic")
	if err != nil || !replay.Replayed || replay.Suppression.ID != first.Suppression.ID {
		t.Fatalf("atomic replay=%+v err=%v", replay, err)
	}
	if repository.calls != 2 || repository.campaignID != recipient.CampaignID {
		t.Fatalf("atomic ledger calls=%d campaign=%s", repository.calls, repository.campaignID)
	}
	if metrics.calls != 0 {
		t.Fatalf("non-atomic metric fallback was invoked %d times", metrics.calls)
	}
}

func TestOptOutProcessorCreatesIdempotentGlobalSuppression(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "recipient-1", CampaignID: "00000000-0000-4000-8000-000000000020", ContactID: "00000000-0000-4000-8000-000000000010", Status: delivery.StatusDelivered, UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	ledger := NewLedgerService(NewMemoryLedgerRepository())
	metrics := &optOutMetricStub{}
	processor := &OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Metrics: metrics, Clock: func() time.Time { return now }}

	first, err := processor.Process(context.Background(), recipient.ID, "evt-stop-1", "STOP", "gateway:evt-stop-1")
	if err != nil || !first.Recognised || first.Replayed || first.Suppression.Scope != SuppressionGlobal {
		t.Fatalf("first result=%+v err=%v", first, err)
	}
	replay, err := processor.Process(context.Background(), recipient.ID, "evt-stop-1", " stop ", "gateway:evt-stop-1")
	if err != nil || !replay.Recognised || !replay.Replayed || replay.Suppression.ID != first.Suppression.ID {
		t.Fatalf("replay result=%+v err=%v", replay, err)
	}
	if metrics.calls != 2 || metrics.recorded != 1 || metrics.campaignID != recipient.CampaignID || metrics.suppressionID != first.Suppression.ID {
		t.Fatalf("opt-out metrics calls=%d recorded=%d campaign=%s suppression=%s", metrics.calls, metrics.recorded, metrics.campaignID, metrics.suppressionID)
	}
}

func TestOptOutProcessorRepairsMetricAfterPartialFailure(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "recipient-repair", CampaignID: "00000000-0000-4000-8000-000000000021", ContactID: "00000000-0000-4000-8000-000000000011", Status: delivery.StatusDelivered, UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	ledger := NewLedgerService(NewMemoryLedgerRepository())
	metrics := &optOutMetricStub{failFirst: true}
	processor := &OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Metrics: metrics, Clock: func() time.Time { return now }}

	if _, err := processor.Process(context.Background(), recipient.ID, "evt-stop-repair", "STOP", "gateway:evt-stop-repair"); err == nil {
		t.Fatal("expected first metric write to fail after durable suppression creation")
	}
	repaired, err := processor.Process(context.Background(), recipient.ID, "evt-stop-repair", "STOP", "gateway:evt-stop-repair")
	if err != nil || !repaired.Recognised || !repaired.Replayed {
		t.Fatalf("retry did not repair metric: result=%+v err=%v", repaired, err)
	}
	third, err := processor.Process(context.Background(), recipient.ID, "evt-stop-repair", "STOP", "gateway:evt-stop-repair")
	if err != nil || !third.Replayed {
		t.Fatalf("third replay failed: result=%+v err=%v", third, err)
	}
	if metrics.calls != 3 || metrics.recorded != 1 || metrics.suppressionID != repaired.Suppression.ID {
		t.Fatalf("metric repair not idempotent: calls=%d recorded=%d suppression=%s", metrics.calls, metrics.recorded, metrics.suppressionID)
	}
}
