package reconciliation

import (
	"context"
	"testing"
	"time"
)

type outcomeRepoStub struct {
	items       []OutcomeCandidate
	final       []string
	quarantined []string
}

func (r *outcomeRepoStub) ListStaleOutcomes(context.Context, time.Time, time.Time, int) ([]OutcomeCandidate, error) {
	return append([]OutcomeCandidate(nil), r.items...), nil
}
func (r *outcomeRepoStub) QuarantineStaleSubmitting(_ context.Context, id string, _, _ time.Time) error {
	r.quarantined = append(r.quarantined, id)
	return nil
}
func (r *outcomeRepoStub) MarkFinalUnknown(_ context.Context, id string, _, _ time.Time) error {
	r.final = append(r.final, id)
	return nil
}

func TestOutcomeQueryLimitHonoursConfiguredRangeAndRejectsOutOfRange(t *testing.T) {
	for _, tc := range []struct {
		input int
		want  int
		ok    bool
	}{
		{input: 1, want: 1, ok: true},
		{input: 1000, want: 1000, ok: true},
		{input: 1001, want: 1001, ok: true},
		{input: 5000, want: 5000, ok: true},
		{input: 0, ok: false},
		{input: 5001, ok: false},
	} {
		got, err := outcomeQueryLimit(tc.input)
		if tc.ok {
			if err != nil || got != tc.want {
				t.Fatalf("limit=%d got=%d err=%v want=%d", tc.input, got, err, tc.want)
			}
		} else if err == nil {
			t.Fatalf("limit=%d unexpectedly accepted as %d", tc.input, got)
		}
	}
}

func TestOutcomeWorkerFinalisesOnlyUnknownPastConfiguredWindow(t *testing.T) {
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	repo := &outcomeRepoStub{items: []OutcomeCandidate{{RecipientID: "stale-submitting", Status: "SUBMITTING", UpdatedAt: now.Add(-2 * time.Hour)}, {RecipientID: "recent-unknown", Status: "UNKNOWN", UpdatedAt: now.Add(-2 * time.Hour)}, {RecipientID: "old-unknown", Status: "UNKNOWN", UpdatedAt: now.Add(-25 * time.Hour)}, {RecipientID: "accepted", Status: "GATEWAY_ACCEPTED", UpdatedAt: now.Add(-30 * time.Hour)}}}
	w := &OutcomeWorker{Repository: repo, ReconciliationWindow: time.Hour, FinalUnknownWindow: 24 * time.Hour, BatchSize: 100, Clock: func() time.Time { return now }}
	n, err := w.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(repo.final) != 1 || repo.final[0] != "old-unknown" || len(repo.quarantined) != 1 || repo.quarantined[0] != "stale-submitting" {
		t.Fatalf("processed=%d final=%v quarantined=%v", n, repo.final, repo.quarantined)
	}
}
