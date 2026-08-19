package consent

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGovernedOptOutPolicyMakerCheckerAndActivation(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	seed := GovernedOptOutPolicy{ID: "seed", Keywords: []string{"STOP"}, Status: OptOutPolicyActive, EffectiveFrom: now.Add(-time.Hour), Version: 1, CreatedBy: "system", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}
	svc := &OptOutPolicyAdministration{Store: NewMemoryOptOutPolicyStore(seed), Clock: func() time.Time { return now }}
	draft, err := svc.CreateDraft(context.Background(), []string{" stop ", "QUIT"}, now.Add(time.Hour), "maker", "new language policy")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := svc.Submit(context.Background(), draft.ID, draft.Version, "maker", "submit for review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Decide(context.Background(), pending.ID, pending.Version, true, "maker", "self approve"); !errors.Is(err, ErrOptOutPolicyInvalid) {
		t.Fatalf("expected maker checker failure: %v", err)
	}
	active, err := svc.Decide(context.Background(), pending.ID, pending.Version, true, "checker", "approved policy")
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != OptOutPolicyActive {
		t.Fatalf("status %s", active.Status)
	}
}

func TestOptOutPolicyCreatorCannotApproveAfterIndependentSubmission(t *testing.T) {
	ctx := context.Background()
	admin := &OptOutPolicyAdministration{Store: NewMemoryOptOutPolicyStore(GovernedOptOutPolicy{})}
	draft, err := admin.CreateDraft(ctx, []string{"STOP"}, time.Now().Add(time.Hour), "maker", "maker creates policy")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := admin.Submit(ctx, draft.ID, draft.Version, "submitter", "submitter sends for review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Decide(ctx, pending.ID, pending.Version, true, "maker", "maker self approves"); !errors.Is(err, ErrOptOutPolicyInvalid) {
		t.Fatalf("creator approved own opt-out policy after another actor submitted it: %v", err)
	}
	stored, err := admin.Store.Get(ctx, pending.ID)
	if err != nil || stored.Status != OptOutPolicyPending {
		t.Fatalf("rejected decision mutated policy: status=%s err=%v", stored.Status, err)
	}
}
