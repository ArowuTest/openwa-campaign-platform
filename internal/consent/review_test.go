package consent

import (
	"testing"
	"time"
)

func TestApprovalRequiresAllChecksAndExpiry(t *testing.T) {
	now := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	review, err := NewReview(CreateInput{
		OrganisationID: "org-1", Name: "Festival consent", Channel: "WHATSAPP",
		PrivacyNoticeReviewed: true, OptOutProcessReviewed: true, SampleRecordsReviewed: false,
	}, now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	expiry := now.Add(30 * 24 * time.Hour)
	if _, err := review.Decide(DecisionInput{ReviewerID: "reviewer", Decision: StatusApproved, ExpiresAt: &expiry, ExpectedVersion: review.Version}, now); err == nil {
		t.Fatal("expected approval to fail when mandatory checks are incomplete")
	}
}
