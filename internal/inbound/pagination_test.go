package inbound

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInboundReplyPageContinuesWithoutDuplicates(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	repo.byID["reply-a"] = Reply{ID: "reply-a", CreatedAt: base}
	repo.byID["reply-b"] = Reply{ID: "reply-b", CreatedAt: base.Add(time.Minute)}
	repo.byID["reply-c"] = Reply{ID: "reply-c", CreatedAt: base.Add(2 * time.Minute)}
	repo.order = []string{"reply-a", "reply-b", "reply-c"}
	service := &Service{Repository: repo}
	first, err := service.ListPage(context.Background(), 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "reply-c" || first.Items[1].ID != "reply-b" {
		t.Fatalf("unexpected first inbound page: %#v", first)
	}
	second, err := service.ListPage(context.Background(), 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "reply-a" {
		t.Fatalf("unexpected second inbound page: %#v", second)
	}
	if _, err := service.ListPage(context.Background(), 2, "invalid"); !errors.Is(err, ErrInvalidReplyCursor) {
		t.Fatalf("invalid reply cursor accepted: %v", err)
	}
}

func TestRotationRunPageContinuesWithoutDuplicates(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRotationRepository()
	repo.runs["run-a"] = RotationRun{ID: "run-a", RequestedAt: base}
	repo.runs["run-b"] = RotationRun{ID: "run-b", RequestedAt: base.Add(time.Minute)}
	repo.runs["run-c"] = RotationRun{ID: "run-c", RequestedAt: base.Add(2 * time.Minute)}
	service := &RotationService{Repository: repo, ActiveKeyVersion: "v2"}
	first, err := service.ListPage(context.Background(), 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "run-c" || first.Items[1].ID != "run-b" {
		t.Fatalf("unexpected first rotation page: %#v", first)
	}
	second, err := service.ListPage(context.Background(), 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "run-a" {
		t.Fatalf("unexpected second rotation page: %#v", second)
	}
	if _, err := service.ListPage(context.Background(), 2, "invalid"); !errors.Is(err, ErrInvalidRotationCursor) {
		t.Fatalf("invalid rotation cursor accepted: %v", err)
	}
}
