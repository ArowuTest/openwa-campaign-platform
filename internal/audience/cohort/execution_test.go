package cohort

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
)

type fakeQueryRepository struct {
	count int64
	ids   []string
	err   error
	last  CompiledQuery
}

func (f *fakeQueryRepository) Count(_ context.Context, q CompiledQuery) (int64, error) {
	f.last = q
	return f.count, f.err
}
func (f *fakeQueryRepository) Members(_ context.Context, q CompiledQuery, limit int) ([]string, error) {
	f.last = q
	if f.err != nil {
		return nil, f.err
	}
	out := append([]string(nil), f.ids...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func testExecutionService(repo QueryRepository) *ExecutionService {
	registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		panic(err)
	}
	service := NewExecutionService(NewCompiler(registry), repo)
	service.Clock = func() time.Time { return time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC) }
	service.MaxMaterialisation = 2
	return service
}
func testGroup() audiencefilter.Group {
	return audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "REPORTED_AGE", Operator: audiencefilter.OperatorBetween, Values: []any{18, 35}}}}
}
func testEligibility() EligibilityContext {
	return EligibilityContext{OrganisationID: "org", PurposeID: "purpose", Channel: "WHATSAPP", AsOf: time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)}
}

type fakeBreakdownRepository struct {
	fakeQueryRepository
	breakdown      EligibilityBreakdown
	breakdownErr   error
	breakdownQuery CompiledQuery
}

func (f *fakeBreakdownRepository) CountBreakdown(_ context.Context, q CompiledQuery) (EligibilityBreakdown, error) {
	f.breakdownQuery = q
	return f.breakdown, f.breakdownErr
}

func TestExecutionEstimateReturnsAuthoritativeEligibilityBreakdown(t *testing.T) {
	repo := &fakeBreakdownRepository{breakdown: EligibilityBreakdown{
		MatchedProfiles:      100,
		ConsentEligible:      80,
		Unsuppressed:         70,
		Eligible:             60,
		ConsentExcluded:      20,
		SuppressionExcluded:  10,
		FrequencyCapExcluded: 10,
	}}
	got, err := testExecutionService(repo).Estimate(context.Background(), testGroup(), testEligibility(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.EligibleCount != 60 || got.Breakdown == nil {
		t.Fatalf("unexpected estimate %+v", got)
	}
	if got.Breakdown.MatchedProfiles != 100 || got.Breakdown.ConsentExcluded != 20 ||
		got.Breakdown.SuppressionExcluded != 10 || got.Breakdown.FrequencyCapExcluded != 10 {
		t.Fatalf("unexpected breakdown %+v", got.Breakdown)
	}
	if repo.breakdownQuery.SQL == "" || !strings.Contains(repo.breakdownQuery.SQL, "matched_profiles") {
		t.Fatalf("breakdown query was not used: %+v", repo.breakdownQuery)
	}
}

func TestExecutionEstimate(t *testing.T) {
	repo := &fakeQueryRepository{count: 42}
	got, err := testExecutionService(repo).Estimate(context.Background(), testGroup(), testEligibility(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.EligibleCount != 42 || !got.CalculatedAt.Equal(time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected estimate %+v", got)
	}
	if repo.last.SQL == "" || len(repo.last.Args) == 0 {
		t.Fatal("compiler output not passed")
	}
}
func TestExecutionMaterialiseHashesEvidence(t *testing.T) {
	repo := &fakeQueryRepository{ids: []string{"a", "b"}}
	got, err := testExecutionService(repo).Materialise(context.Background(), testGroup(), testEligibility(), nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got[0].EligibilityEvidenceHash) != 64 || got[0].EligibilityEvidenceHash == got[1].EligibilityEvidenceHash {
		t.Fatalf("unexpected members %+v", got)
	}
}
func TestExecutionMaterialiseRejectsOversize(t *testing.T) {
	repo := &fakeQueryRepository{ids: []string{"a", "b", "c"}}
	_, err := testExecutionService(repo).Materialise(context.Background(), testGroup(), testEligibility(), nil, 2)
	if !errors.Is(err, ErrCohortTooLarge) {
		t.Fatalf("expected too large, got %v", err)
	}
}
