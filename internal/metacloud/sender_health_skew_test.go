package metacloud

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSenderHealthRejectsFarFutureObservation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 16, 21, 30, 0, 0, time.UTC)
	svc := &Service{Store: NewMemoryStore(), Clock: func() time.Time { return now }}
	draft, err := svc.CreateDraft(ctx, validSender(), "maker", "create Meta sender")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := svc.Submit(ctx, draft.ID, draft.Version, "submitter", "submit Meta sender")
	if err != nil {
		t.Fatal(err)
	}
	active, err := svc.Decide(ctx, pending.ID, pending.Version, true, "checker", "approve Meta sender", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ObserveHealth(ctx, active.ID, active.Version, HealthHealthy, now.Add(2*time.Minute), "worker", "future Graph verification"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("far-future health evidence was accepted: %v", err)
	}
	fresh, err := svc.ObserveHealth(ctx, active.ID, active.Version, HealthHealthy, now.Add(30*time.Second), "worker", "current Graph verification")
	if err != nil {
		t.Fatalf("current health evidence was rejected after future probe: %v", err)
	}
	if fresh.HealthObservedAt == nil || !fresh.HealthObservedAt.Equal(now.Add(30*time.Second)) {
		t.Fatalf("unexpected current health evidence: %#v", fresh.HealthObservedAt)
	}
}
