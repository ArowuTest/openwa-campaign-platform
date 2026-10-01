package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"campaign-platform/internal/gateway"
)

var (
	ErrSessionHeartbeatInvalid          = errors.New("sender session heartbeat is invalid")
	ErrSessionHeartbeatIdentity         = errors.New("sender session heartbeat identity mismatch")
	ErrSessionHeartbeatStale            = errors.New("sender session heartbeat is stale")
	ErrSessionHeartbeatRecoveryRequired = errors.New("sender session heartbeat requires controlled recovery before lease takeover")
)

type SessionHeartbeatReport struct {
	NodeID        string `json:"nodeId"`
	SessionID     string `json:"sessionId"`
	BootID        string `json:"bootId"`
	Status        Status `json:"status"`
	EngineVersion string `json:"engineVersion"`
	SentToday     int64  `json:"sentToday"`
}

type SessionHeartbeatService struct {
	Governance *GovernanceService
	Runtime    *RuntimeRegistrationService
	Leases     gateway.LeaseStore
	LeaseTTL   time.Duration
}

func DecodeSessionHeartbeatReport(raw []byte) (SessionHeartbeatReport, error) {
	var report SessionHeartbeatReport
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return SessionHeartbeatReport{}, fmt.Errorf("%w: %v", ErrSessionHeartbeatInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return SessionHeartbeatReport{}, fmt.Errorf("%w: trailing JSON", ErrSessionHeartbeatInvalid)
	} else if !errors.Is(err, io.EOF) {
		return SessionHeartbeatReport{}, fmt.Errorf("%w: %v", ErrSessionHeartbeatInvalid, err)
	}
	report.NodeID = strings.TrimSpace(report.NodeID)
	report.SessionID = strings.TrimSpace(report.SessionID)
	report.BootID = strings.TrimSpace(report.BootID)
	report.EngineVersion = strings.TrimSpace(report.EngineVersion)
	if report.NodeID == "" || report.SessionID == "" || report.BootID == "" || report.EngineVersion == "" || report.SentToday < 0 {
		return SessionHeartbeatReport{}, ErrSessionHeartbeatInvalid
	}
	if !machineHeartbeatStatusAllowed(report.Status) {
		return SessionHeartbeatReport{}, ErrSessionHeartbeatInvalid
	}
	return report, nil
}

func machineHeartbeatStatusAllowed(status Status) bool {
	switch status {
	case StatusPairing, StatusConnecting, StatusReady, StatusBusy, StatusDraining,
		StatusPaused, StatusDisconnected, StatusRecovering, StatusRecoveryFail, StatusRestricted:
		return true
	default:
		return false
	}
}

func (s *SessionHeartbeatService) Heartbeat(ctx context.Context, pathSessionID, timestamp, nonce, signature string, raw []byte) (GovernedSession, error) {
	if s == nil || s.Governance == nil || s.Governance.Store == nil || s.Runtime == nil || s.Runtime.Store == nil || s.Leases == nil || s.LeaseTTL <= 0 {
		return GovernedSession{}, errors.New("sender session heartbeat governance, runtime, lease store and positive lease TTL are required")
	}
	report, err := DecodeSessionHeartbeatReport(raw)
	if err != nil {
		return GovernedSession{}, err
	}
	if strings.TrimSpace(pathSessionID) != report.SessionID {
		return GovernedSession{}, ErrSessionHeartbeatIdentity
	}
	now := s.Runtime.now()
	maximumSkew := s.Runtime.MaximumSkew
	if maximumSkew <= 0 {
		maximumSkew = 5 * time.Minute
	}
	requestHash, observedAt, err := verifyRuntimeReportAny(
		append([][]byte{s.Runtime.Secret}, s.Runtime.PreviousSecrets...),
		timestamp, nonce, signature, raw, now, maximumSkew,
	)
	if err != nil {
		return GovernedSession{}, err
	}
	if err := s.Runtime.Store.UseRuntimeNonce(ctx, report.NodeID, nonce, requestHash, now.Add(maximumSkew)); err != nil {
		return GovernedSession{}, err
	}
	current, err := s.Governance.Store.GetSession(ctx, report.SessionID)
	if err != nil {
		return GovernedSession{}, err
	}
	if strings.TrimSpace(current.NodeID) == "" || current.NodeID != report.NodeID {
		return GovernedSession{}, ErrSessionHeartbeatIdentity
	}
	node, err := s.Governance.Store.GetNode(ctx, report.NodeID)
	if err != nil {
		return GovernedSession{}, err
	}
	if strings.TrimSpace(node.BootID) == "" || node.BootID != report.BootID {
		return GovernedSession{}, ErrSessionHeartbeatIdentity
	}
	if current.LastHeartbeatAt != nil && !observedAt.After(current.LastHeartbeatAt.UTC()) {
		return GovernedSession{}, ErrSessionHeartbeatStale
	}
	store, ok := s.Governance.Store.(atomicSessionHeartbeatStore)
	if !ok {
		return GovernedSession{}, errors.New("atomic sender session heartbeat storage is required")
	}
	// Initial reads are advisory. Storage rechecks boot, session version and
	// observation order under the same lock/transaction as both mutations.
	// A failed update must roll back ownership, never DELETE its fence history.
	return store.ApplyOwnedSessionHeartbeat(ctx, current, report, observedAt, now, s.LeaseTTL, s.Leases)
}

func (s *SessionHeartbeatService) ensureSessionLease(ctx context.Context, current GovernedSession, report SessionHeartbeatReport, now time.Time) (gateway.Lease, bool, error) {
	existing, ok, err := s.Leases.Get(ctx, current.ID)
	if err != nil {
		return gateway.Lease{}, false, err
	}
	if !ok {
		lease, err := s.Leases.Acquire(ctx, current.ID, report.NodeID, report.BootID, now, s.LeaseTTL)
		return lease, err == nil, err
	}
	if existing.ExpiresAt.After(now) {
		if existing.WorkerID != report.NodeID {
			return gateway.Lease{}, false, gateway.ErrLeaseHeld
		}
		existing.Token = report.BootID
		lease, err := s.Leases.Renew(ctx, existing, now, s.LeaseTTL)
		if errors.Is(err, gateway.ErrLeaseLost) {
			return gateway.Lease{}, false, gateway.ErrLeaseHeld
		}
		return lease, false, err
	}
	if !safeExpiredLeaseTakeover(current.Status, report.Status) {
		return gateway.Lease{}, false, ErrSessionHeartbeatRecoveryRequired
	}
	lease, err := s.Leases.Acquire(ctx, current.ID, report.NodeID, report.BootID, now, s.LeaseTTL)
	return lease, err == nil, err
}

func safeExpiredLeaseTakeover(current, reported Status) bool {
	if reported == StatusReady || reported == StatusBusy {
		return false
	}
	return current == reported || AllowedSessionTransition(current, reported)
}
