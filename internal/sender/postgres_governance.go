package sender

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/shared/id"
)

type PostgreSQLGovernanceStore struct{ DB *sql.DB }

func (p *PostgreSQLGovernanceStore) ListPools(ctx context.Context) ([]Pool, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,name,coalesce(organisation_id::text,''),status,max_messages_per_minute,daily_capacity,reserved_capacity,version,created_at,updated_at FROM sender_pools ORDER BY name LIMIT 5000`)
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

const nodeColumns = `id::text,name,coalesce(public_ip::text,''),coalesce(internal_url,''),coalesce(gateway_pool_id::text,''),coalesce(provider,''),coalesce(engine,''),coalesce(adapter_version,''),coalesce(boot_id,''),status,coalesce(build_version,''),coalesce(gateway_version,''),coalesce(worker_version,''),coalesce(configuration_version,''),runtime_capabilities,coalesce(runtime_state,''),capacity,session_count,queue_depth,cpu_percent::float8,memory_bytes,draining,registered_at,last_heartbeat_at,governance_version`

func scanNode(row interface{ Scan(...any) error }) (Node, error) {
	var value Node
	var raw []byte
	var registered, heartbeat sql.NullTime
	var runtimeState string
	err := row.Scan(&value.ID, &value.Name, &value.PublicIP, &value.InternalURL, &value.GatewayPoolID, &value.Provider, &value.Engine, &value.AdapterVersion, &value.BootID, &value.Status, &value.BuildVersion, &value.GatewayVersion, &value.WorkerVersion, &value.ConfigurationVersion, &raw, &runtimeState, &value.Capacity, &value.SessionCount, &value.QueueDepth, &value.CPUPercent, &value.MemoryBytes, &value.Draining, &registered, &heartbeat, &value.Version)
	if err != nil {
		return Node{}, err
	}
	value.RuntimeState = RuntimeState(runtimeState)
	if len(raw) > 0 {
		if value.RuntimeCapabilities, err = decodeCapabilities(raw); err != nil {
			return Node{}, err
		}
	}
	if registered.Valid {
		t := registered.Time.UTC()
		value.RegisteredAt = &t
	}
	if heartbeat.Valid {
		t := heartbeat.Time.UTC()
		value.LastHeartbeatAt = &t
	}
	return value, nil
}

func (p *PostgreSQLGovernanceStore) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+nodeColumns+` FROM sender_nodes ORDER BY name LIMIT 5000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		value, scanErr := scanNode(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, value)
	}
	return out, rows.Err()
}
func (p *PostgreSQLGovernanceStore) GetNode(ctx context.Context, nodeID string) (Node, error) {
	value, err := scanNode(p.DB.QueryRowContext(ctx, `SELECT `+nodeColumns+` FROM sender_nodes WHERE id=$1::uuid`, nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrSenderNotFound
	}
	return value, err
}
func (p *PostgreSQLGovernanceStore) RegisterNode(ctx context.Context, value Node, actor, reason string) (Node, error) {
	var raw []byte
	if len(value.RuntimeCapabilities) == 0 {
		raw = []byte(`[]`)
	} else {
		var err error
		raw, err = encodeCapabilities(value.RuntimeCapabilities)
		if err != nil {
			return Node{}, err
		}
	}
	row := p.DB.QueryRowContext(ctx, `WITH inserted AS (
INSERT INTO sender_nodes(name,public_ip,internal_url,gateway_pool_id,provider,engine,adapter_version,boot_id,status,build_version,gateway_version,worker_version,configuration_version,runtime_capabilities,runtime_state,capacity,session_count,queue_depth,cpu_percent,memory_bytes,draining,registered_at,last_heartbeat_at,governance_version)
VALUES($1,NULLIF($2,'')::inet,NULLIF($3,''),NULLIF($4,'')::uuid,NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),$9,NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),NULLIF($13,''),$14::jsonb,NULLIF($15,''),$16,$17,$18,$19,$20,$21,$22,$23,1)
RETURNING *), audit AS (
INSERT INTO sender_governance_events(object_type,object_id,action,actor_id,reason,object_version)
SELECT 'NODE',id,'REGISTERED',$24::uuid,$25,governance_version FROM inserted)
SELECT `+nodeColumns+` FROM inserted`, value.Name, value.PublicIP, value.InternalURL, value.GatewayPoolID, value.Provider, value.Engine, value.AdapterVersion, value.BootID, value.Status, value.BuildVersion, value.GatewayVersion, value.WorkerVersion, value.ConfigurationVersion, string(raw), string(value.RuntimeState), value.Capacity, value.SessionCount, value.QueueDepth, value.CPUPercent, value.MemoryBytes, value.Draining, value.RegisteredAt, value.LastHeartbeatAt, actor, reason)
	return scanNode(row)
}
func (p *PostgreSQLGovernanceStore) HeartbeatNode(ctx context.Context, nodeID string, expected int64, value Node, now time.Time) (Node, error) {
	raw, err := encodeCapabilities(value.RuntimeCapabilities)
	if err != nil {
		return Node{}, err
	}
	row := p.DB.QueryRowContext(ctx, `UPDATE sender_nodes SET status=$3,build_version=NULLIF($4,''),gateway_version=NULLIF($5,''),worker_version=NULLIF($6,''),configuration_version=NULLIF($7,''),runtime_capabilities=$8::jsonb,runtime_state=NULLIF($9,''),capacity=$10,session_count=$11,queue_depth=$12,cpu_percent=$13,memory_bytes=$14,draining=$15,internal_url=coalesce(NULLIF($16,''),internal_url),gateway_pool_id=coalesce(NULLIF($17,'')::uuid,gateway_pool_id),provider=coalesce(NULLIF($18,''),provider),engine=coalesce(NULLIF($19,''),engine),adapter_version=coalesce(NULLIF($20,''),adapter_version),boot_id=coalesce(NULLIF($21,''),boot_id),registered_at=coalesce(registered_at,$22),last_heartbeat_at=$22,governance_version=governance_version+1,updated_at=now() WHERE id=$1::uuid AND governance_version=$2 RETURNING `+nodeColumns, nodeID, expected, value.Status, value.BuildVersion, value.GatewayVersion, value.WorkerVersion, value.ConfigurationVersion, string(raw), string(value.RuntimeState), value.Capacity, value.SessionCount, value.QueueDepth, value.CPUPercent, value.MemoryBytes, value.Draining, value.InternalURL, value.GatewayPoolID, value.Provider, value.Engine, value.AdapterVersion, value.BootID, now)
	out, err := scanNode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrSenderConflict
	}
	return out, err
}
func (p *PostgreSQLGovernanceStore) TransitionNode(ctx context.Context, nodeID string, expected int64, status, actor, reason string) (Node, error) {
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Node{}, err
	}
	defer tx.Rollback()
	if status == "RETIRED" {
		var active int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sender_sessions WHERE node_id=$1::uuid AND status<>'RETIRED'`, nodeID).Scan(&active); err != nil {
			return Node{}, err
		}
		if active > 0 {
			return Node{}, errors.New("gateway node has non-retired sessions")
		}
	}
	row := tx.QueryRowContext(ctx, `WITH updated AS (
UPDATE sender_nodes SET status=$3,draining=$4,governance_version=governance_version+1,updated_at=now() WHERE id=$1::uuid AND governance_version=$2 RETURNING *), audit AS (
INSERT INTO sender_governance_events(object_type,object_id,action,actor_id,reason,object_version)
SELECT 'NODE',id,'STATUS_'||status,$5::uuid,$6,governance_version FROM updated)
SELECT `+nodeColumns+` FROM updated`, nodeID, expected, status, status == "DRAINING" || status == "RETIRED", actor, reason)
	value, err := scanNode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrSenderConflict
	}
	if err != nil {
		return Node{}, err
	}
	if err = insertRuntimeEvent(ctx, tx, RuntimeEvent{NodeID: value.ID, GatewayPoolID: value.GatewayPoolID, EventType: status, NodeVersion: value.Version, BootID: value.BootID, RuntimeIdentity: map[string]any{"status": value.Status, "draining": value.Draining}, Reason: reason, OccurredAt: time.Now().UTC()}); err != nil {
		return Node{}, err
	}
	if err = tx.Commit(); err != nil {
		return Node{}, err
	}
	return value, nil
}

const governedSessionColumns = `id::text,coalesce(node_id::text,''),coalesce(sender_pool_id::text,''),coalesce(gateway_pool_id::text,''),masked_msisdn,coalesce(owner_reference,''),coalesce(registration_country_iso2,''),coalesce(profile_display_name,''),coalesce(recovery_reference,''),engine_type,coalesce(engine_version,''),coalesce(state_volume_reference,''),status,coalesce(safe_messages_per_minute,0)::int,coalesce(safe_daily_capacity,0),in_flight_limit,sent_today,last_heartbeat_at,last_success_at,quarantined_at,coalesce(quarantine_reason,''),reinstated_at,governance_version`

func scanGovernedSession(row interface{ Scan(...any) error }) (GovernedSession, error) {
	var value GovernedSession
	var heartbeat, success, quarantined, reinstated sql.NullTime
	err := row.Scan(
		&value.ID, &value.NodeID, &value.PoolID, &value.GatewayPoolID, &value.MaskedMSISDN,
		&value.OwnerReference, &value.RegistrationCountryISO2, &value.ProfileDisplayName, &value.RecoveryReference,
		&value.EngineType, &value.EngineVersion, &value.StateVolumeReference, &value.Status,
		&value.SafeMessagesPerMinute, &value.SafeDailyCapacity, &value.InFlightLimit, &value.SentToday,
		&heartbeat, &success, &quarantined, &value.QuarantineReason, &reinstated, &value.Version,
	)
	if err != nil {
		return GovernedSession{}, err
	}
	value.RecoveryReferenceConfigured = strings.TrimSpace(value.RecoveryReference) != ""
	if heartbeat.Valid {
		value.LastHeartbeatAt = &heartbeat.Time
	}
	if success.Valid {
		value.LastSuccessAt = &success.Time
	}
	if quarantined.Valid {
		value.QuarantinedAt = &quarantined.Time
	}
	if reinstated.Valid {
		value.ReinstatedAt = &reinstated.Time
	}
	return value, nil
}

func (p *PostgreSQLGovernanceStore) ListSessions(ctx context.Context) ([]GovernedSession, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+governedSessionColumns+` FROM sender_sessions ORDER BY masked_msisdn LIMIT 10000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GovernedSession
	for rows.Next() {
		value, scanErr := scanGovernedSession(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (p *PostgreSQLGovernanceStore) GetSession(ctx context.Context, id string) (GovernedSession, error) {
	value, err := scanGovernedSession(p.DB.QueryRowContext(ctx, `SELECT `+governedSessionColumns+` FROM sender_sessions WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return GovernedSession{}, ErrSenderNotFound
	}
	return value, err
}

func (p *PostgreSQLGovernanceStore) RegisterSession(ctx context.Context, v GovernedSession, cipher []byte, actor, reason string) (GovernedSession, error) {
	row := p.DB.QueryRowContext(ctx, `WITH inserted AS (
INSERT INTO sender_sessions(node_id,sender_pool_id,gateway_pool_id,logical_sender_pool,encrypted_msisdn,masked_msisdn,owner_reference,registration_country_iso2,profile_display_name,recovery_reference,engine_type,engine_version,state_volume_reference,status,safe_messages_per_minute,safe_daily_capacity,in_flight_limit,sent_today,governance_version)
VALUES(nullif($1,'')::uuid,nullif($2,'')::uuid,nullif($3,'')::uuid,'',$4,$5,$6,$7,$8,$9,$10,$11,nullif($12,''),$13,$14,$15,$16,0,1)
RETURNING *
), audit AS (
INSERT INTO sender_governance_events(object_type,object_id,action,actor_id,reason,object_version)
SELECT 'SESSION',id,'REGISTERED',$17::uuid,$18,governance_version FROM inserted
)
SELECT `+governedSessionColumns+` FROM inserted`, v.NodeID, v.PoolID, v.GatewayPoolID, cipher, v.MaskedMSISDN,
		v.OwnerReference, v.RegistrationCountryISO2, v.ProfileDisplayName, v.RecoveryReference,
		v.EngineType, v.EngineVersion, v.StateVolumeReference, v.Status, v.SafeMessagesPerMinute, v.SafeDailyCapacity, v.InFlightLimit, actor, reason)
	return scanGovernedSession(row)
}
func (p *PostgreSQLGovernanceStore) UpdateSessionMetadata(ctx context.Context, id string, expected int64, value SessionOperationalMetadata, actor, reason string) (GovernedSession, error) {
	row := p.DB.QueryRowContext(ctx, `WITH updated AS (
UPDATE sender_sessions SET owner_reference=$3,registration_country_iso2=$4,profile_display_name=$5,recovery_reference=$6,
governance_version=governance_version+1,updated_at=now()
WHERE id=$1::uuid AND governance_version=$2
RETURNING *
), audit AS (
INSERT INTO sender_governance_events(object_type,object_id,action,actor_id,reason,object_version)
SELECT 'SESSION',id,'METADATA_UPDATED',$7::uuid,$8,governance_version FROM updated
)
SELECT `+governedSessionColumns+` FROM updated`, id, expected, value.OwnerReference, value.RegistrationCountryISO2, value.ProfileDisplayName, value.RecoveryReference, actor, reason)
	updated, err := scanGovernedSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return GovernedSession{}, ErrSenderConflict
	}
	return updated, err
}
func (p *PostgreSQLGovernanceStore) TransitionSession(ctx context.Context, id string, e int64, status Status, actor, reason string) (GovernedSession, error) {
	row := p.DB.QueryRowContext(ctx, `WITH updated AS (
UPDATE sender_sessions SET status=$3,
quarantined_at=CASE WHEN $3='QUARANTINED' THEN now() ELSE quarantined_at END,
quarantine_reason=CASE WHEN $3='QUARANTINED' THEN $6 WHEN $3='READY' THEN NULL ELSE quarantine_reason END,
reinstated_at=CASE WHEN $3='READY' AND quarantined_at IS NOT NULL THEN now() ELSE reinstated_at END,
governance_version=governance_version+1,updated_at=now()
WHERE id=$1::uuid AND governance_version=$2 AND sender_session_transition_allowed(status,$3)
RETURNING *
), audit AS (
INSERT INTO sender_governance_events(object_type,object_id,action,actor_id,reason,object_version)
SELECT 'SESSION',id,$4,$5::uuid,$6,governance_version FROM updated
)
SELECT `+governedSessionColumns+` FROM updated`, id, e, status, "STATUS_"+string(status), actor, reason)
	value, err := scanGovernedSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return GovernedSession{}, ErrSenderConflict
	}
	return value, err
}
func (p *PostgreSQLGovernanceStore) HeartbeatSession(ctx context.Context, id string, e int64, v GovernedSession, now time.Time) (GovernedSession, error) {
	row := p.DB.QueryRowContext(ctx, `UPDATE sender_sessions SET
status=CASE WHEN status IN('QUARANTINED','RESTRICTED','RETIRED') THEN status WHEN sender_session_transition_allowed(status,$3) THEN $3 ELSE status END,
engine_version=$4,safe_messages_per_minute=$5,safe_daily_capacity=$6,in_flight_limit=$7,sent_today=$8,last_heartbeat_at=$9,
governance_version=governance_version+1,updated_at=now()
WHERE id=$1::uuid AND governance_version=$2
RETURNING `+governedSessionColumns, id, e, v.Status, v.EngineVersion, v.SafeMessagesPerMinute, v.SafeDailyCapacity, v.InFlightLimit, v.SentToday, now)
	value, err := scanGovernedSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return GovernedSession{}, ErrSenderConflict
	}
	return value, err
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

const gatewayPoolColumns = `id::text,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,coalesce(created_by::text,''),coalesce(submitted_by::text,''),coalesce(approved_by::text,''),effective_from,effective_to,coalesce(approval_reason,''),version,created_at,updated_at`

func scanGatewayPool(row interface{ Scan(...any) error }) (GatewayPool, error) {
	var value GatewayPool
	var raw []byte
	var effectiveFrom, effectiveTo sql.NullTime
	err := row.Scan(&value.ID, &value.Name, &value.Provider, &value.Engine, &value.AdapterVersion, &value.Status, &raw, &value.MinimumHealthyNodes, &value.CreatedBy, &value.SubmittedBy, &value.ApprovedBy, &effectiveFrom, &effectiveTo, &value.ApprovalReason, &value.Version, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return GatewayPool{}, err
	}
	value.Capabilities, err = decodeCapabilities(raw)
	if err != nil {
		return GatewayPool{}, err
	}
	if effectiveFrom.Valid {
		t := effectiveFrom.Time.UTC()
		value.EffectiveFrom = &t
	}
	if effectiveTo.Valid {
		t := effectiveTo.Time.UTC()
		value.EffectiveTo = &t
	}
	return value, nil
}

func (p *PostgreSQLGovernanceStore) ListGatewayPools(ctx context.Context) ([]GatewayPool, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+gatewayPoolColumns+` FROM gateway_pools ORDER BY name LIMIT 5000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GatewayPool
	for rows.Next() {
		value, scanErr := scanGatewayPool(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, value)
	}
	return out, rows.Err()
}
func (p *PostgreSQLGovernanceStore) GetGatewayPool(ctx context.Context, poolID string) (GatewayPool, error) {
	value, err := scanGatewayPool(p.DB.QueryRowContext(ctx, `SELECT `+gatewayPoolColumns+` FROM gateway_pools WHERE id=$1::uuid`, poolID))
	if errors.Is(err, sql.ErrNoRows) {
		return GatewayPool{}, ErrSenderNotFound
	}
	return value, err
}
func (p *PostgreSQLGovernanceStore) CreateGatewayPool(ctx context.Context, value GatewayPool, actor, reason string) (GatewayPool, error) {
	raw, err := encodeCapabilities(value.Capabilities)
	if err != nil {
		return GatewayPool{}, err
	}
	row := p.DB.QueryRowContext(ctx, `WITH inserted AS (
INSERT INTO gateway_pools(name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,created_by,submitted_by,approved_by,effective_from,effective_to,approval_reason)
VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,NULLIF($8,'')::uuid,NULLIF($9,'')::uuid,NULLIF($10,'')::uuid,$11,$12,NULLIF($13,'')) RETURNING *), audit AS (
INSERT INTO gateway_pool_events(gateway_pool_id,event_type,version,actor_id,reason,evidence)
SELECT id,'CREATED',version,$14::uuid,$15,jsonb_build_object('provider',provider,'engine',engine,'adapterVersion',adapter_version,'capabilities',capabilities,'status',status) FROM inserted)
SELECT `+gatewayPoolColumns+` FROM inserted`, value.Name, value.Provider, value.Engine, value.AdapterVersion, value.Status, string(raw), value.MinimumHealthyNodes, value.CreatedBy, value.SubmittedBy, value.ApprovedBy, value.EffectiveFrom, value.EffectiveTo, value.ApprovalReason, actor, reason)
	return scanGatewayPool(row)
}
func (p *PostgreSQLGovernanceStore) UpdateGatewayPool(ctx context.Context, poolID string, expected int64, value GatewayPool, actor, reason string) (GatewayPool, error) {
	raw, err := encodeCapabilities(value.Capabilities)
	if err != nil {
		return GatewayPool{}, err
	}
	row := p.DB.QueryRowContext(ctx, `WITH updated AS (
UPDATE gateway_pools SET name=$3,adapter_version=$4,status=$5,capabilities=$6::jsonb,minimum_healthy_nodes=$7,created_by=coalesce(created_by,NULLIF($8,'')::uuid),submitted_by=NULLIF($9,'')::uuid,approved_by=NULLIF($10,'')::uuid,effective_from=$11,effective_to=$12,approval_reason=NULLIF($13,''),version=version+1,updated_at=now()
WHERE id=$1::uuid AND version=$2 AND provider=$14 AND engine=$15 RETURNING *), audit AS (
INSERT INTO gateway_pool_events(gateway_pool_id,event_type,version,actor_id,reason,evidence)
SELECT id,'STATUS_'||status,version,$16::uuid,$17,jsonb_build_object('provider',provider,'engine',engine,'adapterVersion',adapter_version,'capabilities',capabilities,'status',status,'effectiveFrom',effective_from,'effectiveTo',effective_to) FROM updated)
SELECT `+gatewayPoolColumns+` FROM updated`, poolID, expected, value.Name, value.AdapterVersion, value.Status, string(raw), value.MinimumHealthyNodes, value.CreatedBy, value.SubmittedBy, value.ApprovedBy, value.EffectiveFrom, value.EffectiveTo, value.ApprovalReason, value.Provider, value.Engine, actor, reason)
	out, err := scanGatewayPool(row)
	if errors.Is(err, sql.ErrNoRows) {
		return GatewayPool{}, ErrSenderConflict
	}
	return out, err
}

func (p *PostgreSQLGovernanceStore) ListGatewayPoolEvents(ctx context.Context, poolID string, limit int) ([]GatewayPoolEvent, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,gateway_pool_id::text,event_type,version,coalesce(actor_id::text,''),reason,evidence,occurred_at FROM gateway_pool_events WHERE gateway_pool_id=$1::uuid ORDER BY occurred_at DESC,id DESC LIMIT $2`, poolID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GatewayPoolEvent{}
	for rows.Next() {
		var value GatewayPoolEvent
		var raw []byte
		if err := rows.Scan(&value.ID, &value.PoolID, &value.EventType, &value.Version, &value.ActorID, &value.Reason, &raw, &value.OccurredAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &value.Evidence); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (p *PostgreSQLGovernanceStore) UseRuntimeNonce(ctx context.Context, nodeID, nonce, requestHash string, expiresAt time.Time) error {
	if p == nil || p.DB == nil {
		return errors.New("database is required")
	}
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM gateway_runtime_nonces WHERE expires_at<=now()`); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO gateway_runtime_nonces(nonce,node_id,request_hash,expires_at) VALUES($1,$2::uuid,$3,$4) ON CONFLICT (nonce) DO NOTHING`, nonce, nodeID, requestHash, expiresAt)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrRuntimeReplay
	}
	return tx.Commit()
}

func runtimeIdentity(report RuntimeReport) map[string]any {
	return map[string]any{
		"provider": report.Provider, "engine": report.Engine, "adapterVersion": report.AdapterVersion,
		"gatewayVersion": report.GatewayVersion, "workerVersion": report.WorkerVersion,
		"configurationVersion": report.ConfigurationVersion, "capabilities": report.Capabilities,
		"runtimeState": report.RuntimeState, "internalUrl": report.InternalURL,
		"capacity": report.Capacity, "sessionCount": report.SessionCount,
		"queueDepth": report.QueueDepth, "cpuPercent": report.CPUPercent, "memoryBytes": report.MemoryBytes,
	}
}

func insertRuntimeEvent(ctx context.Context, tx *sql.Tx, event RuntimeEvent) error {
	if event.ID == "" {
		var err error
		event.ID, err = id.New()
		if err != nil {
			return err
		}
	}
	raw, err := json.Marshal(event.RuntimeIdentity)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO gateway_runtime_events(id,node_id,gateway_pool_id,event_type,node_version,boot_id,runtime_identity,request_hash,reason,occurred_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7::jsonb,NULLIF($8,''),$9,$10)`, event.ID, event.NodeID, event.GatewayPoolID, event.EventType, event.NodeVersion, event.BootID, string(raw), event.RequestHash, event.Reason, event.OccurredAt)
	return err
}

func (p *PostgreSQLGovernanceStore) ApplyRuntimeReport(ctx context.Context, nodeID string, expected int64, report RuntimeReport, requestHash string, now time.Time) (Node, error) {
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Node{}, err
	}
	defer tx.Rollback()
	var oldBoot string
	var oldRegistered sql.NullTime
	var currentVersion int64
	if err = tx.QueryRowContext(ctx, `SELECT coalesce(boot_id,''),registered_at,governance_version FROM sender_nodes WHERE id=$1::uuid FOR UPDATE`, nodeID).Scan(&oldBoot, &oldRegistered, &currentVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Node{}, ErrSenderNotFound
		}
		return Node{}, err
	}
	if currentVersion != expected {
		return Node{}, ErrSenderConflict
	}
	capabilities, err := encodeCapabilities(report.Capabilities)
	if err != nil {
		return Node{}, err
	}
	status := "READY"
	draining := false
	switch report.RuntimeState {
	case RuntimeDraining:
		status, draining = "DRAINING", true
	case RuntimeDegraded, RuntimeUnavailable:
		status = "UNHEALTHY"
	}
	registeredAt := now
	if oldBoot == report.BootID && oldRegistered.Valid {
		registeredAt = oldRegistered.Time.UTC()
	}
	row := tx.QueryRowContext(ctx, `UPDATE sender_nodes SET internal_url=$3,gateway_pool_id=$4::uuid,provider=$5,engine=$6,adapter_version=$7,boot_id=$8,status=$9,build_version=$10,gateway_version=$11,worker_version=$12,configuration_version=$13,runtime_capabilities=$14::jsonb,runtime_state=$15,capacity=$16,session_count=$17,queue_depth=$18,cpu_percent=$19,memory_bytes=$20,draining=$21,registered_at=$22,last_heartbeat_at=$23,governance_version=governance_version+1,updated_at=$23 WHERE id=$1::uuid AND governance_version=$2 RETURNING `+nodeColumns, nodeID, expected, report.InternalURL, report.GatewayPoolID, report.Provider, report.Engine, report.AdapterVersion, report.BootID, status, report.WorkerVersion, report.GatewayVersion, report.WorkerVersion, report.ConfigurationVersion, string(capabilities), string(report.RuntimeState), report.Capacity, report.SessionCount, report.QueueDepth, report.CPUPercent, report.MemoryBytes, draining, registeredAt, now)
	out, err := scanNode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrSenderConflict
	}
	if err != nil {
		return Node{}, err
	}
	eventType := "HEARTBEAT"
	if oldBoot != report.BootID || !oldRegistered.Valid {
		eventType = "REGISTERED"
	}
	if err = insertRuntimeEvent(ctx, tx, RuntimeEvent{NodeID: nodeID, GatewayPoolID: report.GatewayPoolID, EventType: eventType, NodeVersion: out.Version, BootID: report.BootID, RuntimeIdentity: runtimeIdentity(report), RequestHash: requestHash, Reason: "signed gateway runtime report accepted", OccurredAt: now}); err != nil {
		return Node{}, err
	}
	if err = tx.Commit(); err != nil {
		return Node{}, err
	}
	return out, nil
}

