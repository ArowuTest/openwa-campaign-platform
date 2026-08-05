package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/sender"
	sharedcrypto "campaign-platform/internal/shared/crypto"
	"campaign-platform/internal/shared/httpx"
)

func (s *Server) senderActor(r *http.Request) (string, bool) {
	p, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		return "", false
	}
	return p.User.ID, true
}
func (s *Server) requireSenderGovernance(w http.ResponseWriter, r *http.Request) bool {
	if s.deps.SenderGovernance == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "SENDER_GOVERNANCE_UNAVAILABLE", "Sender governance is unavailable.", nil)
		return false
	}
	return true
}

func (s *Server) requireSenderStepUp(w http.ResponseWriter, r *http.Request) bool {
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required.", nil)
		return false
	}
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		if s.deps.Identity != nil {
			s.deps.Identity.RecordStepUpRequired(r.Context(), principal.User.ID, "sender.session.quarantine", authenticationAttempt(r, principal.User.Email))
		}
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for this sender action.", nil)
		return false
	}
	return true
}

func (s *Server) listSenderPools(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) {
		return
	}
	v, err := s.deps.SenderGovernance.Store.ListPools(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": v, "count": len(v)})
}

type senderPoolRequest struct {
	Name                 string `json:"name"`
	OrganisationID       string `json:"organisationId"`
	Status               string `json:"status"`
	MaxMessagesPerMinute int    `json:"maxMessagesPerMinute"`
	DailyCapacity        int64  `json:"dailyCapacity"`
	ReservedCapacity     int64  `json:"reservedCapacity"`
	ExpectedVersion      int64  `json:"expectedVersion"`
	Reason               string `json:"reason"`
}

func (s *Server) createSenderPool(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) {
		return
	}
	var in senderPoolRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The sender-pool request is invalid.", nil)
		return
	}
	actor, _ := s.senderActor(r)
	v, err := s.deps.SenderGovernance.CreatePool(r.Context(), sender.Pool{Name: in.Name, OrganisationID: in.OrganisationID, Status: in.Status, MaxMessagesPerMinute: in.MaxMessagesPerMinute, DailyCapacity: in.DailyCapacity, ReservedCapacity: in.ReservedCapacity}, actor, in.Reason)
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 201, v)
}
func (s *Server) updateSenderPool(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) {
		return
	}
	var in senderPoolRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The sender-pool update is invalid.", nil)
		return
	}
	actor, _ := s.senderActor(r)
	v, err := s.deps.SenderGovernance.UpdatePool(r.Context(), r.PathValue("id"), in.ExpectedVersion, sender.Pool{Name: in.Name, Status: in.Status, MaxMessagesPerMinute: in.MaxMessagesPerMinute, DailyCapacity: in.DailyCapacity, ReservedCapacity: in.ReservedCapacity}, actor, in.Reason)
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, v)
}
func (s *Server) getSenderPoolCapacity(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) {
		return
	}
	v, err := s.deps.SenderGovernance.Store.Capacity(r.Context(), r.PathValue("id"), time.Now().UTC())
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, v)
}
func (s *Server) listSenderNodes(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) {
		return
	}
	v, err := s.deps.SenderGovernance.Store.ListNodes(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": v, "count": len(v)})
}

type senderNodeRequest struct {
	Name            string `json:"name"`
	PublicIP        string `json:"publicIp"`
	Status          string `json:"status"`
	BuildVersion    string `json:"buildVersion"`
	Capacity        int    `json:"capacity"`
	QueueDepth      int64  `json:"queueDepth"`
	Draining        bool   `json:"draining"`
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason"`
}

func (s *Server) registerSenderNode(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) {
		return
	}
	var in senderNodeRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The sender-node request is invalid.", nil)
		return
	}
	actor, _ := s.senderActor(r)
	v, err := s.deps.SenderGovernance.RegisterNode(r.Context(), sender.Node{Name: in.Name, PublicIP: in.PublicIP, Status: in.Status, BuildVersion: in.BuildVersion, Capacity: in.Capacity, QueueDepth: in.QueueDepth, Draining: in.Draining}, actor, in.Reason)
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 201, v)
}
func (s *Server) heartbeatSenderNode(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) {
		return
	}
	var in senderNodeRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The node heartbeat is invalid.", nil)
		return
	}
	v, err := s.deps.SenderGovernance.Store.HeartbeatNode(r.Context(), r.PathValue("id"), in.ExpectedVersion, sender.Node{Status: strings.ToUpper(in.Status), BuildVersion: in.BuildVersion, Capacity: in.Capacity, QueueDepth: in.QueueDepth, Draining: in.Draining}, time.Now().UTC())
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, v)
}
func (s *Server) listSenderSessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) {
		return
	}
	v, err := s.deps.SenderGovernance.Store.ListSessions(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": v, "count": len(v)})
}

