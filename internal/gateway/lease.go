package gateway

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrLeaseHeld = errors.New("session lease is held by another worker")
	ErrLeaseLost = errors.New("session lease is no longer owned")
)

type Lease struct {
	SessionID string    `json:"sessionId"`
	WorkerID  string    `json:"workerId"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	Version   int64     `json:"version"`
}

type LeaseStore interface {
	Acquire(context.Context, string, string, string, time.Time, time.Duration) (Lease, error)
	Renew(context.Context, Lease, time.Time, time.Duration) (Lease, error)
	Release(context.Context, Lease) error
	Validate(context.Context, Lease, time.Time) error
	Get(context.Context, string) (Lease, bool, error)
}

type MemoryLeaseStore struct {
	mu     sync.Mutex
	leases map[string]Lease
}

func NewMemoryLeaseStore() *MemoryLeaseStore {
	return &MemoryLeaseStore{leases: make(map[string]Lease)}
}

func (s *MemoryLeaseStore) Acquire(_ context.Context, sessionID, workerID, token string, now time.Time, ttl time.Duration) (Lease, error) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(workerID) == "" || strings.TrimSpace(token) == "" || ttl <= 0 {
		return Lease{}, errors.New("session, worker, token and positive TTL are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.leases[sessionID]
	if exists && current.ExpiresAt.After(now) && (current.WorkerID != workerID || current.Token != token) {
		return Lease{}, ErrLeaseHeld
	}
	version := int64(1)
	if exists {
		version = current.Version + 1
	}
	lease := Lease{SessionID: sessionID, WorkerID: workerID, Token: token, ExpiresAt: now.UTC().Add(ttl), Version: version}
	s.leases[sessionID] = lease
	return lease, nil
}
func (s *MemoryLeaseStore) Renew(_ context.Context, lease Lease, now time.Time, ttl time.Duration) (Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.leases[lease.SessionID]
	if !ok || current.WorkerID != lease.WorkerID || current.Token != lease.Token || current.Version != lease.Version || !current.ExpiresAt.After(now) {
		return Lease{}, ErrLeaseLost
	}
	current.ExpiresAt = now.UTC().Add(ttl)
	current.Version++
	s.leases[lease.SessionID] = current
	return current, nil
}
func (s *MemoryLeaseStore) Release(_ context.Context, lease Lease) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.leases[lease.SessionID]
	if !ok {
		return nil
	}
	if current.WorkerID != lease.WorkerID || current.Token != lease.Token || current.Version != lease.Version {
		return ErrLeaseLost
	}
	delete(s.leases, lease.SessionID)
	return nil
}
func (s *MemoryLeaseStore) Validate(_ context.Context, lease Lease, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.leases[lease.SessionID]
	if !ok || current.WorkerID != lease.WorkerID || current.Token != lease.Token || current.Version != lease.Version || !current.ExpiresAt.After(now) {
		return ErrLeaseLost
	}
	return nil
}
func (s *MemoryLeaseStore) Get(_ context.Context, sessionID string) (Lease, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lease, ok := s.leases[sessionID]
	return lease, ok, nil
}
