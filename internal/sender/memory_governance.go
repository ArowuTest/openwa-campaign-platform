package sender

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

type MemoryGovernanceStore struct {
	mu       sync.Mutex
	pools    map[string]Pool
	nodes    map[string]Node
	sessions map[string]GovernedSession
	seq      int64
}

func NewMemoryGovernanceStore() *MemoryGovernanceStore {
	return &MemoryGovernanceStore{pools: map[string]Pool{}, nodes: map[string]Node{}, sessions: map[string]GovernedSession{}}
}
func (m *MemoryGovernanceStore) next(prefix string) string {
	m.seq++
	return prefix + "-" + time.Now().UTC().Format("20060102150405") + "-" + string(rune('a'+m.seq%26))
}
func (m *MemoryGovernanceStore) ListPools(context.Context) ([]Pool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Pool, 0, len(m.pools))
	for _, v := range m.pools {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (m *MemoryGovernanceStore) CreatePool(_ context.Context, v Pool, _, _ string) (Pool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pools {
		if p.Name == v.Name {
			return Pool{}, errors.New("sender pool name already exists")
		}
	}
	now := time.Now().UTC()
	v.ID = m.next("pool")
	v.Version = 1
	v.CreatedAt = now
	v.UpdatedAt = now
	m.pools[v.ID] = v
	return v, nil
}
func (m *MemoryGovernanceStore) UpdatePool(_ context.Context, id string, e int64, v Pool, _, _ string) (Pool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.pools[id]
	if !ok {
		return Pool{}, ErrSenderNotFound
	}
	if cur.Version != e {
		return Pool{}, ErrSenderConflict
	}
	v.ID = id
	v.CreatedAt = cur.CreatedAt
	v.Version = e + 1
	v.UpdatedAt = time.Now().UTC()
	m.pools[id] = v
	return v, nil
}
func (m *MemoryGovernanceStore) ListNodes(context.Context) ([]Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Node, 0, len(m.nodes))
	for _, v := range m.nodes {
		out = append(out, v)
	}
	return out, nil
}
func (m *MemoryGovernanceStore) RegisterNode(_ context.Context, v Node, _, _ string) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v.ID = m.next("node")
	v.Version = 1
	m.nodes[v.ID] = v
	return v, nil
}
func (m *MemoryGovernanceStore) HeartbeatNode(_ context.Context, id string, e int64, v Node, now time.Time) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.nodes[id]
	if !ok {
		return Node{}, ErrSenderNotFound
	}
	if cur.Version != e {
		return Node{}, ErrSenderConflict
	}
	cur.Status = v.Status
	cur.BuildVersion = v.BuildVersion
	cur.Capacity = v.Capacity
	cur.QueueDepth = v.QueueDepth
	cur.Draining = v.Draining
	cur.LastHeartbeatAt = &now
	cur.Version++
	m.nodes[id] = cur
	return cur, nil
}
func (m *MemoryGovernanceStore) ListSessions(context.Context) ([]GovernedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]GovernedSession, 0, len(m.sessions))
	for _, v := range m.sessions {
		out = append(out, v)
	}
	return out, nil
}
func (m *MemoryGovernanceStore) RegisterSession(_ context.Context, v GovernedSession, _ []byte, _, _ string) (GovernedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v.ID = m.next("session")
	v.Version = 1
	m.sessions[v.ID] = v
	return v, nil
}
func (m *MemoryGovernanceStore) TransitionSession(_ context.Context, id string, e int64, status Status, _, _ string) (GovernedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.sessions[id]
	if !ok {
		return GovernedSession{}, ErrSenderNotFound
	}
	if cur.Version != e {
		return GovernedSession{}, ErrSenderConflict
	}
	cur.Status = status
	cur.Version++
	m.sessions[id] = cur
	return cur, nil
}
func (m *MemoryGovernanceStore) HeartbeatSession(_ context.Context, id string, e int64, v GovernedSession, now time.Time) (GovernedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.sessions[id]
	if !ok {
		return GovernedSession{}, ErrSenderNotFound
	}
	if cur.Version != e {
		return GovernedSession{}, ErrSenderConflict
	}
	cur.Status = v.Status
	cur.EngineVersion = v.EngineVersion
	cur.SafeMessagesPerMinute = v.SafeMessagesPerMinute
	cur.SafeDailyCapacity = v.SafeDailyCapacity
	cur.InFlightLimit = v.InFlightLimit
	cur.SentToday = v.SentToday
	cur.LastHeartbeatAt = &now
	cur.Version++
	m.sessions[id] = cur
	return cur, nil
}
func (m *MemoryGovernanceStore) Capacity(_ context.Context, pool string, now time.Time) (CapacitySummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pools[pool]
	if !ok {
		return CapacitySummary{}, ErrSenderNotFound
	}
	r := CapacitySummary{PoolID: pool, ReservedCapacity: p.ReservedCapacity, AsAt: now}
	for _, s := range m.sessions {
		if s.PoolID != pool || (s.Status != StatusReady && s.Status != StatusBusy) || s.LastHeartbeatAt == nil || now.Sub(*s.LastHeartbeatAt) > 90*time.Second {
			continue
		}
		r.ReadySessions++
		r.ConfiguredMessagesPerMinute += s.SafeMessagesPerMinute
		r.AvailableMessagesPerMinute += s.SafeMessagesPerMinute
		r.ConfiguredDailyCapacity += s.SafeDailyCapacity
		if s.SafeDailyCapacity > s.SentToday {
			r.RemainingDailyCapacity += s.SafeDailyCapacity - s.SentToday
		}
	}
	r.AvailableDailyCapacity = r.RemainingDailyCapacity - r.ReservedCapacity
	if r.AvailableDailyCapacity < 0 {
		r.AvailableDailyCapacity = 0
	}
	return r, nil
}