func (p *PostgreSQLGovernanceStore) RecordRuntimeRejection(ctx context.Context, nodeID string, report RuntimeReport, requestHash, reason string, now time.Time) error {
	// Rejections are recorded only where the governed node and pool both exist;
	// authentication failures must not create arbitrary evidence rows.
	var exists bool
	if err := p.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sender_nodes n JOIN gateway_pools gp ON gp.id=$2::uuid WHERE n.id=$1::uuid)`, nodeID, report.GatewayPoolID).Scan(&exists); err != nil || !exists {
		return err
	}
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = insertRuntimeEvent(ctx, tx, RuntimeEvent{NodeID: nodeID, GatewayPoolID: report.GatewayPoolID, EventType: "REJECTED", NodeVersion: report.ExpectedNodeVersion, BootID: report.BootID, RuntimeIdentity: runtimeIdentity(report), RequestHash: requestHash, Reason: reason, OccurredAt: now}); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *PostgreSQLGovernanceStore) ListRuntimeEvents(ctx context.Context, nodeID string, limit int) ([]RuntimeEvent, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,node_id::text,gateway_pool_id::text,event_type,node_version,boot_id,runtime_identity,coalesce(request_hash,''),reason,occurred_at FROM gateway_runtime_events WHERE node_id=$1::uuid ORDER BY occurred_at DESC,id DESC LIMIT $2`, nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RuntimeEvent{}
	for rows.Next() {
		var value RuntimeEvent
		var raw []byte
		if err := rows.Scan(&value.ID, &value.NodeID, &value.GatewayPoolID, &value.EventType, &value.NodeVersion, &value.BootID, &raw, &value.RequestHash, &value.Reason, &value.OccurredAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &value.RuntimeIdentity); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (p *PostgreSQLGovernanceStore) GatewayPoolUsage(ctx context.Context, poolID string, now time.Time) (GatewayPoolUsage, error) {
	var usage GatewayPoolUsage
	err := p.DB.QueryRowContext(ctx, `SELECT
  (SELECT count(*) FROM sender_nodes WHERE gateway_pool_id=$1::uuid AND status<>'RETIRED'),
  (SELECT count(*) FROM sender_sessions WHERE gateway_pool_id=$1::uuid AND status<>'RETIRED'),
  (SELECT count(DISTINCT c.id) FROM campaign_routing_plan_pools rpp JOIN campaign_routing_plans rp ON rp.id=rpp.routing_plan_id JOIN campaigns c ON c.id=rp.campaign_id WHERE rpp.gateway_pool_id=$1::uuid AND c.status NOT IN ('COMPLETED','COMPLETED_WITH_EXCEPTIONS','CANCELLED')),
  (SELECT count(*) FROM campaign_pool_capacity_reservations r JOIN campaign_routing_plan_pools rpp ON rpp.routing_plan_id=r.routing_plan_id AND rpp.sender_pool_id=r.sender_pool_id WHERE rpp.gateway_pool_id=$1::uuid AND r.status IN ('HELD','ACTIVE') AND r.reservation_end>$2)`, poolID, now).Scan(&usage.NonRetiredNodes, &usage.NonRetiredSessions, &usage.NonTerminalCampaigns, &usage.ActiveReservations)
	return usage, err
}
