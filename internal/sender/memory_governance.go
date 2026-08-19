package sender

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

type MemoryGovernanceStore struct {
	mu                sync.Mutex
	pools             map[string]Pool
	nodes             map[string]Node
	sessions          map[string]GovernedSession
	sessionProxies    map[string][]byte
	gatewayPools      map[string]GatewayPool
	runtimeNonces     map[string]time.Time
	runtimeEvents     map[string][]RuntimeEvent
	gatewayPoolEvents map[string][]GatewayPoolEvent
	seq               int64
}

func NewMemoryGovernanceStore() *MemoryGovernanceStore {
	return &MemoryGovernanceStore{pools: map[string]Pool{}, nodes: map[string]Node{}, sessions: map[string]GovernedSession{}, sessionProxies: map[string][]byte{}, gatewayPools: map[string]GatewayPool{}, runtimeNonces: map[string]time.Time{}, runtimeEvents: map[string][]RuntimeEvent{}, gatewayPoolEvents: map[string][]GatewayPoolEvent{}}
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
func (m *MemoryGovernanceStore) ListPoolPage(_ context.Context, limit int, afterName, afterID string) ([]Pool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Pool, 0, len(m.pools))
	for _, v := range m.pools {
		if afterName != "" && !(v.Name > afterName || (v.Name == afterName && v.ID > afterID)) {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
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
func (m *MemoryGovernanceStore) ListNodePage(_ context.Context, limit int, afterName, afterID string) ([]Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Node, 0, len(m.nodes))
	for _, v := range m.nodes {
		if afterName != "" && !(v.Name > afterName || (v.Name == afterName && v.ID > afterID)) {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
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
func (m *MemoryGovernanceStore) TransitionNode(_ context.Context, id string, expected int64, status, _, _ string) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.nodes[id]
	if !ok {
		return Node{}, ErrSenderNotFound
	}
	if current.Version != expected {
		return Node{}, ErrSenderConflict
	}
	if status == "RETIRED" {
		for _, session := range m.sessions {
			if session.NodeID == id && session.Status != StatusRetired {
				return Node{}, errors.New("gateway node has non-retired sessions")
			}
		}
	}
	current.Status = status
	current.Draining = status == "DRAINING" || status == "RETIRED"
	current.Version++
	m.nodes[id] = current
	return current, nil
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

func (m *MemoryGovernanceStore) ListSessionPage(_ context.Context, limit int, afterMaskedMSISDN, afterID string) ([]GovernedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	values := make([]GovernedSession, 0, len(m.sessions))
	for _, value := range m.sessions {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].MaskedMSISDN == values[j].MaskedMSISDN {
			return values[i].ID < values[j].ID
		}
		return values[i].MaskedMSISDN < values[j].MaskedMSISDN
	})
	out := make([]GovernedSession, 0, limit)
	for _, value := range values {
		if afterMaskedMSISDN != "" && (value.MaskedMSISDN < afterMaskedMSISDN || value.MaskedMSISDN == afterMaskedMSISDN && value.ID <= afterID) {
			continue
		}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
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
func (m *MemoryGovernanceStore) UpdateSessionMetadata(_ context.Context, id string, expected int64, value SessionOperationalMetadata, _, _ string) (GovernedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.sessions[id]
	if !ok {
		return GovernedSession{}, ErrSenderNotFound
	}
	if current.Version != expected {
		return GovernedSession{}, ErrSenderConflict
	}
	current.OwnerReference = value.OwnerReference
	current.RegistrationCountryISO2 = value.RegistrationCountryISO2
	current.ProfileDisplayName = value.ProfileDisplayName
	current.RecoveryReference = value.RecoveryReference
	current.RecoveryReferenceConfigured = value.RecoveryReference != ""
	current.Version++
	m.sessions[id] = current
	return current, nil
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
	if cur.Status != StatusQuarantined && cur.Status != StatusRetired && cur.Status != StatusRestricted && AllowedSessionTransition(cur.Status, v.Status) {
		cur.Status = v.Status
	}
	cur.EngineVersion = v.EngineVersion
	sameDay := false
	if cur.LastHeartbeatAt != nil {
		previous := cur.LastHeartbeatAt.UTC()
		current := now.UTC()
		sameDay = previous.Year() == current.Year() && previous.YearDay() == current.YearDay()
	}
	if !sameDay {
		cur.SentToday = v.SentToday
	} else if v.SentToday > cur.SentToday {
		cur.SentToday = v.SentToday
	}
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
func (m *MemoryGovernanceStore) ListGatewayPoolPage(_ context.Context, limit int, afterName, afterID string) ([]GatewayPool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]GatewayPool, 0, len(m.gatewayPools))
	for _, v := range m.gatewayPools {
		if afterName != "" && !(v.Name > afterName || (v.Name == afterName && v.ID > afterID)) {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
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

func (m *MemoryGovernanceStore) CreateGatewayPool(_ context.Context, value GatewayPool, actor, reason string) (GatewayPool, error) {
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
	if value.CreatedBy == "" {
		value.CreatedBy = actor
	}
	if value.Status == GatewayPoolActive && value.EffectiveFrom == nil {
		effective := now
		value.EffectiveFrom = &effective
	}
	m.gatewayPools[value.ID] = value
	m.gatewayPoolEvents[value.ID] = append(m.gatewayPoolEvents[value.ID], GatewayPoolEvent{ID: m.next("gateway-event"), PoolID: value.ID, EventType: "CREATED", Version: 1, ActorID: actor, Reason: reason, Evidence: map[string]any{"status": value.Status, "provider": value.Provider, "engine": value.Engine}, OccurredAt: now})
	return value, nil
}

func (m *MemoryGovernanceStore) UpdateGatewayPool(_ context.Context, id string, expectedVersion int64, value GatewayPool, actor, reason string) (GatewayPool, error) {
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
	m.gatewayPoolEvents[id] = append(m.gatewayPoolEvents[id], GatewayPoolEvent{ID: m.next("gateway-event"), PoolID: id, EventType: "STATUS_" + string(value.Status), Version: value.Version, ActorID: actor, Reason: reason, Evidence: map[string]any{"status": value.Status, "adapterVersion": value.AdapterVersion}, OccurredAt: value.UpdatedAt})
	return value, nil
}

func (m *MemoryGovernanceStore) ListGatewayPoolEvents(_ context.Context, poolID string, limit int) ([]GatewayPoolEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.gatewayPools[poolID]; !ok {
		return nil, ErrSenderNotFound
	}
	values := m.gatewayPoolEvents[poolID]
	if limit <= 0 || limit > len(values) {
		limit = len(values)
	}
	out := make([]GatewayPoolEvent, 0, limit)
	for index := len(values) - 1; index >= len(values)-limit; index-- {
		out = append(out, values[index])
	}
	return out, nil
}

func (m *MemoryGovernanceStore) ListGatewayPoolEventPage(_ context.Context, poolID string, limit int, before *time.Time, beforeID string) ([]GatewayPoolEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.gatewayPools[poolID]; !ok {
		return nil, ErrSenderNotFound
	}
	values := append([]GatewayPoolEvent(nil), m.gatewayPoolEvents[poolID]...)
	sort.Slice(values, func(i, j int) bool {
		if values[i].OccurredAt.Equal(values[j].OccurredAt) {
			return values[i].ID > values[j].ID
		}
		return values[i].OccurredAt.After(values[j].OccurredAt)
	})
	out := make([]GatewayPoolEvent, 0, limit)
	for _, value := range values {
		if before != nil && (value.OccurredAt.After(*before) || value.OccurredAt.Equal(*before) && value.ID >= beforeID) {
			continue
		}
		out = append(out, value)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (m *MemoryGovernanceStore) UseRuntimeNonce(_ context.Context, nodeID, nonce, _ string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	for key, expiry := range m.runtimeNonces {
		if !expiry.After(now) {
			delete(m.runtimeNonces, key)
		}
	}
	key := nodeID + ":" + nonce
	if _, exists := m.runtimeNonces[key]; exists {
		return ErrRuntimeReplay
	}
	m.runtimeNonces[key] = expiresAt
	return nil
}

func cloneRuntimeEvent(value RuntimeEvent) RuntimeEvent {
	if value.RuntimeIdentity != nil {
		copyValue := map[string]any{}
		for key, item := range value.RuntimeIdentity {
			copyValue[key] = item
		}
		value.RuntimeIdentity = copyValue
	}
	return value
}

func (m *MemoryGovernanceStore) ApplyRuntimeReport(_ context.Context, nodeID string, expected int64, report RuntimeReport, requestHash string, now time.Time) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.nodes[nodeID]
	if !ok {
		return Node{}, ErrSenderNotFound
	}
	if current.Version != expected {
		return Node{}, ErrSenderConflict
	}
	previousBootID := current.BootID
	wasRegistered := current.RegisteredAt != nil
	current.GatewayPoolID = report.GatewayPoolID
	current.Provider = report.Provider
	current.Engine = report.Engine
	current.AdapterVersion = report.AdapterVersion
	current.GatewayVersion = report.GatewayVersion
	current.WorkerVersion = report.WorkerVersion
	current.ConfigurationVersion = report.ConfigurationVersion
	current.BootID = report.BootID
	current.InternalURL = report.InternalURL
	current.RuntimeCapabilities = append([]Capability(nil), report.Capabilities...)
	current.RuntimeState = report.RuntimeState
	current.Capacity = report.Capacity
	current.SessionCount = report.SessionCount
	current.QueueDepth = report.QueueDepth
	current.CPUPercent = report.CPUPercent
	current.MemoryBytes = report.MemoryBytes
	current.ResourceHealth = report.ResourceHealth
	current.Draining = report.RuntimeState == RuntimeDraining
	status := "READY"
	switch report.RuntimeState {
	case RuntimeDraining:
		status = "DRAINING"
	case RuntimeDegraded, RuntimeUnavailable:
		status = "UNHEALTHY"
	}
	current.Status = status
	current.LastHeartbeatAt = &now
	if current.RegisteredAt == nil || previousBootID != report.BootID {
		registered := now
		current.RegisteredAt = &registered
	}
	m.nodes[nodeID] = current
	eventType := "HEARTBEAT"
	if !wasRegistered || previousBootID != report.BootID {
		eventType = "REGISTERED"
	}
	m.seq++
	event := RuntimeEvent{ID: m.next("runtime"), NodeID: nodeID, GatewayPoolID: report.GatewayPoolID, EventType: eventType, NodeVersion: current.Version, BootID: report.BootID, RuntimeIdentity: map[string]any{"provider": report.Provider, "engine": report.Engine, "adapterVersion": report.AdapterVersion, "gatewayVersion": report.GatewayVersion, "workerVersion": report.WorkerVersion, "configurationVersion": report.ConfigurationVersion, "capabilities": report.Capabilities, "runtimeState": report.RuntimeState}, RequestHash: requestHash, Reason: "signed gateway runtime report accepted", OccurredAt: now}
	m.runtimeEvents[nodeID] = append(m.runtimeEvents[nodeID], event)
	return current, nil
}

func (m *MemoryGovernanceStore) RecordRuntimeRejection(_ context.Context, nodeID string, report RuntimeReport, requestHash, reason string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	event := RuntimeEvent{ID: m.next("runtime"), NodeID: nodeID, GatewayPoolID: report.GatewayPoolID, EventType: "REJECTED", NodeVersion: report.ExpectedNodeVersion, BootID: report.BootID, RuntimeIdentity: map[string]any{"provider": report.Provider, "engine": report.Engine, "adapterVersion": report.AdapterVersion, "gatewayVersion": report.GatewayVersion, "workerVersion": report.WorkerVersion, "configurationVersion": report.ConfigurationVersion, "capabilities": report.Capabilities, "runtimeState": report.RuntimeState}, RequestHash: requestHash, Reason: reason, OccurredAt: now}
	m.runtimeEvents[nodeID] = append(m.runtimeEvents[nodeID], event)
	return nil
}

func (m *MemoryGovernanceStore) ListRuntimeEvents(_ context.Context, nodeID string, limit int) ([]RuntimeEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.nodes[nodeID]; !ok {
		return nil, ErrSenderNotFound
	}
	values := m.runtimeEvents[nodeID]
	if limit <= 0 || limit > len(values) {
		limit = len(values)
	}
	out := make([]RuntimeEvent, 0, limit)
	for index := len(values) - 1; index >= len(values)-limit; index-- {
		out = append(out, cloneRuntimeEvent(values[index]))
	}
	return out, nil
}

func (m *MemoryGovernanceStore) ListRuntimeEventPage(_ context.Context, nodeID string, limit int, before *time.Time, beforeID string) ([]RuntimeEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.nodes[nodeID]; !ok {
		return nil, ErrSenderNotFound
	}
	values := append([]RuntimeEvent(nil), m.runtimeEvents[nodeID]...)
	sort.Slice(values, func(i, j int) bool {
		if values[i].OccurredAt.Equal(values[j].OccurredAt) {
			return values[i].ID > values[j].ID
		}
		return values[i].OccurredAt.After(values[j].OccurredAt)
	})
	out := make([]RuntimeEvent, 0, limit)
	for _, value := range values {
		if before != nil && (value.OccurredAt.After(*before) || value.OccurredAt.Equal(*before) && value.ID >= beforeID) {
			continue
		}
		out = append(out, cloneRuntimeEvent(value))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}
func (m *MemoryGovernanceStore) GatewayPoolUsage(_ context.Context, poolID string, _ time.Time) (GatewayPoolUsage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.gatewayPools[poolID]; !ok {
		return GatewayPoolUsage{}, ErrSenderNotFound
	}
	var usage GatewayPoolUsage
	for _, node := range m.nodes {
		if node.GatewayPoolID == poolID && node.Status != "RETIRED" {
			usage.NonRetiredNodes++
		}
	}
	for _, session := range m.sessions {
		if session.GatewayPoolID == poolID && session.Status != StatusRetired {
			usage.NonRetiredSessions++
		}
	}
	return usage, nil
}
