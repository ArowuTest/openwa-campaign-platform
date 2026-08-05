package main

import (
	"context"
	"errors"
	"testing"

	audiencefilter "campaign-platform/internal/audience/filter"
)

type failingFilterStore struct {
	audiencefilter.AdministrationStore
}

func (f failingFilterStore) Generation(context.Context) (int64, error) {
	return 0, errors.New("database unavailable")
}

func TestBuildFilterRuntimeLoadsGovernedDefinitions(t *testing.T) {
	store, err := audiencefilter.NewMemoryAdministrationStore(audiencefilter.DefaultDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := buildFilterRuntime(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Registry == nil || runtime.Administration == nil || runtime.Compiler == nil {
		t.Fatal("filter runtime was incomplete")
	}
	for _, code := range []string{"COUNTRY", "STATE", "LGA", "REPORTED_AGE", "GENDER"} {
		if _, ok := runtime.Registry.Get(code); !ok {
			t.Fatalf("mandatory definition %s was not loaded", code)
		}
	}
	if err := runtime.Readiness.Check(context.Background()); err != nil {
		t.Fatalf("readiness failed: %v", err)
	}
}

func TestBuildFilterRuntimeFailsClosedWhenStoreUnavailable(t *testing.T) {
	if _, err := buildFilterRuntime(context.Background(), failingFilterStore{}); err == nil {
		t.Fatal("expected unavailable governed store to block startup")
	}
}

func TestBuildFilterRuntimeRejectsMissingMandatoryDefinitions(t *testing.T) {
	items := audiencefilter.DefaultDefinitions()
	filtered := items[:0]
	for _, item := range items {
		if item.Code != "STATE" {
			filtered = append(filtered, item)
		}
	}
	store, err := audiencefilter.NewMemoryAdministrationStore(filtered)
	if err == nil {
		_, err = buildFilterRuntime(context.Background(), store)
	}
	if err == nil {
		t.Fatal("expected missing mandatory definition to block startup")
	}
}
