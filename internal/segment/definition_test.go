package segment

import (
	"context"
	"errors"
	"testing"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
)

func segmentTestRegistry(t *testing.T) *audiencefilter.Registry {
	t.Helper()
	r, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func segmentTestGroup() audiencefilter.Group {
	return audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "REPORTED_AGE", Operator: audiencefilter.OperatorBetween, Values: []any{18, 35}}}}
}
func TestDefinitionLifecycle(t *testing.T) {
	repo := NewMemoryDefinitionRepository()
	svc := &DefinitionService{Repository: repo, Registry: segmentTestRegistry(t), Clock: func() time.Time { return time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC) }}
	created, err := svc.Create(context.Background(), CreateDefinitionInput{OrganisationID: "org", Name: "Lagos adults", Definition: segmentTestGroup(), ActorID: "maker"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.Update(context.Background(), created.ID, UpdateDefinitionInput{Name: "Lagos adults 18-35", Definition: segmentTestGroup(), ExpectedVersion: 1, Reason: "refine label", ActorID: "maker"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 {
		t.Fatalf("unexpected version %+v", updated)
	}
	if _, err := svc.Update(context.Background(), created.ID, UpdateDefinitionInput{Name: "stale", Definition: segmentTestGroup(), ExpectedVersion: 1, Reason: "stale", ActorID: "maker"}, nil); !errors.Is(err, ErrDefinitionConflict) {
		t.Fatalf("expected conflict got %v", err)
	}
	archived, err := svc.Archive(context.Background(), created.ID, "checker", "retired", 2)
	if err != nil {
		t.Fatal(err)
	}
	if archived.Status != StatusArchived || archived.Version != 3 {
		t.Fatalf("unexpected archive %+v", archived)
	}
	versions, err := svc.Versions(context.Background(), created.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions got %d", len(versions))
	}
}
func TestDefinitionRejectsInvalidFilter(t *testing.T) {
	svc := &DefinitionService{Repository: NewMemoryDefinitionRepository(), Registry: segmentTestRegistry(t)}
	_, err := svc.Create(context.Background(), CreateDefinitionInput{OrganisationID: "org", Name: "bad", Definition: audiencefilter.Group{Join: audiencefilter.JoinAnd}, ActorID: "maker"}, nil)
	if err == nil {
		t.Fatal("invalid segment accepted")
	}
}

func TestDefinitionServiceCloneAndCompareVersions(t *testing.T) {
	registry, err := audiencefilter.NewRegistry(audiencefilter.Definition{
		Code: "COUNTRY", DisplayName: "Country", DataType: audiencefilter.DataTypeText,
		Operators: []audiencefilter.Operator{audiencefilter.OperatorEquals}, Filterable: true,
		Storage: audiencefilter.StorageCoreColumn, QueryableField: "country_id", Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	repository := NewMemoryDefinitionRepository()
	clock := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	service := &DefinitionService{Repository: repository, Registry: registry, Clock: func() time.Time { return clock }}
	created, err := service.Create(context.Background(), CreateDefinitionInput{
		OrganisationID: "org-1", Name: "Lagos audience", ActorID: "maker-1",
		Definition: audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorEquals, Values: []any{"NG"}}}},
	}, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Hour)
	updated, err := service.Update(context.Background(), created.ID, UpdateDefinitionInput{
		Name: "Ghana audience", ExpectedVersion: 1, Reason: "change target geography", ActorID: "maker-2",
		Definition: audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorEquals, Values: []any{"GH"}}}},
	}, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := service.CompareVersions(context.Background(), created.ID, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !comparison.NameChanged || len(comparison.ChangedRules) != 1 || comparison.Equivalent {
		t.Fatalf("unexpected comparison: %+v", comparison)
	}
	clone, err := service.Clone(context.Background(), updated.ID, CloneDefinitionInput{Name: "Ghana audience copy", Reason: "reuse for another campaign", ActorID: "maker-3"}, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if clone.ID == updated.ID || clone.OrganisationID != updated.OrganisationID || clone.Definition.Rules[0].Values[0] != "GH" {
		t.Fatalf("unexpected clone: %+v", clone)
	}
	clone.Definition.Rules[0].Values[0] = "NG"
	source, err := service.Get(context.Background(), updated.ID)
	if err != nil {
		t.Fatal(err)
	}
	if source.Definition.Rules[0].Values[0] != "GH" {
		t.Fatal("clone mutated source definition")
	}
}
