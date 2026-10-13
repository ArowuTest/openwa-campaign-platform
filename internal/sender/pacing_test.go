package sender

import (
	"context"
	"errors"
	"testing"
	"time"
)

func basePacing(scope PacingScope, id string) PacingPolicy {
	return PacingPolicy{Scope: scope, ScopeID: id, Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", MinimumDelayMS: 3000, MaximumDelayMS: 8000, JitterMode: JitterUniform, MaxInFlight: 1, MessagesPerMinute: 12, HourlyAllowance: 500, DailyAllowance: 4000, MaxActiveCampaigns: 2, BurstSize: 1, CooldownSeconds: 60, RecoveryRampMinutes: 15, FailureThresholdBPS: 500, DisconnectThreshold: 3, AutoQuarantine: true, Overrides: []MessageTypeOverride{{MessageType: "VIDEO", MinimumDelayMS: 8000, MaximumDelayMS: 15000}}}
}
func TestPacingPolicyMakerCheckerAndResolution(t *testing.T) {
	ctx := context.Background()
	clock := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	svc := &PacingAdministration{Store: NewMemoryPacingStore(), Clock: func() time.Time { return clock }}
	platform, err := svc.CreateDraft(ctx, basePacing(PacingPlatform, ""), "maker", "platform defaults")
	if err != nil {
		t.Fatal(err)
	}
	platform, err = svc.Submit(ctx, platform.ID, platform.Version, "maker", "submit defaults")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Decide(ctx, platform.ID, platform.Version, true, "maker", "approve defaults"); err == nil {
		t.Fatal("maker must not approve")
	}
	platform, err = svc.Decide(ctx, platform.ID, platform.Version, true, "checker", "approve defaults")
	if err != nil {
		t.Fatal(err)
	}
	sessionDraft := basePacing(PacingSession, "session-1")
	sessionDraft.MinimumDelayMS = 10000
	sessionDraft.MaximumDelayMS = 20000
	session, err := svc.CreateDraft(ctx, sessionDraft, "maker", "session override")
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.Submit(ctx, session.ID, session.Version, "maker", "submit session")
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.Decide(ctx, session.ID, session.Version, true, "checker", "approve session")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.Resolve(ctx, []PacingScopeRef{{PacingPlatform, ""}, {PacingSession, "session-1"}}, clock)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Policy.ID != session.ID || resolved.Policy.MinimumDelayMS != 10000 {
		t.Fatalf("expected session override, got %#v", resolved)
	}
}
func TestPacingPolicyDefaultsToOneActiveCampaignWhenOmitted(t *testing.T) {
	ctx := context.Background()
	svc := &PacingAdministration{Store: NewMemoryPacingStore()}
	p := basePacing(PacingSession, "session-1")
	p.MaxActiveCampaigns = 0
	created, err := svc.CreateDraft(ctx, p, "maker", "use safe sender defaults")
	if err != nil {
		t.Fatal(err)
	}
	if created.MaxActiveCampaigns != 1 {
		t.Fatalf("maxActiveCampaigns=%d want=1", created.MaxActiveCampaigns)
	}
}

func TestPacingPolicyRejectsInvalidAllowances(t *testing.T) {
	p := basePacing(PacingSession, "s")
	p.HourlyAllowance = 5000
	p.DailyAllowance = 1000
	if err := validatePacing(&p); err == nil {
		t.Fatal("expected invalid allowance hierarchy")
	}
}
func TestPacingPolicyRejectsDuplicateMessageOverrides(t *testing.T) {
	p := basePacing(PacingSession, "s")
	p.Overrides = append(p.Overrides, p.Overrides[0])
	if err := validatePacing(&p); err == nil {
		t.Fatal("expected duplicate override rejection")
	}
}

func TestPacingPolicyCreatorCannotApproveAfterIndependentSubmission(t *testing.T) {
	ctx := context.Background()
	admin := &PacingAdministration{Store: NewMemoryPacingStore()}
	draft, err := admin.CreateDraft(ctx, basePacing(PacingPlatform, ""), "maker", "maker creates pacing policy")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := admin.Submit(ctx, draft.ID, draft.Version, "submitter", "submitter sends for review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Decide(ctx, pending.ID, pending.Version, true, "maker", "maker self approves"); !errors.Is(err, ErrPacingInvalid) {
		t.Fatalf("creator approved own pacing policy after another actor submitted it: %v", err)
	}
	stored, err := admin.Store.Get(ctx, pending.ID)
	if err != nil || stored.Status != PacingPending {
		t.Fatalf("rejected decision mutated policy: status=%s err=%v", stored.Status, err)
	}
}

func TestResolveStableWindowRejectsMoreSpecificPolicyOnlyInMiddle(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	store := NewMemoryPacingStore()

	platform := basePacing(PacingPlatform, "")
	platform.ID = "platform-stable"
	platform.Status = PacingActive
	platform.Version = 1
	platform.EffectiveFrom = start.Add(-time.Hour)
	platform.CreatedAt = start.Add(-2 * time.Hour)
	if _, err := store.Create(ctx, platform); err != nil {
		t.Fatal(err)
	}

	middle := basePacing(PacingCampaign, "campaign-1")
	middle.ID = "campaign-middle"
	middle.Status = PacingActive
	middle.Version = 1
	middle.MessagesPerMinute = 1
	middle.HourlyAllowance = 60
	middle.DailyAllowance = 60
	middle.EffectiveFrom = start.Add(30 * time.Minute)
	middleEnd := start.Add(60 * time.Minute)
	middle.EffectiveTo = &middleEnd
	middle.CreatedAt = start.Add(-time.Hour)
	if _, err := store.Create(ctx, middle); err != nil {
		t.Fatal(err)
	}

	admin := &PacingAdministration{Store: store}
	scopes := []PacingScopeRef{{Scope: PacingPlatform}, {Scope: PacingCampaign, ID: "campaign-1"}}
	atStart, err := admin.Resolve(ctx, scopes, start)
	if err != nil {
		t.Fatal(err)
	}
	atEnd, err := admin.Resolve(ctx, scopes, end.Add(-time.Nanosecond))
	if err != nil {
		t.Fatal(err)
	}
	if atStart.Policy.ID != platform.ID || atEnd.Policy.ID != platform.ID {
		t.Fatalf("fixture does not reproduce equal endpoint authority: start=%s end=%s", atStart.Policy.ID, atEnd.Policy.ID)
	}
	if _, err := admin.ResolveStableWindow(ctx, scopes, start, end); !errors.Is(err, ErrPacingWindowDrift) {
		t.Fatalf("mid-window pacing override was not rejected: %v", err)
	}
}

func TestResolveStableWindowReturnsOneAuthorityAcrossWholeWindow(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	store := NewMemoryPacingStore()
	policy := basePacing(PacingSenderPool, "pool-1")
	policy.ID = "pool-policy"
	policy.Status = PacingActive
	policy.Version = 4
	policy.EffectiveFrom = start.Add(-time.Hour)
	policy.CreatedAt = start.Add(-2 * time.Hour)
	if _, err := store.Create(ctx, policy); err != nil {
		t.Fatal(err)
	}
	admin := &PacingAdministration{Store: store}
	resolved, err := admin.ResolveStableWindow(ctx, []PacingScopeRef{{Scope: PacingSenderPool, ID: "pool-1"}}, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Policy.ID != policy.ID || resolved.Policy.Version != policy.Version {
		t.Fatalf("wrong stable authority: %+v", resolved.Policy)
	}
}
