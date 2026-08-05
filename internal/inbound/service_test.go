package inbound

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRecordReplayAndReview(t *testing.T) {
	repo := NewMemoryRepository()
	svc := &Service{Repository: repo, Clock: func() time.Time { return time.Date(2026, 8, 4, 20, 0, 0, 0, time.UTC) }}
	input := CreateInput{EventID: "evt-1", RecipientID: "r1", ContactID: "c1", CampaignID: "cmp1", SessionID: "s1", MessageText: "Please help"}
	first, created, err := svc.Record(context.Background(), input)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	replay, created, err := svc.Record(context.Background(), input)
	if err != nil || created || replay.ID != first.ID {
		t.Fatalf("replay=%#v created=%v err=%v", replay, created, err)
	}
	input.MessageText = "different"
	if _, _, err := svc.Record(context.Background(), input); !errors.Is(err, ErrReplayConflict) {
		t.Fatalf("expected replay conflict, got %v", err)
	}
	reviewed, err := svc.Review(context.Background(), first.ID, 1, ClassificationQuestion, true, "support follow-up", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.Version != 2 || reviewed.Classification != ClassificationQuestion || !reviewed.Escalated {
		t.Fatalf("unexpected reviewed reply %#v", reviewed)
	}
	if _, err := svc.Review(context.Background(), first.ID, 1, ClassificationOther, false, "", "user-1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestInboundReplyRetentionRedactsContentButPreservesEvidence(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	service := &Service{Repository: repo, Clock: func() time.Time { return now }, ContentRetention: time.Hour}
	created, _, err := service.Record(context.Background(), CreateInput{EventID: "evt-retain", RecipientID: "recipient", ContactID: "contact", CampaignID: "campaign", SessionID: "session", MessageText: "please call me", Classification: ClassificationQuestion})
	if err != nil {
		t.Fatal(err)
	}
	service.Clock = func() time.Time { return now.Add(2 * time.Hour) }
	count, err := service.RedactExpired(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	stored, err := service.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.MessageText != "" || stored.ContentRedactedAt == nil || stored.MessageFingerprint == "" {
		t.Fatalf("unexpected retained evidence: %#v", stored)
	}
}

type auditCapture struct{ records []AuditRecord }

func (a *auditCapture) RecordInboundAudit(_ context.Context, record AuditRecord) error {
	a.records = append(a.records, record)
	return nil
}

func TestRevealAndRetentionSweepProduceAuditEvidence(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	audit := &auditCapture{}
	service := &Service{Repository: repo, Audit: audit, Clock: func() time.Time { return now }, ContentRetention: time.Hour}
	created, _, err := service.Record(context.Background(), CreateInput{EventID: "evt-audit", RecipientID: "recipient", ContactID: "contact", CampaignID: "campaign", SessionID: "session", MessageText: "private reply"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Reveal(context.Background(), created.ID, "00000000-0000-4000-8000-000000000001", "request-reveal"); err != nil {
		t.Fatal(err)
	}
	service.Clock = func() time.Time { return now.Add(2 * time.Hour) }
	if count, err := service.RedactExpiredAudited(context.Background(), "00000000-0000-4000-8000-000000000001", "request-sweep"); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if len(audit.records) != 2 || audit.records[0].Action != "INBOUND_CONTENT_REVEALED" || audit.records[1].Action != "INBOUND_RETENTION_SWEEP" {
		t.Fatalf("unexpected audit records: %#v", audit.records)
	}
}

func TestLegalHoldPreventsRedactionAndRequiresVersionedRelease(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	audit := &auditCapture{}
	service := &Service{Repository: repo, Audit: audit, Clock: func() time.Time { return now }, ContentRetention: time.Hour}
	item, _, err := service.Record(context.Background(), CreateInput{EventID: "evt-hold", RecipientID: "recipient", ContactID: "contact", CampaignID: "campaign", SessionID: "session", MessageText: "hold me"})
	if err != nil {
		t.Fatal(err)
	}
	item, err = service.SetLegalHold(context.Background(), item.ID, item.Version, true, "regulatory investigation", "00000000-0000-4000-8000-000000000001", "req-hold")
	if err != nil {
		t.Fatal(err)
	}
	service.Clock = func() time.Time { return now.Add(2 * time.Hour) }
	if count, err := service.RedactExpired(context.Background()); err != nil || count != 0 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if _, err := service.SetLegalHold(context.Background(), item.ID, item.Version-1, false, "released", "00000000-0000-4000-8000-000000000001", "req-stale"); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	item, err = service.SetLegalHold(context.Background(), item.ID, item.Version, false, "investigation closed", "00000000-0000-4000-8000-000000000001", "req-release")
	if err != nil {
		t.Fatal(err)
	}
	if item.LegalHold || item.LegalHoldReleasedAt == nil {
		t.Fatalf("unexpected release state: %#v", item)
	}
	if count, err := service.RedactExpired(context.Background()); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestRecordUsesActiveGovernedRetentionPolicy(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	policies := &RetentionPolicyAdministration{Store: NewMemoryRetentionPolicyStore(RetentionPolicy{ID: "active", RetentionDays: 14, Status: RetentionPolicyActive, EffectiveFrom: now.Add(-time.Hour), Version: 1, CreatedBy: "system", Reason: "approved retention", CreatedAt: now, UpdatedAt: now}), Clock: func() time.Time { return now }}
	service := &Service{Repository: NewMemoryRepository(), Clock: func() time.Time { return now }, ContentRetention: 90 * 24 * time.Hour, RetentionPolicies: policies}
	item, _, err := service.Record(context.Background(), CreateInput{EventID: "evt-policy", RecipientID: "recipient", ContactID: "contact", CampaignID: "campaign", SessionID: "session", MessageText: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if got := item.ContentRetainUntil.Sub(now); got != 14*24*time.Hour {
		t.Fatalf("retention=%v", got)
	}
}
