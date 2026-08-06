package sender

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

type MemoryGovernanceStore struct {
	mu           sync.Mutex
	pools        map[string]Pool
	nodes        map[string]Node
	sessions     map[string]GovernedSession
	gatewayPools map[string]GatewayPool
	seq          int64
}

func NewMemoryGovernanceStore() *MemoryGovernanceStore {
	return &MemoryGovernanceStore{pools: map[string]Pool{}, nodes: map[string]Node{}, sessions: map[string]GovernedSession{}, gatewayPools: map[string]GatewayPool{}}
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
func (m *MemoryGovernanceStore) GetNode(_ context.Context, id string) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.nodes[id]
	if !ok {
		return Node{}, ErrSenderNotFound
	}
	return v, nil
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
	if v.InternalURL != "" {
		cur.InternalURL = v.InternalURL
	}
	if v.GatewayPoolID != "" {
		cur.GatewayPoolID = v.GatewayPoolID
	}
	if v.Provider != "" {
		cur.Provider = v.Provider
	}
	if v.Engine != "" {
		cur.Engine = v.Engine
	}
	if v.AdapterVersion != "" {
		cur.AdapterVersion = v.AdapterVersion
	}
	if v.BootID != "" {
		cur.BootID = v.BootID
	}
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

func (m *MemoryGovernanceStore) GetSession(_ context.Context, id string) (GovernedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.sessions[id]
	if !ok {
		return GovernedSession{}, ErrSenderNotFound
	}
	return v, nil
}

func (m *MemoryGovernanceStore) RegisterSession(_ context.Context, v GovernedSession, _ []byte, _, _ string) (GovernedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v.ID = m.next("session")
	v.Version = 1
	m.sessions[v.ID] = v
	return v, nil
}
func (m *MemoryGovernanceStore) TransitionSession(_ context.Context, id string, e int64, status Status, _, reason string) (GovernedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.sessions[id]
	if !ok {
		return GovernedSession{}, ErrSenderNotFound
	}
	if cur.Version != e {
		return GovernedSession{}, ErrSenderConflict
	}
	if !AllowedSessionTransition(cur.Status, status) {
		return GovernedSession{}, ErrSenderConflict
	}
	if status == StatusQuarantined && (cur.Status == StatusRetired || cur.Status == StatusQuarantined) {
		return GovernedSession{}, ErrSenderConflict
	}
	if status == StatusReady && cur.QuarantinedAt != nil && cur.Status != StatusQuarantined {
		return GovernedSession{}, ErrSenderConflict
	}
	cur.Status = status
	now := time.Now().UTC()
	if status == StatusQuarantined {
		cur.QuarantinedAt = &now
		cur.QuarantineReason = reason
		cur.ReinstatedAt = nil
	} else if status == StatusReady && cur.QuarantinedAt != nil {
		cur.ReinstatedAt = &now
		cur.QuarantineReason = ""
	}
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
	if cur.Status != StatusQuarantined && cur.Status != StatusRetired && cur.Status != StatusRestricted {
		cur.Status = v.Status
	}
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

func (m *MemoryGovernanceStore) ListGatewayPools(context.Context) ([]GatewayPool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]GatewayPool, 0, len(m.gatewayPools))
	for _, value := range m.gatewayPools {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *MemoryGovernanceStore) GetGatewayPool(_ context.Context, id string) (GatewayPool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.gatewayPools[id]
	if !ok {
		return GatewayPool{}, ErrSenderNotFound
	}
	return value, nil
}

func (m *MemoryGovernanceStore) CreateGatewayPool(_ context.Context, value GatewayPool, _, _ string) (GatewayPool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.gatewayPools {
		if existing.Name == value.Name {
			return GatewayPool{}, errors.New("gateway pool name already exists")
		}
	}
	now := time.Now().UTC()
	if value.ID == "" {
		value.ID = m.next("gateway")
	}
	value.Version = 1
	value.CreatedAt = now
	value.UpdatedAt = now
	m.gatewayPools[value.ID] = value
	return value, nil
}

func (m *MemoryGovernanceStore) UpdateGatewayPool(_ context.Context, id string, expectedVersion int64, value GatewayPool, _, _ string) (GatewayPool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.gatewayPools[id]
	if !ok {
		return GatewayPool{}, ErrSenderNotFound
	}
	if current.Version != expectedVersion {
		return GatewayPool{}, ErrSenderConflict
	}
	value.ID = id
	value.CreatedAt = current.CreatedAt
	value.UpdatedAt = time.Now().UTC()
	value.Version = expectedVersion + 1
	m.gatewayPools[id] = value
	return value, nil
}