func (s *Server) getSenderSessionHealth(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) {
		return
	}
	v, err := s.deps.SenderGovernance.AssessSession(r.Context(), r.PathValue("id"), time.Now().UTC())
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

type senderSessionRequest struct {
	NodeID                string        `json:"nodeId"`
	PoolID                string        `json:"poolId"`
	MSISDN                string        `json:"msisdn"`
	EngineType            string        `json:"engineType"`
	EngineVersion         string        `json:"engineVersion"`
	Status                sender.Status `json:"status"`
	SafeMessagesPerMinute int           `json:"safeMessagesPerMinute"`
	SafeDailyCapacity     int64         `json:"safeDailyCapacity"`
	InFlightLimit         int           `json:"inFlightLimit"`
	SentToday             int64         `json:"sentToday"`
	ExpectedVersion       int64         `json:"expectedVersion"`
	Reason                string        `json:"reason"`
}

func (s *Server) registerSenderSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) || s.deps.MSISDNProtector == nil {
		return
	}
	var in senderSessionRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The sender-session request is invalid.", nil)
		return
	}
	msisdn := strings.TrimSpace(in.MSISDN)
	if !strings.HasPrefix(msisdn, "+") {
		httpx.WriteError(w, r, 422, "INVALID_MSISDN", "A canonical E.164 sender MSISDN is required.", nil)
		return
	}
	cipher, err := s.deps.MSISDNProtector.Encrypt(msisdn)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	actor, _ := s.senderActor(r)
	v, err := s.deps.SenderGovernance.RegisterSession(r.Context(), sender.GovernedSession{NodeID: in.NodeID, PoolID: in.PoolID, MaskedMSISDN: sharedcrypto.Mask(msisdn), EngineType: in.EngineType, EngineVersion: in.EngineVersion, Status: in.Status, SafeMessagesPerMinute: in.SafeMessagesPerMinute, SafeDailyCapacity: in.SafeDailyCapacity, InFlightLimit: in.InFlightLimit}, cipher, actor, in.Reason)
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 201, v)
}
func (s *Server) transitionSenderSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) {
		return
	}
	var in senderSessionRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The sender transition is invalid.", nil)
		return
	}
	actor, _ := s.senderActor(r)
	v, err := s.deps.SenderGovernance.TransitionSession(r.Context(), r.PathValue("id"), in.ExpectedVersion, in.Status, actor, in.Reason)
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, v)
}

func (s *Server) quarantineSenderSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) || !s.requireSenderStepUp(w, r) {
		return
	}
	var in senderSessionRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The sender quarantine request is invalid.", nil)
		return
	}
	actor, _ := s.senderActor(r)
	v, err := s.deps.SenderGovernance.QuarantineSession(r.Context(), r.PathValue("id"), in.ExpectedVersion, actor, in.Reason)
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (s *Server) reinstateSenderSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) || !s.requireSenderStepUp(w, r) {
		return
	}
	var in senderSessionRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The sender reinstatement request is invalid.", nil)
		return
	}
	actor, _ := s.senderActor(r)
	v, err := s.deps.SenderGovernance.ReinstateSession(r.Context(), r.PathValue("id"), in.ExpectedVersion, actor, in.Reason)
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (s *Server) heartbeatSenderSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) {
		return
	}
	var in senderSessionRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The session heartbeat is invalid.", nil)
		return
	}
	v, err := s.deps.SenderGovernance.Store.HeartbeatSession(r.Context(), r.PathValue("id"), in.ExpectedVersion, sender.GovernedSession{Status: in.Status, EngineVersion: in.EngineVersion, SafeMessagesPerMinute: in.SafeMessagesPerMinute, SafeDailyCapacity: in.SafeDailyCapacity, InFlightLimit: in.InFlightLimit, SentToday: in.SentToday}, time.Now().UTC())
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, v)
}
func (s *Server) writeSenderError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, sender.ErrSenderNotFound):
		httpx.WriteError(w, r, 404, "SENDER_NOT_FOUND", "The sender resource does not exist.", nil)
	case errors.Is(err, sender.ErrSenderConflict):
		httpx.WriteError(w, r, 409, "SENDER_VERSION_CONFLICT", "The sender resource changed; reload before retrying.", nil)
	default:
		httpx.WriteError(w, r, 422, "SENDER_GOVERNANCE_INVALID", err.Error(), nil)
	}
}
