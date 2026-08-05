package message

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentDraftsReceiveDistinctVersions(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	service.clock = func() time.Time { return time.Date(2026, 8, 4, 7, 0, 0, 0, time.UTC) }
	var wg sync.WaitGroup
	versions := make(chan int, 2)
	for _, body := range []string{"message one", "message two"} {
		body := body
		wg.Add(1)
		go func() {
			defer wg.Done()
			created, err := service.CreateDraft(context.Background(), Input{CampaignID: "campaign-1", Type: TypeText, Body: body, CreatedBy: "maker", IdempotencyKey: "message-request-" + strings.ReplaceAll(body, " ", "-")})
			if err != nil {
				t.Errorf("create draft: %v", err)
				return
			}
			versions <- created.Version
		}()
	}
	wg.Wait()
	close(versions)
	seen := map[int]bool{}
	for version := range versions {
		seen[version] = true
	}
	if !seen[1] || !seen[2] || len(seen) != 2 {
		t.Fatalf("expected unique versions 1 and 2, got %#v", seen)
	}
}

func TestApprovalUsesExpectedHashAndMakerChecker(t *testing.T) {
	service := NewService(NewMemoryRepository())
	now := time.Date(2026, 8, 4, 7, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	draft, err := service.CreateDraft(context.Background(), Input{CampaignID: "campaign-1", Type: TypeText, Body: "hello", CreatedBy: "maker", IdempotencyKey: "message-request-hello"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(context.Background(), draft.ID, "checker", "wrong"); !errors.Is(err, ErrApprovalStale) {
		t.Fatalf("expected stale approval, got %v", err)
	}
	if _, err := service.Approve(context.Background(), draft.ID, "maker", draft.ContentHash); err == nil {
		t.Fatal("expected maker-checker rejection")
	}
	approved, err := service.Approve(context.Background(), draft.ID, "checker", draft.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != StatusApproved || approved.ApprovedBy != "checker" {
		t.Fatalf("unexpected approval: %#v", approved)
	}
}

func TestDuplicateContentRejected(t *testing.T) {
	service := NewService(NewMemoryRepository())
	input := Input{CampaignID: "campaign-1", Type: TypeText, Body: "same", CreatedBy: "maker", IdempotencyKey: "message-request-same"}
	if _, err := service.CreateDraft(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	input.IdempotencyKey = "message-request-same-second"
	if _, err := service.CreateDraft(context.Background(), input); !errors.Is(err, ErrDuplicateContent) {
		t.Fatalf("expected duplicate content error, got %v", err)
	}
}

func TestCreateDraftIdempotentReplayReturnsOriginal(t *testing.T) {
	service := NewService(NewMemoryRepository())
	input := Input{CampaignID: "campaign-1", Type: TypeText, Body: "same request", CreatedBy: "maker", IdempotencyKey: "message-request-replay-001"}
	first, err := service.CreateDraft(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateDraft(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Version != second.Version || first.ContentHash != second.ContentHash {
		t.Fatalf("idempotent replay created different resource: first=%+v second=%+v", first, second)
	}
}

func TestCreateDraftRejectsIdempotencyKeyPayloadMismatch(t *testing.T) {
	service := NewService(NewMemoryRepository())
	input := Input{CampaignID: "campaign-1", Type: TypeText, Body: "original", CreatedBy: "maker", IdempotencyKey: "message-request-conflict-001"}
	if _, err := service.CreateDraft(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	input.Body = "different"
	if _, err := service.CreateDraft(context.Background(), input); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestApprovalReplayIsIdempotentForSameApprover(t *testing.T) {
	service := NewService(NewMemoryRepository())
	draft, err := service.CreateDraft(context.Background(), Input{CampaignID: "campaign-1", Type: TypeText, Body: "approve once", CreatedBy: "maker", IdempotencyKey: "message-request-approve-001"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Approve(context.Background(), draft.ID, "checker", draft.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Approve(context.Background(), draft.ID, "checker", draft.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	if first.ApprovedAt == nil || second.ApprovedAt == nil || !first.ApprovedAt.Equal(*second.ApprovedAt) {
		t.Fatalf("approval replay changed evidence: first=%+v second=%+v", first, second)
	}
}
