package main

import (
	"context"
	"errors"
	"fmt"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/platform/httpserver"
)

type filterRuntime struct {
	Registry       *audiencefilter.Registry
	Administration *audiencefilter.AdministrationService
	Compiler       *cohort.Compiler
	Readiness      httpserver.ReadinessCheck
}

// buildFilterRuntime constructs the query registry exclusively from the governed
// administration store. Production callers must pass the PostgreSQL store; using
// hard-coded defaults outside development would let the running compiler diverge
// from the approved configuration recorded with campaigns.
func buildFilterRuntime(ctx context.Context, store audiencefilter.AdministrationStore) (*filterRuntime, error) {
	if store == nil {
		return nil, errors.New("filter administration store is required")
	}
	registry, err := audiencefilter.NewRegistry()
	if err != nil {
		return nil, fmt.Errorf("create filter registry: %w", err)
	}
	administration := audiencefilter.NewAdministrationService(store, registry)
	if err := administration.Refresh(ctx); err != nil {
		return nil, fmt.Errorf("load governed filter definitions: %w", err)
	}
	return &filterRuntime{
		Registry:       registry,
		Administration: administration,
		Compiler:       cohort.NewCompiler(registry),
		Readiness: httpserver.ReadinessCheck{Name: "filterDefinitions", Check: func(checkCtx context.Context) error {
			return administration.Refresh(checkCtx)
		}},
	}, nil
}
