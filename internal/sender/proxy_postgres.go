package sender

import (
	"context"
	"database/sql"
	"errors"
)

func (p *PostgreSQLGovernanceStore) ConfigureSessionProxy(ctx context.Context, sessionID string, expected int64, ciphertext []byte, actor, reason string) (SessionProxyStatus, error) {
	var version int64
	err := p.DB.QueryRowContext(ctx, `WITH updated AS (
UPDATE sender_sessions
SET proxy_configuration_ciphertext=$3, governance_version=governance_version+1, updated_at=now()
WHERE id=$1::uuid AND governance_version=$2
  AND status IN ('NEW','PAIRING','PAUSED','DISCONNECTED','RECOVERING','FAILED_RECOVERY','RESTRICTED')
RETURNING id,governance_version
), audit AS (
INSERT INTO sender_governance_events(object_type,object_id,action,actor_id,reason,object_version)
SELECT 'SESSION',id,'PROXY_CONFIGURED',$4::uuid,$5,governance_version FROM updated
)
SELECT governance_version FROM updated`, sessionID, expected, ciphertext, actor, reason).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionProxyStatus{}, ErrSenderConflict
	}
	return SessionProxyStatus{SessionID: sessionID, Configured: true, Version: version}, err
}
func (p *PostgreSQLGovernanceStore) ClearSessionProxy(ctx context.Context, sessionID string, expected int64, actor, reason string) (SessionProxyStatus, error) {
	var version int64
	err := p.DB.QueryRowContext(ctx, `WITH updated AS (
UPDATE sender_sessions
SET proxy_configuration_ciphertext=NULL, governance_version=governance_version+1, updated_at=now()
WHERE id=$1::uuid AND governance_version=$2
  AND status IN ('NEW','PAIRING','PAUSED','DISCONNECTED','RECOVERING','FAILED_RECOVERY','RESTRICTED')
RETURNING id,governance_version
), audit AS (
INSERT INTO sender_governance_events(object_type,object_id,action,actor_id,reason,object_version)
SELECT 'SESSION',id,'PROXY_CLEARED',$3::uuid,$4,governance_version FROM updated
)
SELECT governance_version FROM updated`, sessionID, expected, actor, reason).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionProxyStatus{}, ErrSenderConflict
	}
	return SessionProxyStatus{SessionID: sessionID, Configured: false, Version: version}, err
}

func (p *PostgreSQLGovernanceStore) LoadSessionProxy(ctx context.Context, sessionID string) ([]byte, int64, error) {
	var raw []byte
	var version int64
	err := p.DB.QueryRowContext(ctx, `SELECT proxy_configuration_ciphertext,governance_version FROM sender_sessions WHERE id=$1::uuid`, sessionID).Scan(&raw, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrSenderNotFound
	}
	return raw, version, err
}
