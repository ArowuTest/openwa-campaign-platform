package sender

import "context"

func (m *MemoryGovernanceStore) ConfigureSessionProxy(_ context.Context, sessionID string, expected int64, ciphertext []byte, _, _ string) (SessionProxyStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.sessions[sessionID]
	if !ok {
		return SessionProxyStatus{}, ErrSenderNotFound
	}
	if current.Version != expected || !proxyConfigurationMutable(current.Status) {
		return SessionProxyStatus{}, ErrSenderConflict
	}
	m.sessionProxies[sessionID] = append([]byte(nil), ciphertext...)
	current.Version++
	m.sessions[sessionID] = current
	return SessionProxyStatus{SessionID: sessionID, Configured: true, Version: current.Version}, nil
}

func (m *MemoryGovernanceStore) ClearSessionProxy(_ context.Context, sessionID string, expected int64, _, _ string) (SessionProxyStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.sessions[sessionID]
	if !ok {
		return SessionProxyStatus{}, ErrSenderNotFound
	}
	if current.Version != expected || !proxyConfigurationMutable(current.Status) {
		return SessionProxyStatus{}, ErrSenderConflict
	}
	delete(m.sessionProxies, sessionID)
	current.Version++
	m.sessions[sessionID] = current
	return SessionProxyStatus{SessionID: sessionID, Configured: false, Version: current.Version}, nil
}

func (m *MemoryGovernanceStore) LoadSessionProxy(_ context.Context, sessionID string) ([]byte, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.sessions[sessionID]
	if !ok {
		return nil, 0, ErrSenderNotFound
	}
	return append([]byte(nil), m.sessionProxies[sessionID]...), current.Version, nil
}
