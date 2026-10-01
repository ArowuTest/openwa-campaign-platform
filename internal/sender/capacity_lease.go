package sender

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func capacityHeartbeatTTL(ttl time.Duration) (time.Duration, error) {
	if ttl == 0 {
		return 90 * time.Second, nil
	}
	if ttl < 10*time.Second || ttl > 10*time.Minute {
		return 0, errors.New("sender capacity heartbeat TTL must be between 10 seconds and 10 minutes")
	}
	return ttl, nil
}

func (m *MemoryGovernanceStore) leaseCapacity(ctx context.Context, pool string, now time.Time) (CapacitySummary, error) {
	ttl, err := capacityHeartbeatTTL(m.HeartbeatTTL)
	if err != nil {
		return CapacitySummary{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pools[pool]
	if !ok {
		return CapacitySummary{}, ErrSenderNotFound
	}
	result := CapacitySummary{PoolID: pool, ReservedCapacity: p.ReservedCapacity, AsAt: now}
	nodes := map[string]bool{}
	for _, s := range m.sessions {
		if s.PoolID != pool {
			continue
		}
		result.ConfiguredDailyCapacity += s.SafeDailyCapacity
		node, exists := m.nodes[s.NodeID]
		if !exists || s.Status != StatusReady && s.Status != StatusBusy || s.LastHeartbeatAt == nil || !s.LastHeartbeatAt.After(now.Add(-ttl)) || node.Status != "READY" || node.Draining || node.LastHeartbeatAt == nil || !node.LastHeartbeatAt.After(now.Add(-ttl)) || node.BootID == "" || m.Leases == nil {
			continue
		}
		lease, exists, err := m.Leases.Get(ctx, s.ID)
		if err != nil {
			return CapacitySummary{}, err
		}
		if !exists || lease.WorkerID != node.ID || !lease.ExpiresAt.After(now) {
			continue
		}
		lease.Token = node.BootID
		if err = m.Leases.Validate(ctx, lease, now); err != nil {
			continue
		}
		nodes[node.ID] = true
		result.ReadySessions++
		result.ConfiguredMessagesPerMinute += s.SafeMessagesPerMinute
		result.AvailableMessagesPerMinute += s.SafeMessagesPerMinute
		if s.SafeDailyCapacity > s.SentToday {
			result.RemainingDailyCapacity += s.SafeDailyCapacity - s.SentToday
		}
	}
	result.HealthyNodes = len(nodes)
	result.AvailableMessagesPerMinute = min(p.MaxMessagesPerMinute, result.AvailableMessagesPerMinute)
	result.AvailableDailyCapacity = max(int64(0), min(p.DailyCapacity, result.RemainingDailyCapacity)-p.ReservedCapacity)
	return result, nil
}

func (p *PostgreSQLGovernanceStore) leaseCapacity(ctx context.Context, pool string, now time.Time) (CapacitySummary, error) {
	ttl, err := capacityHeartbeatTTL(p.HeartbeatTTL)
	if err != nil {
		return CapacitySummary{}, err
	}
	result := CapacitySummary{PoolID: pool, AsAt: now}
	const query = `WITH measured AS (
 SELECT ss.*,coalesce(
  ss.status IN ('READY','BUSY') AND ss.last_heartbeat_at>$2
  AND sn.status='READY' AND NOT sn.draining AND sn.last_heartbeat_at>$2
  AND sl.worker_node_id=ss.node_id AND sl.expires_at>$3
  AND coalesce(sn.boot_id,'')<>''
  AND sl.lease_token_hash=sha256(convert_to(sn.boot_id,'UTF8')),false) AS healthy
 FROM sender_sessions ss
 LEFT JOIN sender_nodes sn ON sn.id=ss.node_id
 LEFT JOIN sender_session_leases sl ON sl.session_id=ss.id
 WHERE ss.sender_pool_id=$1::uuid
)
SELECT count(ss.id) FILTER(WHERE ss.healthy),
 count(DISTINCT ss.node_id) FILTER(WHERE ss.healthy),
 coalesce(sum(ss.safe_messages_per_minute) FILTER(WHERE ss.healthy),0)::int,
 least(sp.max_messages_per_minute,coalesce(sum(ss.safe_messages_per_minute) FILTER(WHERE ss.healthy),0))::int,
 coalesce(sum(ss.safe_daily_capacity),0),
 coalesce(sum(greatest(ss.safe_daily_capacity-ss.sent_today,0)) FILTER(WHERE ss.healthy),0),
 sp.reserved_capacity,
 greatest(least(sp.daily_capacity,coalesce(sum(greatest(ss.safe_daily_capacity-ss.sent_today,0)) FILTER(WHERE ss.healthy),0))-sp.reserved_capacity,0)
FROM sender_pools sp LEFT JOIN measured ss ON ss.sender_pool_id=sp.id
WHERE sp.id=$1::uuid GROUP BY sp.max_messages_per_minute,sp.daily_capacity,sp.reserved_capacity`
	err = p.DB.QueryRowContext(ctx, query, pool, now.Add(-ttl), now).Scan(&result.ReadySessions, &result.HealthyNodes, &result.ConfiguredMessagesPerMinute, &result.AvailableMessagesPerMinute, &result.ConfiguredDailyCapacity, &result.RemainingDailyCapacity, &result.ReservedCapacity, &result.AvailableDailyCapacity)
	if errors.Is(err, sql.ErrNoRows) {
		return CapacitySummary{}, ErrSenderNotFound
	}
	if err != nil {
		return CapacitySummary{}, fmt.Errorf("calculate lease-bound sender capacity: %w", err)
	}
	return result, nil
}
