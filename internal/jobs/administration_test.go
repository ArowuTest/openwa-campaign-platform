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
	if events[0].ID == "" {
		t.Fatalf("administration event identity was discarded: %#v", events[0])
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

func TestAdministrationListPageContinuesWithoutDuplicates(t *testing.T) {
	repo := NewMemoryRepository()
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo.items["job-a"] = Job{ID: "job-a", Type: "DISPATCH", Status: StatusPending, CreatedAt: base, UpdatedAt: base}
	repo.items["job-b"] = Job{ID: "job-b", Type: "DISPATCH", Status: StatusPending, CreatedAt: base.Add(time.Minute), UpdatedAt: base.Add(time.Minute)}
	repo.items["job-c"] = Job{ID: "job-c", Type: "DISPATCH", Status: StatusPending, CreatedAt: base.Add(2 * time.Minute), UpdatedAt: base.Add(2 * time.Minute)}
	svc := &AdministrationService{Repository: repo}
	first, err := svc.ListPage(context.Background(), Query{Limit: 2}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "job-c" || first.Items[1].ID != "job-b" {
		t.Fatalf("unexpected first page: %#v", first)
	}
	second, err := svc.ListPage(context.Background(), Query{Limit: 2}, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "job-a" {
		t.Fatalf("unexpected second page: %#v", second)
	}
	if _, err := svc.ListPage(context.Background(), Query{Limit: 2}, "not-a-cursor"); !errors.Is(err, ErrInvalidJobCursor) {
		t.Fatalf("invalid cursor accepted: %v", err)
	}
}

func TestAdministrationGetDoesNotDependOnLatestListWindow(t *testing.T) {
	repo := NewMemoryRepository()
	now := time.Now().UTC()
	repo.items["old-job"] = Job{ID: "old-job", Type: "DISPATCH", Status: StatusCompleted, CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now}
	svc := &AdministrationService{Repository: repo}
	job, err := svc.Get(context.Background(), "old-job")
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "old-job" {
		t.Fatalf("wrong job returned: %#v", job)
	}
}
