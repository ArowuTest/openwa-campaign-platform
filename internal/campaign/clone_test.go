package campaign

import (
	"context"
	"testing"
	"time"
)

func TestCloneCreatesFreshDraftWithoutApprovalEvidence(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo)
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	source, err := New(CreateInput{
		OrganisationID: "org-1", Name: "Original", PurposeID: "purpose-1", ConsentReviewID: "review-1",
		MaximumUniqueRecipients: 1000, MaximumMessagesPerRecipient: 1, CreatedBy: "maker-1",
		Transport: validTransport(), Timezone: "Africa/Lagos", QuietHoursStart: "22:00", QuietHoursEnd: "07:00",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	source.Status = StatusScheduled
	source.AudienceSnapshotID = "snapshot-1"
	source.MessageVersionID = "message-1"
	source.CommercialApprovalID = "commercial-1"
	source.FinalApprovedBy = "approver-1"
	if err := repo.Create(context.Background(), source); err != nil {
		t.Fatal(err)
	}

	clone, err := service.Clone(context.Background(), source.ID, CloneInput{Name: "Copy", ActorID: "operator-2"})
	if err != nil {
		t.Fatal(err)
	}
	if clone.ID == source.ID {
		t.Fatal("clone reused source ID")
	}
	if clone.Status != StatusDraft {
		t.Fatalf("status=%s", clone.Status)
	}
	if clone.AudienceSnapshotID != "" || clone.MessageVersionID != "" || clone.CommercialApprovalID != "" || clone.FinalApprovedBy != "" {
		t.Fatal("clone retained approval evidence")
	}
	if clone.CreatedBy != "operator-2" || clone.Name != "Copy" {
		t.Fatalf("unexpected clone: %+v", clone)
	}
	if clone.Transport.Engine != source.Transport.Engine || clone.ConsentReviewID != source.ConsentReviewID {
		t.Fatal("clone lost reusable configuration")
	}
}

func TestCloneAllowsFreshWindowAndEntitlement(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo)
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	source, err := New(CreateInput{OrganisationID: "org-1", Name: "Original", PurposeID: "purpose-1", ConsentReviewID: "review-1", MaximumUniqueRecipients: 1000, MaximumMessagesPerRecipient: 1, CreatedBy: "maker-1", Transport: validTransport()}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	start := now.Add(24 * time.Hour)
	deadline := start.Add(8 * time.Hour)
	clone, err := service.Clone(context.Background(), source.ID, CloneInput{ActorID: "operator-2", MaximumUniqueRecipients: 2500, RequestedStartAt: &start, CompletionDeadlineAt: &deadline, Timezone: "Europe/London"})
	if err != nil {
		t.Fatal(err)
	}
	if clone.MaximumUniqueRecipients != 2500 || clone.RequestedStartAt == nil || !clone.RequestedStartAt.Equal(start) || clone.Timezone != "Europe/London" {
		t.Fatalf("unexpected clone: %+v", clone)
	}
}
