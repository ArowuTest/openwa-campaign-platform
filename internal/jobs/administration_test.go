package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAdministrationRetriesDeadLetterWithEvidence(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	baseService := NewService(repo)
	baseService.clock = func() time.Time { return now }
	job, _, err := baseService.Enqueue(context.Background(), EnqueueInput{Type: "DISPATCH", DedupKey: "one", Payload: map[string]string{"id": "1"}, MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.Claim(context.Background(), "worker", now, time.Minute, 1, nil)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v %#v", err, claimed)
	}
	if err := repo.Fail(context.Background(), job.ID, "worker", claimed[0].LeaseVersion, now.Add(time.Second), false, 0, "BROKEN", "failed"); err != nil {
		t.Fatal(err)
	}
	svc := &AdministrationService{Repository: repo, Clock: func() time.Time { return now.Add(2 * time.Second) }}
	retried, err := svc.Retry(context.Background(), job.ID, "actor", "approved operational replay")
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != StatusPending || retried.AttemptCount != 0 {
		t.Fatalf("unexpected retry state: %#v", retried)
	}
	events, err := svc.Events(context.Background(), job.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "RETRY_DEAD_LETTER" {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func TestAdministrationRejectsWrongStateAndWeakReason(t *testing.T) {
	repo := NewMemoryRepository()
	job, _, _ := NewService(repo).Enqueue(context.Background(), EnqueueInput{Type: "X", DedupKey: "two", Payload: map[string]string{"id": "2"}})
	svc := &AdministrationService{Repository: repo}
	if _, err := svc.Retry(context.Background(), job.ID, "actor", "valid retry reason"); !errors.Is(err, ErrAdministrativeConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if _, err := svc.Cancel(context.Background(), job.ID, "actor", "short"); err == nil {
		t.Fatal("expected reason validation")
	}
}

func TestAdministrationSummaryAndCancel(t *testing.T) {
	now := time.Now().UTC()
	repo := NewMemoryRepository()
	service := NewService(repo)
	first, _, _ := service.Enqueue(context.Background(), EnqueueInput{Type: "A", DedupKey: "a", Payload: 1})
	_, _, _ = service.Enqueue(context.Background(), EnqueueInput{Type: "B", DedupKey: "b", Payload: 2})
	svc := &AdministrationService{Repository: repo, Clock: func() time.Time { return now }}
	if _, err := svc.Cancel(context.Background(), first.ID, "actor", "cancel duplicate campaign job"); err != nil {
		t.Fatal(err)
	}
	summary, err := svc.Summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.CountsByStatus[StatusCancelled] != 1 || summary.CountsByStatus[StatusPending] != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}
