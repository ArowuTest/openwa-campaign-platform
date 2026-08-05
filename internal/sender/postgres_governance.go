package sender

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type PostgreSQLGovernanceStore struct{ DB *sql.DB }

func (p *PostgreSQLGovernanceStore) ListPools(ctx context.Context) ([]Pool, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,name,coalesce(organisation_id::text,''),status,max_messages_per_minute,daily_capacity,reserved_capacity,version,created_at,updated_at FROM sender_pools ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pool
	for rows.Next() {
		var v Pool
		if err := rows.Scan(&v.ID, &v.Name, &v.OrganisationID, &v.Status, &v.MaxMessagesPerMinute, &v.DailyCapacity, &v.ReservedCapacity, &v.Version, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (p *PostgreSQLGovernanceStore) CreatePool(ctx context.Context, v Pool, actor, reason string) (Pool, error) {
	err := p.DB.QueryRowContext(ctx, `WITH inserted AS (INSERT INTO sender_pools(name,organisation_id,status,max_messages_per_minute,daily_capacity,reserved_capacity,version) VALUES($1,nullif($2,'')::uuid,$3,$4,$5,$6,1) RETURNING id,name,coalesce(organisation_id::text,''),status,max_messages_per_minute,daily_capacity,reserved_capacity,version,created_at,updated_at), audit AS (INSERT INTO sender_governance_events(object_type,object_id,action,actor_id,reason,object_version) SELECT 'POOL',id,'CREATED',$7::uuid,$8,version FROM inserted) SELECT id::text,name,coalesce(organisation_id::text,''),status,max_messages_per_minute,daily_capacity,reserved_capacity,version,created_at,updated_at FROM inserted`, v.Name, v.OrganisationID, v.Status, v.MaxMessagesPerMinute, v.DailyCapacity, v.ReservedCapacity, actor, reason).Scan(&v.ID, &v.Name, &v.OrganisationID, &v.Status, &v.MaxMessagesPerMinute, &v.DailyCapacity, &v.ReservedCapacity, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func (p *PostgreSQLGovernanceStore) UpdatePool(ctx context.Context, id string, e int64, v Pool, actor, reason string) (Pool, error) {
	err := p.DB.QueryRowContext(ctx, `WITH updated AS (UPDATE sender_pools SET name=$3,status=$4,max_messages_per_minute=$5,daily_capacity=$6,reserved_capacity=$7,version=version+1,updated_at=now() WHERE id=$1::uuid AND version=$2 RETURNING id,name,coalesce(organisation_id::text,''),status,max_messages_per_minute,daily_capacity,reserved_capacity,version,created_at,updated_at), audit AS (INSERT INTO sender_governance_events(object_type,object_id,action,actor_id,reason,object_version) SELECT 'POOL',id,'UPDATED',$8::uuid,$9,version FROM updated) SELECT id::text,name,coalesce(organisation_id::text,''),status,max_messages_per_minute,daily_capacity,reserved_capacity,version,created_at,updated_at FROM updated`, id, e, v.Name, v.Status, v.MaxMessagesPerMinute, v.DailyCapacity, v.ReservedCapacity, actor, reason).Scan(&v.ID, &v.Name, &v.OrganisationID, &v.Status, &v.MaxMessagesPerMinute, &v.DailyCapacity, &v.ReservedCapacity, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Pool{}, ErrSenderConflict
	}
	return v, err
}
func (p *PostgreSQLGovernanceStore) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,name,coalesce(public_ip::text,''),status,coalesce(build_version,''),capacity,queue_depth,draining,last_heartbeat_at,governance_version FROM sender_nodes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		var v Node
		var hb sql.NullTime
		if err := rows.Scan(&v.ID, &v.Name, &v.PublicIP, &v.Status, &v.BuildVersion, &v.Capacity, &v.QueueDepth, &v.Draining, &hb, &v.Version); err != nil {
			return nil, err
		}
		if hb.Valid {
			v.LastHeartbeatAt = &hb.Time
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (p *PostgreSQLGovernanceStore) RegisterNode(ctx context.Context, v Node, actor, reason string) (Node, error) {
	err := p.DB.QueryRowContext(ctx, `WITH inserted AS (INSERT INTO sender_nodes(name,public_ip,status,build_version,capacity,queue_depth,draining,governance_version) VALUES($1,nullif($2,'')::inet,$3,$4,$5,$6,$7,1) RETURNING id,name,coalesce(public_ip::text,''),status,coalesce(build_version,''),capacity,queue_depth,draining,last_heartbeat_at,governance_version),audit AS (INSERT INTO sender_governance_events(object_type,object_id,action,actor_id,reason,object_version) SELECT 'NODE',id,'REGISTERED',$8::uuid,$9,governance_version FROM inserted) SELECT id::text,name,coalesce(public_ip::text,''),status,coalesce(build_version,''),capacity,queue_depth,draining,last_heartbeat_at,governance_version FROM inserted`, v.Name, v.PublicIP, v.Status, v.BuildVersion, v.Capacity, v.QueueDepth, v.Draining, actor, reason).Scan(&v.ID, &v.Name, &v.PublicIP, &v.Status, &v.BuildVersion, &v.Capacity, &v.QueueDepth, &v.Draining, &v.LastHeartbeatAt, &v.Version)
	return v, err
}
func (p *PostgreSQLGovernanceStore) HeartbeatNode(ctx context.Context, id string, e int64, v Node, now time.Time) (Node, error) {
	var hb sql.NullTime
	err := p.DB.QueryRowContext(ctx, `UPDATE sender_nodes SET status=$3,build_version=$4,capacity=$5,queue_depth=$6,draining=$7,last_heartbeat_at=$8,governance_version=governance_version+1,updated_at=now() WHERE id=$1::uuid AND governance_version=$2 RETURNING id::text,name,coalesce(public_ip::text,''),status,coalesce(build_version,''),capacity,queue_depth,draining,last_heartbeat_at,governance_version`, id, e, v.Status, v.BuildVersion, v.Capacity, v.QueueDepth, v.Draining, now).Scan(&v.ID, &v.Name, &v.PublicIP, &v.Status, &v.BuildVersion, &v.Capacity, &v.QueueDepth, &v.Draining, &hb, &v.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrSenderConflict
	}
	if hb.Valid {
		v.LastHeartbeatAt = &hb.Time
	}
	return v, err
}
func (p *PostgreSQLGovernanceStore) ListSessions(ctx context.Context) ([]GovernedSession, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,coalesce(node_id::text,''),coalesce(sender_pool_id::text,''),masked_msisdn,engine_type,coalesce(engine_version,''),status,coalesce(safe_messages_per_minute,0)::int,coalesce(safe_daily_capacity,0),in_flight_limit,sent_today,last_heartbeat_at,last_success_at,governance_version FROM sender_sessions ORDER BY masked_msisdn`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GovernedSession
	for rows.Next() {
		var v GovernedSession
		var hb, success sql.NullTime
		if err := rows.Scan(&v.ID, &v.NodeID, &v.PoolID, &v.MaskedMSISDN, &v.EngineType, &v.EngineVersion, &v.Status, &v.SafeMessagesPerMinute, &v.SafeDailyCapacity, &v.InFlightLimit, &v.SentToday, &hb, &success, &v.Version); err != nil {
			return nil, err
		}
		if hb.Valid {
			v.LastHeartbeatAt = &hb.Time
		}
		if success.Valid {
			v.LastSuccessAt = &success.Time
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (p *PostgreSQLGovernanceStore) RegisterSession(ctx context.Context, v GovernedSession, cipher []byte, actor, reason string) (GovernedSession, error) {
	err := p.DB.QueryRowContext(ctx, `WITH inserted AS (INSERT INTO sender_sessions(node_id,sender_pool_id,logical_sender_pool,encrypted_msisdn,masked_msisdn,engine_type,engine_version,status,safe_messages_per_minute,safe_daily_capacity,in_flight_limit,sent_today,governance_version) VALUES(nullif($1,'')::uuid,nullif($2,'')::uuid,'',$3,$4,$5,$6,$7,$8,$9,$10,0,1) RETURNING id,node_id,sender_pool_id,masked_msisdn,engine_type,engine_version,status,safe_messages_per_minute,safe_daily_capacity,in_flight_limit,sent_today,last_heartbeat_at,last_success_at,governance_version),audit AS (INSERT INTO sender_governance_events(object_type,object_id,action,actor_id,reason,object_version) SELECT 'SESSION',id,'REGISTERED',$11::uuid,$12,governance_version FROM inserted) SELECT id::text,coalesce(node_id::text,''),coalesce(sender_pool_id::text,''),masked_msisdn,engine_type,coalesce(engine_version,''),status,safe_messages_per_minute::int,safe_daily_capacity,in_flight_limit,sent_today,last_heartbeat_at,last_success_at,governance_version FROM inserted`, v.NodeID, v.PoolID, cipher, v.MaskedMSISDN, v.EngineType, v.EngineVersion, v.Status, v.SafeMessagesPerMinute, v.SafeDailyCapacity, v.InFlightLimit, actor, reason).Scan(&v.ID, &v.NodeID, &v.PoolID, &v.MaskedMSISDN, &v.EngineType, &v.EngineVersion, &v.Status, &v.SafeMessagesPerMinute, &v.SafeDailyCapacity, &v.InFlightLimit, &v.SentToday, &v.LastHeartbeatAt, &v.LastSuccessAt, &v.Version)
	return v, err
}
func (p *PostgreSQLGovernanceStore) TransitionSession(ctx context.Context, id string, e int64, status Status, actor, reason string) (GovernedSession, error) {
	var v GovernedSession
	var hb, success sql.NullTime
	err := p.DB.QueryRowContext(ctx, `WITH updated AS (UPDATE sender_sessions SET status=$3,governance_version=governance_version+1,updated_at=now() WHERE id=$1::uuid AND governance_version=$2 RETURNING *),audit AS (INSERT INTO sender_governance_events(object_type,object_id,action,actor_id,reason,object_version) SELECT 'SESSION',id,$4,$5::uuid,$6,governance_version FROM updated) SELECT id::text,coalesce(node_id::text,''),coalesce(sender_pool_id::text,''),masked_msisdn,engine_type,coalesce(engine_version,''),status,safe_messages_per_minute::int,safe_daily_capacity,in_flight_limit,sent_today,last_heartbeat_at,last_success_at,governance_version FROM updated`, id, e, status, "STATUS_"+string(status), actor, reason).Scan(&v.ID, &v.NodeID, &v.PoolID, &v.MaskedMSISDN, &v.EngineType, &v.EngineVersion, &v.Status, &v.SafeMessagesPerMinute, &v.SafeDailyCapacity, &v.InFlightLimit, &v.SentToday, &hb, &success, &v.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return GovernedSession{}, ErrSenderConflict
	}
	if hb.Valid {
		v.LastHeartbeatAt = &hb.Time
	}
	if success.Valid {
		v.LastSuccessAt = &success.Time
	}
	return v, err
}
func (p *PostgreSQLGovernanceStore) HeartbeatSession(ctx context.Context, id string, e int64, v GovernedSession, now time.Time) (GovernedSession, error) {
	var hb, success sql.NullTime
	err := p.DB.QueryRowContext(ctx, `UPDATE sender_sessions SET status=$3,engine_version=$4,safe_messages_per_minute=$5,safe_daily_capacity=$6,in_flight_limit=$7,sent_today=$8,last_heartbeat_at=$9,governance_version=governance_version+1,updated_at=now() WHERE id=$1::uuid AND governance_version=$2 RETURNING id::text,coalesce(node_id::text,''),coalesce(sender_pool_id::text,''),masked_msisdn,engine_type,coalesce(engine_version,''),status,safe_messages_per_minute::int,safe_daily_capacity,in_flight_limit,sent_today,last_heartbeat_at,last_success_at,governance_version`, id, e, v.Status, v.EngineVersion, v.SafeMessagesPerMinute, v.SafeDailyCapacity, v.InFlightLimit, v.SentToday, now).Scan(&v.ID, &v.NodeID, &v.PoolID, &v.MaskedMSISDN, &v.EngineType, &v.EngineVersion, &v.Status, &v.SafeMessagesPerMinute, &v.SafeDailyCapacity, &v.InFlightLimit, &v.SentToday, &hb, &success, &v.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return GovernedSession{}, ErrSenderConflict
	}
	if hb.Valid {
		v.LastHeartbeatAt = &hb.Time
	}
	if success.Valid {
		v.LastSuccessAt = &success.Time
	}
	return v, err
}
func (p *PostgreSQLGovernanceStore) Capacity(ctx context.Context, pool string, now time.Time) (CapacitySummary, error) {
	var v CapacitySummary
	v.PoolID = pool
	v.AsAt = now
	err := p.DB.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE ss.status IN ('READY','BUSY') AND ss.last_heartbeat_at>$2),count(DISTINCT ss.node_id) FILTER(WHERE sn.status='READY' AND NOT sn.draining AND sn.last_heartbeat_at>$2),coalesce(sum(ss.safe_messages_per_minute) FILTER(WHERE ss.status IN ('READY','BUSY') AND ss.last_heartbeat_at>$2),0)::int,least(sp.max_messages_per_minute,coalesce(sum(ss.safe_messages_per_minute) FILTER(WHERE ss.status IN ('READY','BUSY') AND ss.last_heartbeat_at>$2),0))::int,coalesce(sum(ss.safe_daily_capacity),0),coalesce(sum(greatest(ss.safe_daily_capacity-ss.sent_today,0)) FILTER(WHERE ss.status IN ('READY','BUSY') AND ss.last_heartbeat_at>$2),0),sp.reserved_capacity,greatest(least(sp.daily_capacity,coalesce(sum(greatest(ss.safe_daily_capacity-ss.sent_today,0)) FILTER(WHERE ss.status IN ('READY','BUSY') AND ss.last_heartbeat_at>$2),0))-sp.reserved_capacity,0) FROM sender_pools sp LEFT JOIN sender_sessions ss ON ss.sender_pool_id=sp.id LEFT JOIN sender_nodes sn ON sn.id=ss.node_id WHERE sp.id=$1::uuid GROUP BY sp.max_messages_per_minute,sp.daily_capacity,sp.reserved_capacity`, pool, now.Add(-90*time.Second)).Scan(&v.ReadySessions, &v.HealthyNodes, &v.ConfiguredMessagesPerMinute, &v.AvailableMessagesPerMinute, &v.ConfiguredDailyCapacity, &v.RemainingDailyCapacity, &v.ReservedCapacity, &v.AvailableDailyCapacity)
	if errors.Is(err, sql.ErrNoRows) {
		return CapacitySummary{}, ErrSenderNotFound
	}
	if err != nil {
		return CapacitySummary{}, fmt.Errorf("calculate sender capacity: %w", err)
	}
	return v, nil
}

func decodeCapabilities(raw []byte) ([]Capability, error) {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	out := make([]Capability, 0, len(values))
	for _, value := range values {
		out = append(out, Capability(value))
	}
	return out, nil
}
func encodeCapabilities(values []Capability) ([]byte, error) {
	raw := make([]string, len(values))
	for i, v := range values {
		raw[i] = string(v)
	}
	return json.Marshal(raw)
}

func (p *PostgreSQLGovernanceStore) ListGatewayPools(ctx context.Context) ([]GatewayPool, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,version,created_at,updated_at FROM gateway_pools ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GatewayPool
	for rows.Next() {
		var v GatewayPool
		var raw []byte
		if err := rows.Scan(&v.ID, &v.Name, &v.Provider, &v.Engine, &v.AdapterVersion, &v.Status, &raw, &v.MinimumHealthyNodes, &v.Version, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		v.Capabilities, err = decodeCapabilities(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (p *PostgreSQLGovernanceStore) GetGatewayPool(ctx context.Context, id string) (GatewayPool, error) {
	var v GatewayPool
	var raw []byte
	err := p.DB.QueryRowContext(ctx, `SELECT id::text,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,version,created_at,updated_at FROM gateway_pools WHERE id=$1::uuid`, id).Scan(&v.ID, &v.Name, &v.Provider, &v.Engine, &v.AdapterVersion, &v.Status, &raw, &v.MinimumHealthyNodes, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return GatewayPool{}, ErrSenderNotFound
	}
	if err != nil {
		return GatewayPool{}, err
	}
	v.Capabilities, err = decodeCapabilities(raw)
	return v, err
}
func (p *PostgreSQLGovernanceStore) CreateGatewayPool(ctx context.Context, v GatewayPool, actor, reason string) (GatewayPool, error) {
	raw, err := encodeCapabilities(v.Capabilities)
	if err != nil {
		return GatewayPool{}, err
	}
	var stored []byte
	err = p.DB.QueryRowContext(ctx, `WITH inserted AS (INSERT INTO gateway_pools(name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7) RETURNING *),audit AS (INSERT INTO gateway_pool_events(gateway_pool_id,event_type,version,actor_id,reason,evidence) SELECT id,'CREATED',version,$8::uuid,$9,jsonb_build_object('provider',provider,'engine',engine,'adapterVersion',adapter_version,'capabilities',capabilities) FROM inserted) SELECT id::text,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,version,created_at,updated_at FROM inserted`, v.Name, v.Provider, v.Engine, v.AdapterVersion, v.Status, string(raw), v.MinimumHealthyNodes, actor, reason).Scan(&v.ID, &v.Name, &v.Provider, &v.Engine, &v.AdapterVersion, &v.Status, &stored, &v.MinimumHealthyNodes, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return GatewayPool{}, err
	}
	v.Capabilities, err = decodeCapabilities(stored)
	return v, err
}
func (p *PostgreSQLGovernanceStore) UpdateGatewayPool(ctx context.Context, id string, expected int64, v GatewayPool, actor, reason string) (GatewayPool, error) {
	raw, err := encodeCapabilities(v.Capabilities)
	if err != nil {
		return GatewayPool{}, err
	}
	var stored []byte
	err = p.DB.QueryRowContext(ctx, `WITH updated AS (UPDATE gateway_pools SET name=$3,adapter_version=$4,status=$5,capabilities=$6::jsonb,minimum_healthy_nodes=$7,version=version+1,updated_at=now() WHERE id=$1::uuid AND version=$2 AND provider=$8 AND engine=$9 RETURNING *),audit AS (INSERT INTO gateway_pool_events(gateway_pool_id,event_type,version,actor_id,reason,evidence) SELECT id,'UPDATED',version,$10::uuid,$11,jsonb_build_object('provider',provider,'engine',engine,'adapterVersion',adapter_version,'capabilities',capabilities) FROM updated) SELECT id::text,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,version,created_at,updated_at FROM updated`, id, expected, v.Name, v.AdapterVersion, v.Status, string(raw), v.MinimumHealthyNodes, v.Provider, v.Engine, actor, reason).Scan(&v.ID, &v.Name, &v.Provider, &v.Engine, &v.AdapterVersion, &v.Status, &stored, &v.MinimumHealthyNodes, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return GatewayPool{}, ErrSenderConflict
	}
	if err != nil {
		return GatewayPool{}, err
	}
	v.Capabilities, err = decodeCapabilities(stored)
	return v, err
}
