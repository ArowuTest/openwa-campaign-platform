package campaignpreparation

import (
	"campaign-platform/internal/execution"
	"context"
	"database/sql"
	"time"
)

type PostgreSQLCapacityReader struct{ DB *sql.DB }

// RouteCapacity reports a gross current aggregate with the static pool reserve
// deduction. It does not account for dynamic campaign reservations.
func (s *PostgreSQLCapacityReader) RouteCapacity(ctx context.Context, route execution.PoolRoute, at time.Time) (execution.PoolCapacity, error) {
	if s == nil || s.DB == nil {
		return execution.PoolCapacity{}, ErrUnavailable
	}
	if route.Provider != "OPENWA" || (route.Engine != "BAILEYS" && route.Engine != "WHATSAPP_WEB_JS") || route.ProviderAdapterVersion == "" || route.SenderPoolID == "" || route.GatewayPoolID == "" {
		return execution.PoolCapacity{}, ErrRouteIdentityUnavailable
	}
	out := execution.PoolCapacity{SenderPoolID: route.SenderPoolID, GatewayPoolID: route.GatewayPoolID}
	var noncanonicalRegistration bool
	err := s.DB.QueryRowContext(ctx, `
WITH healthy_route AS (
 SELECT ss.node_id,ss.engine_type,ss.safe_messages_per_minute,
        greatest(ss.safe_daily_capacity-ss.sent_today,0) AS remaining_daily
 FROM sender_sessions ss
 JOIN sender_nodes sn ON sn.id=ss.node_id
 WHERE ss.sender_pool_id=$1::uuid AND ss.gateway_pool_id=$2::uuid
   AND sn.gateway_pool_id=$2::uuid
   AND sn.provider=$4 AND sn.engine=$5 AND sn.adapter_version=$6
   AND ss.status IN ('READY','BUSY') AND sn.status='READY' AND NOT sn.draining
   AND ss.last_heartbeat_at>$3 AND sn.last_heartbeat_at>$3
   AND ss.last_heartbeat_at<=$7 AND sn.last_heartbeat_at<=$7
), eligible AS (
 SELECT * FROM healthy_route WHERE engine_type=$5
), measured AS (
 SELECT count(*)::int AS healthy_sessions,count(DISTINCT node_id)::int AS healthy_nodes,
 coalesce(sum(safe_messages_per_minute),0) AS mpm,
 coalesce(sum(remaining_daily),0) AS daily FROM eligible
)
SELECT m.healthy_sessions,m.healthy_nodes,gp.minimum_healthy_nodes,
 floor(least(sp.max_messages_per_minute,m.mpm))::bigint,
 greatest(least(sp.daily_capacity,m.daily)-sp.reserved_capacity,0)::bigint,
 EXISTS(SELECT 1 FROM healthy_route WHERE engine_type NOT IN ('BAILEYS','WHATSAPP_WEB_JS'))
FROM sender_pools sp
JOIN gateway_pools gp ON gp.id=$2::uuid
CROSS JOIN measured m
WHERE sp.id=$1::uuid AND sp.status='ACTIVE' AND gp.status='ACTIVE'
 AND gp.provider=$4 AND gp.engine=$5 AND gp.adapter_version=$6
`, route.SenderPoolID, route.GatewayPoolID, at.Add(-90*time.Second), route.Provider, route.Engine, route.ProviderAdapterVersion, at).Scan(&out.HealthySessions, &out.HealthyNodes, &out.MinimumHealthyNodes, &out.AvailableMessagesPerMinute, &out.AvailableDailyUnits, &noncanonicalRegistration)
	if err != nil {
		return execution.PoolCapacity{}, err
	}
	// Noncanonical registrations are unusable evidence, never aliases for the
	// requested engine. Distinguish an identity migration gap from absent health
	// only when no exact canonical session can be measured on this healthy route.
	if out.HealthySessions == 0 && noncanonicalRegistration {
		return execution.PoolCapacity{}, ErrRouteIdentityUnavailable
	}
	if out.AvailableMessagesPerMinute < 0 || int64(out.AvailableMessagesPerMinute) > MaxSafeInteger/60 {
		return execution.PoolCapacity{}, ErrUnavailable
	}
	out.AvailableHourlyUnits = int64(out.AvailableMessagesPerMinute) * 60
	return out, nil
}
