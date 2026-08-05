package inbound

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetentionPolicyMakerCheckerAndActivation(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	seed := RetentionPolicy{ID: "seed", RetentionDays: 90, Status: RetentionPolicyActive, EffectiveFrom: now.Add(-time.Hour), Version: 1, CreatedBy: "system", Reason: "initial policy", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}
	store := NewMemoryRetentionPolicyStore(seed)
	admin := &RetentionPolicyAdministration{Store: store, Clock: func() time.Time { return now }}
	p, err := admin.CreateDraft(context.Background(), 30, now.Add(time.Hour), "maker", "reduce exposure")
	if err != nil {
		t.Fatal(err)
	}
	p, err = admin.Submit(context.Background(), p.ID, p.Version, "maker", "ready for review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Decide(context.Background(), p.ID, p.Version, true, "maker", "self approval"); !errors.Is(err, ErrRetentionPolicyInvalid) {
		t.Fatalf("expected maker checker error got %v", err)
	}
	p, err = admin.Decide(context.Background(), p.ID, p.Version, true, "checker", "approved policy")
	if err != nil {
		t.Fatal(err)
	}
	admin.Clock = func() time.Time { return now.Add(2 * time.Hour) }
	d, err := admin.ActiveDuration(context.Background())
	if err != nil || d != 30*24*time.Hour {
		t.Fatalf("duration=%v err=%v", d, err)
	}
}
