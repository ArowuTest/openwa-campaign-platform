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
)

var (
	ErrSessionHeartbeatInvalid  = errors.New("sender session heartbeat is invalid")
	ErrSessionHeartbeatIdentity = errors.New("sender session heartbeat identity mismatch")
	ErrSessionHeartbeatStale    = errors.New("sender session heartbeat is stale")
)

type SessionHeartbeatReport struct {
	NodeID        string `json:"nodeId"`
	SessionID     string `json:"sessionId"`
	Status        Status `json:"status"`
	EngineVersion string `json:"engineVersion"`
	SentToday     int64  `json:"sentToday"`
}

type SessionHeartbeatService struct {
	Governance *GovernanceService
	Runtime    *RuntimeRegistrationService
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
	report.EngineVersion = strings.TrimSpace(report.EngineVersion)
	if report.NodeID == "" || report.SessionID == "" || report.SentToday < 0 {
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
	if s == nil || s.Governance == nil || s.Governance.Store == nil || s.Runtime == nil || s.Runtime.Store == nil {
		return GovernedSession{}, errors.New("sender session heartbeat dependencies are required")
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
	if current.LastHeartbeatAt != nil && !observedAt.After(current.LastHeartbeatAt.UTC()) {
		return GovernedSession{}, ErrSessionHeartbeatStale
	}
	return s.Governance.Store.HeartbeatSession(ctx, current.ID, current.Version, GovernedSession{
		Status: report.Status, EngineVersion: report.EngineVersion, SentToday: report.SentToday,
	}, observedAt)
}
