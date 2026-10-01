package sender

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"campaign-platform/internal/gateway"
)

// The authenticated service must use a storage boundary that serializes the
// node boot, session version and lease. Separate reads followed by independent
// writes cannot fence a heartbeat against a concurrently replaced process.
// SessionOwnershipReceipt is returned only with a committed signed heartbeat.
// It is not stored as sender metadata and carries no reusable secret.
type SessionOwnershipReceipt struct {
	NodeID         string    `json:"nodeId"`
	SessionID      string    `json:"sessionId"`
	BootID         string    `json:"bootId"`
	LeaseVersion   int64     `json:"leaseVersion"`
	LeaseExpiresAt time.Time `json:"leaseExpiresAt"`
	ServerNow      time.Time `json:"serverNow"`
}

func heartbeatOwnershipReceipt(report SessionHeartbeatReport, lease gateway.Lease, now time.Time) *SessionOwnershipReceipt {
	return &SessionOwnershipReceipt{NodeID: report.NodeID, SessionID: report.SessionID, BootID: report.BootID, LeaseVersion: lease.Version, LeaseExpiresAt: lease.ExpiresAt.UTC(), ServerNow: now.UTC()}
}

type atomicSessionHeartbeatStore interface {
	ApplyOwnedSessionHeartbeat(context.Context, GovernedSession, SessionHeartbeatReport, time.Time, time.Time, time.Duration, gateway.LeaseStore) (GovernedSession, error)
}
type sessionHeartbeatSQLStore interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func validateLockedHeartbeat(current, expected GovernedSession, node Node, report SessionHeartbeatReport, observedAt time.Time) error {
	if current.ID != report.SessionID || current.NodeID != report.NodeID || node.ID != report.NodeID || node.BootID == "" || node.BootID != report.BootID {
		return ErrSessionHeartbeatIdentity
	}
	// Gateway creation makes the local record visible before the operator's
	// NEW -> PAIRING transition commits. Defer machine telemetry until then:
	// taking its version here would strand creation and mint premature ownership.
	if current.Status == StatusNew || current.Version != expected.Version {
		return ErrSenderConflict
	}
	if current.LastHeartbeatAt != nil && !observedAt.After(current.LastHeartbeatAt.UTC()) {
		return ErrSessionHeartbeatStale
	}
	return nil
}

func (p *PostgreSQLGovernanceStore) ApplyOwnedSessionHeartbeat(ctx context.Context, expected GovernedSession, report SessionHeartbeatReport, observedAt, now time.Time, ttl time.Duration, configured gateway.LeaseStore) (GovernedSession, error) {
	leases, ok := configured.(*gateway.PostgreSQLLeaseStore)
	if p == nil || p.DB == nil || !ok || leases == nil || leases.DB != p.DB || ttl <= 0 {
		return GovernedSession{}, errors.New("session governance and leases must share one PostgreSQL database")
	}
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return GovernedSession{}, err
	}
	defer tx.Rollback()

	// Runtime registration takes a conflicting UPDATE lock on this node. Once
	// its replacement boot commits, an earlier heartbeat must see the new boot;
	// once this lock is held, replacement cannot commit ahead of this heartbeat.
	var node Node
	err = tx.QueryRowContext(ctx, `SELECT id::text,coalesce(boot_id,'') FROM sender_nodes WHERE id=$1::uuid FOR SHARE`, report.NodeID).Scan(&node.ID, &node.BootID)
	if errors.Is(err, sql.ErrNoRows) {
		return GovernedSession{}, ErrSenderNotFound
	}
	if err != nil {
		return GovernedSession{}, err
	}
	current, err := scanGovernedSession(tx.QueryRowContext(ctx, `SELECT `+governedSessionColumns+` FROM sender_sessions WHERE id=$1::uuid FOR UPDATE`, report.SessionID))
	if errors.Is(err, sql.ErrNoRows) {
		return GovernedSession{}, ErrSenderNotFound
	}
	if err != nil {
		return GovernedSession{}, err
	}
	if err = validateLockedHeartbeat(current, expected, node, report, observedAt); err != nil {
		return GovernedSession{}, err
	}

	owner := &SessionHeartbeatService{Leases: &gateway.PostgreSQLLeaseStore{DB: tx}, LeaseTTL: ttl}
	lease, acquired, err := owner.ensureSessionLease(ctx, current, report, now)
	if err != nil {
		return GovernedSession{}, err
	}
	updated, err := heartbeatPostgreSQLSession(ctx, tx, current.ID, current.Version, GovernedSession{
		Status: report.Status, EngineVersion: report.EngineVersion, SentToday: report.SentToday,
	}, observedAt)
	if err != nil {
		return GovernedSession{}, err
	}
	if acquired && lease.Version > 1 && (updated.Status == StatusReady || updated.Status == StatusBusy) {
		return GovernedSession{}, ErrSessionHeartbeatRecoveryRequired
	}
	// Failure rolls back both mutations, leaving the previously durable lease
	// row and its high-water version intact. No compensating DELETE is allowed.
	if err = tx.Commit(); err != nil {
		return GovernedSession{}, err
	}
	updated.Ownership = heartbeatOwnershipReceipt(report, lease, now)
	return updated, nil
}

func (m *MemoryGovernanceStore) ApplyOwnedSessionHeartbeat(ctx context.Context, expected GovernedSession, report SessionHeartbeatReport, observedAt, now time.Time, ttl time.Duration, leases gateway.LeaseStore) (GovernedSession, error) {
	memoryLeases, ok := leases.(*gateway.MemoryLeaseStore)
	if m == nil || !ok || memoryLeases == nil || ttl <= 0 {
		return GovernedSession{}, errors.New("memory session governance requires an in-memory lease store")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	node, ok := m.nodes[report.NodeID]
	if !ok {
		return GovernedSession{}, ErrSenderNotFound
	}
	current, ok := m.sessions[report.SessionID]
	if !ok {
		return GovernedSession{}, ErrSenderNotFound
	}
	if err := validateLockedHeartbeat(current, expected, node, report, observedAt); err != nil {
		return GovernedSession{}, err
	}
	owner := &SessionHeartbeatService{Leases: memoryLeases, LeaseTTL: ttl}
	lease, _, err := owner.ensureSessionLease(ctx, current, report, now)
	if err != nil {
		return GovernedSession{}, err
	}
	// No fallible external work remains. Holding mu prevents version/boot changes
	// between the checks, lease mutation, and this in-memory session assignment.
	updated, err := m.heartbeatSessionLocked(current.ID, current.Version, GovernedSession{
		Status: report.Status, EngineVersion: report.EngineVersion, SentToday: report.SentToday,
	}, observedAt)
	if err != nil {
		return GovernedSession{}, err
	}
	updated.Ownership = heartbeatOwnershipReceipt(report, lease, now)
	return updated, nil
}
