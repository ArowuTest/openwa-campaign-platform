package execution

import (
	"context"
	"sync"
	"time"
)

type MemoryStore struct {
	mu                sync.Mutex
	MetricsByCampaign map[string]Metrics
	MessagesPerMinute int
	DailyCapacity     int64
	Admissions        []CapacityEvidence
	Events            []map[string]any
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{MetricsByCampaign: map[string]Metrics{}, MessagesPerMinute: 60, DailyCapacity: 100000}
}
func (s *MemoryStore) RecordAdmission(_ context.Context, e CapacityEvidence) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Admissions = append(s.Admissions, e)
	return nil
}
func (s *MemoryStore) Metrics(_ context.Context, id string) (Metrics, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.MetricsByCampaign[id]
	if !ok {
		return Metrics{}, nil
	}
	return m, nil
}
func (s *MemoryStore) Capacity(context.Context, string, time.Time) (int, int64, error) {
	return s.MessagesPerMinute, s.DailyCapacity, nil
}
func (s *MemoryStore) RecordEvent(_ context.Context, cid, typ, actor, reason string, details map[string]any, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Events = append(s.Events, map[string]any{"campaignId": cid, "eventType": typ, "actor": actor, "reason": reason, "details": details, "createdAt": now})
	return nil
}
