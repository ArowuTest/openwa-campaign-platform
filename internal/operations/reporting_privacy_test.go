package operations

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReportingPrivacySuppressesSmallCellsAndFreezesPolicyEvidence(t *testing.T) {
	now := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	report := CampaignReport{RawBreakdowns: map[string]map[string]int64{
		"state":  {"Lagos": 100, "Ekiti": 4},
		"gender": {"Female": 55, "Not stated": 3},
	}}
	policy := ReportingPrivacyPolicy{ID: "policy-1", Version: 7, MinimumCohortSize: 10, SuppressionLabel: "SMALL_CELL", ApplyGeography: true, ApplyDemographics: true, ApplyAttributes: true}
	applyCampaignReportingPrivacy(&report, policy, now)
	if report.RawBreakdowns != nil {
		t.Fatal("raw breakdowns must not leave the service boundary")
	}
	if report.Privacy.PolicyID != "policy-1" || report.Privacy.PolicyVersion != 7 || report.Privacy.SuppressedCellCount != 2 {
		t.Fatalf("unexpected privacy evidence: %+v", report.Privacy)
	}
	state := report.Breakdowns["state"]
	if len(state) != 2 || state[0].Label != "Ekiti" || !state[0].Suppressed || state[0].Count != nil || state[1].Count == nil || *state[1].Count != 100 {
		t.Fatalf("unexpected protected state cells: %+v", state)
	}
}

func TestFutureReportingPrivacyPolicyPreservesCurrentPolicyUntilBoundary(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	store := NewMemoryReportingPrivacyStore()
	admin := &ReportingPrivacyAdministration{Store: store, Clock: func() time.Time { return now }}
	current, err := admin.Create(ctx, ReportingPrivacyPolicy{MinimumCohortSize: 10, ApplyGeography: true, ApplyDemographics: true, ApplyAttributes: true, EffectiveFrom: now.Add(-24 * time.Hour)}, "maker-current", "create current policy", "req-1")
	if err != nil {
		t.Fatal(err)
	}
	current, err = admin.Submit(ctx, current.ID, current.Version, "submitter-current", "submit current policy", "req-2")
	if err != nil {
		t.Fatal(err)
	}
	current, err = admin.Decide(ctx, current.ID, current.Version, true, "checker-current", "approve current policy", "req-3")
	if err != nil {
		t.Fatal(err)
	}

	futureStart := now.Add(48 * time.Hour)
	future, err := admin.Create(ctx, ReportingPrivacyPolicy{MinimumCohortSize: 25, ApplyGeography: true, ApplyDemographics: true, ApplyAttributes: true, EffectiveFrom: futureStart}, "maker-future", "create future policy", "req-4")
	if err != nil {
		t.Fatal(err)
	}
	future, err = admin.Submit(ctx, future.ID, future.Version, "submitter-future", "submit future policy", "req-5")
	if err != nil {
		t.Fatal(err)
	}
	future, err = admin.Decide(ctx, future.ID, future.Version, true, "checker-future", "approve future policy", "req-6")
	if err != nil {
		t.Fatal(err)
	}

	resolvedNow, err := admin.Resolve(ctx, "", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if resolvedNow.ID != current.ID || resolvedNow.MinimumCohortSize != 10 {
		t.Fatalf("current policy was retired too early: %+v", resolvedNow)
	}
	resolvedFuture, err := admin.Resolve(ctx, "", futureStart.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if resolvedFuture.ID != future.ID || resolvedFuture.MinimumCohortSize != 25 {
		t.Fatalf("future policy did not take effect: %+v", resolvedFuture)
	}
}

func TestReportingPrivacyCreatorCannotApproveAfterIndependentSubmission(t *testing.T) {
	ctx := context.Background()
	admin := &ReportingPrivacyAdministration{Store: NewMemoryReportingPrivacyStore()}
	draft, err := admin.Create(ctx, ReportingPrivacyPolicy{
		MinimumCohortSize: 10, ApplyGeography: true, ApplyDemographics: true, ApplyAttributes: true,
	}, "maker", "maker creates privacy policy", "request-create")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := admin.Submit(ctx, draft.ID, draft.Version, "submitter", "submitter sends policy", "request-submit")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Decide(ctx, pending.ID, pending.Version, true, "maker", "maker self approves policy", "request-decide"); !errors.Is(err, ErrConflict) {
		t.Fatalf("creator approved own reporting-privacy policy after another actor submitted it: %v", err)
	}
	stored, err := admin.Store.Get(ctx, pending.ID)
	if err != nil || stored.Status != ReportingPrivacyPending {
		t.Fatalf("rejected decision mutated policy: status=%s err=%v", stored.Status, err)
	}
}
