package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/inbound"
)

type failingRetentionBootstrapStore struct {
	activeErr error
	createErr error
	created   int
}

func (s *failingRetentionBootstrapStore) Active(context.Context, time.Time) (inbound.RetentionPolicy, error) {
	return inbound.RetentionPolicy{}, s.activeErr
}

func (s *failingRetentionBootstrapStore) Create(_ context.Context, policy inbound.RetentionPolicy) (inbound.RetentionPolicy, error) {
	s.created++
	return policy, s.createErr
}

func TestEnsureInboundRetentionPolicyPropagatesBootstrapCreateFailure(t *testing.T) {
	createErr := errors.New("retention bootstrap insert failed")
	store := &failingRetentionBootstrapStore{
		activeErr: inbound.ErrRetentionPolicyNotFound,
		createErr: createErr,
	}
	err := ensureInboundRetentionPolicy(context.Background(), store, 30, time.Date(2026, 8, 16, 7, 0, 0, 0, time.UTC))
	if !errors.Is(err, createErr) {
		t.Fatalf("bootstrap create error was swallowed: %v", err)
	}
	if store.created != 1 {
		t.Fatalf("bootstrap create calls=%d", store.created)
	}
}

type racingRetentionBootstrapStore struct{ activeCalls, createCalls int }

func (s *racingRetentionBootstrapStore) Active(context.Context, time.Time) (inbound.RetentionPolicy, error) {
	s.activeCalls++
	if s.activeCalls == 1 {
		return inbound.RetentionPolicy{}, inbound.ErrRetentionPolicyNotFound
	}
	return inbound.RetentionPolicy{ID: "winner", Status: inbound.RetentionPolicyActive}, nil
}
func (s *racingRetentionBootstrapStore) Create(_ context.Context, policy inbound.RetentionPolicy) (inbound.RetentionPolicy, error) {
	s.createCalls++
	return policy, errors.New("duplicate bootstrap retention policy")
}
func TestEnsureInboundRetentionPolicyConvergesWhenAnotherReplicaWinsCreate(t *testing.T) {
	store := &racingRetentionBootstrapStore{}
	if err := ensureInboundRetentionPolicy(context.Background(), store, 30, time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("concurrent bootstrap winner was not accepted: %v", err)
	}
	if store.activeCalls != 2 || store.createCalls != 1 {
		t.Fatalf("unexpected bootstrap calls active=%d create=%d", store.activeCalls, store.createCalls)
	}
}
