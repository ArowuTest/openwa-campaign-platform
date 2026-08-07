package sender

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/platformpolicy"
)

type SessionGatewayResult struct {
	SessionID string         `json:"sessionId"`
	Status    string         `json:"status,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

type SessionGateway interface {
	Create(context.Context, Node, GovernedSession) (SessionGatewayResult, error)
	Start(context.Context, Node, GovernedSession, *SessionProxyConfiguration) (SessionGatewayResult, error)
	Stop(context.Context, Node, GovernedSession) (SessionGatewayResult, error)
	Logout(context.Context, Node, GovernedSession) (SessionGatewayResult, error)
	Delete(context.Context, Node, GovernedSession) error
	QR(context.Context, Node, GovernedSession) (SessionGatewayResult, error)
	PairingCode(context.Context, Node, GovernedSession, string) (SessionGatewayResult, error)
	Drain(context.Context, Node, GovernedSession) error
	Resume(context.Context, Node, GovernedSession) error
	Health(context.Context, Node, GovernedSession) (SessionGatewayResult, error)
}

type ActiveSessionWorkChecker interface {
	HasActiveSessionWork(context.Context, string) (bool, error)
}

type SessionLifecycleService struct {
	Governance *GovernanceService
	Gateway    SessionGateway
	Proxies    interface {
		Resolve(context.Context, string) (*SessionProxyConfiguration, error)
	}
	ActiveWork  ActiveSessionWorkChecker
	Maintenance *platformpolicy.MaintenanceAdministration
	Clock       func() time.Time
}

func (s *SessionLifecycleService) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func (s *SessionLifecycleService) load(ctx context.Context, id string) (GovernedSession, Node, error) {
	if s == nil || s.Governance == nil || s.Governance.Store == nil || s.Gateway == nil {
		return GovernedSession{}, Node{}, errors.New("session lifecycle dependencies are required")
	}
	session, err := s.Governance.Store.GetSession(ctx, strings.TrimSpace(id))
	if err != nil {
		return GovernedSession{}, Node{}, err
	}
	if strings.TrimSpace(session.NodeID) == "" {
		return GovernedSession{}, Node{}, errors.New("session has no owner node")
	}
	node, err := s.Governance.Store.GetNode(ctx, session.NodeID)
	if err != nil {
		return GovernedSession{}, Node{}, err
	}
	if strings.TrimSpace(node.InternalURL) == "" {
		return GovernedSession{}, Node{}, errors.New("owner node has no governed internal URL")
	}
	if session.GatewayPoolID != "" && node.GatewayPoolID != "" && session.GatewayPoolID != node.GatewayPoolID {
		return GovernedSession{}, Node{}, errors.New("session and owner node gateway pools do not match")
	}
	if node.Engine != "" && strings.ToUpper(session.EngineType) != strings.ToUpper(node.Engine) {
		return GovernedSession{}, Node{}, errors.New("session and owner node engines do not match")
	}
	return session, node, nil
}

func (s *SessionLifecycleService) loadForChange(ctx context.Context, id string) (GovernedSession, Node, error) {
	session, node, err := s.load(ctx, id)
	if err != nil {
		return GovernedSession{}, Node{}, err
	}
	if s.Maintenance != nil {
		err = s.Maintenance.Check(ctx, platformpolicy.OperationSessionChange, platformpolicy.OperationalScope{Provider: node.Provider, GatewayPoolID: node.GatewayPoolID, SenderPoolID: session.PoolID}, s.now())
		if err != nil {
			return GovernedSession{}, Node{}, err
		}
	}
	return session, node, nil
}
func validateLifecycleInput(expected int64, actor, reason string) error {
	if expected <= 0 || strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 8 {
		return errors.New("expected version, actor and a meaningful reason are required")
	}
	return nil
}

func (s *SessionLifecycleService) Create(ctx context.Context, id string, expected int64, actor, reason string) (GovernedSession, SessionGatewayResult, error) {
	if err := validateLifecycleInput(expected, actor, reason); err != nil {
		return GovernedSession{}, SessionGatewayResult{}, err
	}
	session, node, err := s.loadForChange(ctx, id)
	if err != nil {
		return GovernedSession{}, SessionGatewayResult{}, err
	}
	if session.Version != expected || session.Status != StatusNew {
		return GovernedSession{}, SessionGatewayResult{}, ErrSenderConflict
	}
	result, err := s.Gateway.Create(ctx, node, session)
	if err != nil {
		return GovernedSession{}, SessionGatewayResult{}, err
	}
	updated, err := s.Governance.TransitionSession(ctx, id, expected, StatusPairing, actor, reason)
	return updated, result, err
}

func (s *SessionLifecycleService) QR(ctx context.Context, id string) (SessionGatewayResult, error) {
	session, node, err := s.loadForChange(ctx, id)
	if err != nil {
		return SessionGatewayResult{}, err
	}
	if session.Status != StatusPairing && session.Status != StatusDisconnected {
		return SessionGatewayResult{}, fmt.Errorf("QR pairing is not permitted while session is %s", session.Status)
	}
	return s.Gateway.QR(ctx, node, session)
}

func (s *SessionLifecycleService) PairingCode(ctx context.Context, id, phone string) (SessionGatewayResult, error) {
	session, node, err := s.loadForChange(ctx, id)
	if err != nil {
		return SessionGatewayResult{}, err
	}
	if session.Status != StatusPairing && session.Status != StatusDisconnected {
		return SessionGatewayResult{}, fmt.Errorf("pairing code is not permitted while session is %s", session.Status)
	}
	phone = strings.TrimPrefix(strings.TrimSpace(phone), "+")
	if len(phone) < 8 || len(phone) > 15 {
		return SessionGatewayResult{}, errors.New("pairing phone number must contain 8 to 15 digits")
	}
	for _, r := range phone {
		if r < '0' || r > '9' {
			return SessionGatewayResult{}, errors.New("pairing phone number must contain digits only")
		}
	}
	return s.Gateway.PairingCode(ctx, node, session, phone)
}

func (s *SessionLifecycleService) Start(ctx context.Context, id string, expected int64, actor, reason string) (GovernedSession, SessionGatewayResult, error) {
	if err := validateLifecycleInput(expected, actor, reason); err != nil {
		return GovernedSession{}, SessionGatewayResult{}, err
	}
	session, node, err := s.loadForChange(ctx, id)
	if err != nil {
		return GovernedSession{}, SessionGatewayResult{}, err
	}
	if session.Version != expected {
		return GovernedSession{}, SessionGatewayResult{}, ErrSenderConflict
	}
	if session.Status != StatusPairing && session.Status != StatusDisconnected && session.Status != StatusPaused && session.Status != StatusRecovering {
		return GovernedSession{}, SessionGatewayResult{}, fmt.Errorf("session cannot start from %s", session.Status)
	}
	var proxy *SessionProxyConfiguration
	if s.Proxies != nil {
		proxy, err = s.Proxies.Resolve(ctx, session.ID)
		if err != nil {
			return GovernedSession{}, SessionGatewayResult{}, fmt.Errorf("resolve governed session proxy: %w", err)
		}
	}
	result, err := s.Gateway.Start(ctx, node, session, proxy)
	if err != nil {
		return GovernedSession{}, SessionGatewayResult{}, err
	}
	updated, err := s.Governance.TransitionSession(ctx, id, expected, StatusConnecting, actor, reason)
	return updated, result, err
}

func (s *SessionLifecycleService) Drain(ctx context.Context, id string, expected int64, actor, reason string) (GovernedSession, error) {
	if err := validateLifecycleInput(expected, actor, reason); err != nil {
		return GovernedSession{}, err
	}
	session, node, err := s.loadForChange(ctx, id)
	if err != nil {
		return GovernedSession{}, err
	}
	if session.Version != expected || (session.Status != StatusReady && session.Status != StatusBusy) {
		return GovernedSession{}, ErrSenderConflict
	}
	if err := s.Gateway.Drain(ctx, node, session); err != nil {
		return GovernedSession{}, err
	}
	return s.Governance.TransitionSession(ctx, id, expected, StatusDraining, actor, reason)
}

func (s *SessionLifecycleService) Resume(ctx context.Context, id string, expected int64, actor, reason string) (GovernedSession, error) {
	if err := validateLifecycleInput(expected, actor, reason); err != nil {
		return GovernedSession{}, err
	}
	session, node, err := s.loadForChange(ctx, id)
	if err != nil {
		return GovernedSession{}, err
	}
	if session.Version != expected || (session.Status != StatusPaused && session.Status != StatusDraining) {
		return GovernedSession{}, ErrSenderConflict
	}
	if err := s.Gateway.Resume(ctx, node, session); err != nil {
		return GovernedSession{}, err
	}
	return s.Governance.TransitionSession(ctx, id, expected, StatusConnecting, actor, reason)
}

func (s *SessionLifecycleService) Stop(ctx context.Context, id string, expected int64, actor, reason string) (GovernedSession, SessionGatewayResult, error) {
	if err := validateLifecycleInput(expected, actor, reason); err != nil {
		return GovernedSession{}, SessionGatewayResult{}, err
	}
	session, node, err := s.loadForChange(ctx, id)
	if err != nil {
		return GovernedSession{}, SessionGatewayResult{}, err
	}
	if session.Version != expected || !AllowedSessionTransition(session.Status, StatusDisconnected) {
		return GovernedSession{}, SessionGatewayResult{}, ErrSenderConflict
	}
	result, err := s.Gateway.Stop(ctx, node, session)
	if err != nil {
		return GovernedSession{}, SessionGatewayResult{}, err
	}
	updated, err := s.Governance.TransitionSession(ctx, id, expected, StatusDisconnected, actor, reason)
	return updated, result, err
}

func (s *SessionLifecycleService) assertNoActiveWork(ctx context.Context, id string) error {
	if s.ActiveWork == nil {
		return errors.New("active session work checker is required for destructive operations")
	}
	active, err := s.ActiveWork.HasActiveSessionWork(ctx, id)
	if err != nil {
		return err
	}
	if active {
		return errors.New("session still owns active campaign work")
	}
	return nil
}

func (s *SessionLifecycleService) Logout(ctx context.Context, id string, expected int64, actor, reason string) (GovernedSession, SessionGatewayResult, error) {
	if err := validateLifecycleInput(expected, actor, reason); err != nil {
		return GovernedSession{}, SessionGatewayResult{}, err
	}
	if err := s.assertNoActiveWork(ctx, id); err != nil {
		return GovernedSession{}, SessionGatewayResult{}, err
	}
	session, node, err := s.loadForChange(ctx, id)
	if err != nil {
		return GovernedSession{}, SessionGatewayResult{}, err
	}
	if session.Version != expected || !AllowedSessionTransition(session.Status, StatusPairing) {
		return GovernedSession{}, SessionGatewayResult{}, ErrSenderConflict
	}
	result, err := s.Gateway.Logout(ctx, node, session)
	if err != nil {
		return GovernedSession{}, SessionGatewayResult{}, err
	}
	updated, err := s.Governance.TransitionSession(ctx, id, expected, StatusPairing, actor, reason)
	return updated, result, err
}

func (s *SessionLifecycleService) Delete(ctx context.Context, id string, expected int64, actor, reason string) (GovernedSession, error) {
	if err := validateLifecycleInput(expected, actor, reason); err != nil {
		return GovernedSession{}, err
	}
	if err := s.assertNoActiveWork(ctx, id); err != nil {
		return GovernedSession{}, err
	}
	session, node, err := s.loadForChange(ctx, id)
	if err != nil {
		return GovernedSession{}, err
	}
	if session.Version != expected || !AllowedSessionTransition(session.Status, StatusRetired) {
		return GovernedSession{}, ErrSenderConflict
	}
	if err := s.Gateway.Delete(ctx, node, session); err != nil {
		return GovernedSession{}, err
	}
	return s.Governance.TransitionSession(ctx, id, expected, StatusRetired, actor, reason)
}

func (s *SessionLifecycleService) Health(ctx context.Context, id string) (SessionGatewayResult, error) {
	session, node, err := s.load(ctx, id)
	if err != nil {
		return SessionGatewayResult{}, err
	}
	return s.Gateway.Health(ctx, node, session)
}

type StaticActiveSessionWorkChecker bool

func (s StaticActiveSessionWorkChecker) HasActiveSessionWork(context.Context, string) (bool, error) {
	return bool(s), nil
}
