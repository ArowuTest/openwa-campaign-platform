package sender

import "context"

func (m *MemoryGovernanceStore) GetPool(_ context.Context, identifier string) (Pool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.pools[identifier]
	if !ok {
		return Pool{}, ErrSenderNotFound
	}
	return value, nil
}
