package sender

import (
	"context"
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
	resolved, err := svc.Resolve(ctx, []struct {
		Scope PacingScope
		ID    string
	}{{PacingPlatform, ""}, {PacingSession, "session-1"}}, clock)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Policy.ID != session.ID || resolved.Policy.MinimumDelayMS != 10000 {
		t.Fatalf("expected session override, got %#v", resolved)
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
