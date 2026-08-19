package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/consent"
)

type racingOptOutBootstrapStore struct{ activeCalls, createCalls int }

func (s *racingOptOutBootstrapStore) Active(context.Context, time.Time) (consent.GovernedOptOutPolicy, error) {
	s.activeCalls++
	if s.activeCalls == 1 {
		return consent.GovernedOptOutPolicy{}, consent.ErrOptOutPolicyNotFound
	}
	return consent.GovernedOptOutPolicy{ID: "winner", Status: consent.OptOutPolicyActive}, nil
}

func (s *racingOptOutBootstrapStore) Create(_ context.Context, policy consent.GovernedOptOutPolicy) (consent.GovernedOptOutPolicy, error) {
	s.createCalls++
	return policy, errors.New("duplicate bootstrap opt-out policy")
}
func TestEnsureOptOutPolicyConvergesWhenAnotherReplicaWinsCreate(t *testing.T) {
	store := &racingOptOutBootstrapStore{}
	if err := ensureOptOutPolicy(context.Background(), store, []string{"STOP"}, time.Date(2026, 8, 16, 8, 30, 0, 0, time.UTC)); err != nil {
		t.Fatalf("concurrent opt-out bootstrap winner was not accepted: %v", err)
	}
	if store.activeCalls != 2 || store.createCalls != 1 {
		t.Fatalf("unexpected bootstrap calls active=%d create=%d", store.activeCalls, store.createCalls)
	}
}
