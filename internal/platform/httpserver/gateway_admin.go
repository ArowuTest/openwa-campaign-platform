package httpserver

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"campaign-platform/internal/sender"
	"campaign-platform/internal/shared/httpx"
)

type gatewayPoolRequest struct {
	Name                string                 `json:"name"`
	Provider            sender.GatewayProvider `json:"provider"`
	Engine              sender.GatewayEngine   `json:"engine"`
	AdapterVersion      string                 `json:"adapterVersion"`
	Capabilities        []sender.Capability    `json:"capabilities"`
	MinimumHealthyNodes int                    `json:"minimumHealthyNodes"`
	ExpectedVersion     int64                  `json:"expectedVersion"`
	Approve             bool                   `json:"approve"`
	EffectiveFrom       *time.Time             `json:"effectiveFrom"`
	Reason              string                 `json:"reason"`
}

func (s *Server) requireGatewayPools(w http.ResponseWriter, r *http.Request) (*sender.GatewayPoolAdministration, bool) {
	if s.deps.GatewayPools == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "GATEWAY_POOL_GOVERNANCE_UNAVAILABLE", "Gateway-pool governance is unavailable.", nil)
		return nil, false
	}
	return s.deps.GatewayPools, true
}
func (s *Server) listGatewayPools(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireGatewayPools(w, r)
	if !ok {
		return
	}
	request, err := httpx.ParsePage(r, 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE", "The gateway-pool page request is invalid.", nil)
		return
	}
	page, err := admin.ListPage(r.Context(), request.Limit, request.Cursor)
	if errors.Is(err, sender.ErrInvalidInventoryCursor) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The gateway-pool page cursor is invalid.", nil)
		return
	}
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}
func (s *Server) getGatewayPool(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireGatewayPools(w, r)
	if !ok {
		return
	}
	value, err := admin.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
func (s *Server) createGatewayPool(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireGatewayPools(w, r)
	if !ok {
		return
	}
	var input gatewayPoolRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The gateway-pool request is invalid.", nil)
		return
	}
	value, err := admin.Create(r.Context(), sender.GatewayPool{Name: input.Name, Provider: input.Provider, Engine: input.Engine, AdapterVersion: input.AdapterVersion, Capabilities: input.Capabilities, MinimumHealthyNodes: input.MinimumHealthyNodes}, platformPolicyActor(r), input.Reason)
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, value)
}
func (s *Server) submitGatewayPool(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireGatewayPools(w, r)
	if !ok {
		return
	}
	var input gatewayPoolRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The gateway-pool submission is invalid.", nil)
		return
	}
	value, err := admin.Submit(r.Context(), r.PathValue("id"), input.ExpectedVersion, platformPolicyActor(r), input.Reason)
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
func (s *Server) decideGatewayPool(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireGatewayPools(w, r)
	if !ok || !s.requirePlatformPolicyStepUp(w, r, "gateway_pool.decision") {
		return
	}
	var input gatewayPoolRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The gateway-pool decision is invalid.", nil)
		return
	}
	effective := time.Time{}
	if input.EffectiveFrom != nil {
		effective = input.EffectiveFrom.UTC()
	}
	value, err := admin.Decide(r.Context(), r.PathValue("id"), input.ExpectedVersion, input.Approve, platformPolicyActor(r), input.Reason, effective)
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
func (s *Server) retireGatewayPool(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireGatewayPools(w, r)
	if !ok || !s.requirePlatformPolicyStepUp(w, r, "gateway_pool.retire") {
		return
	}
	var input gatewayPoolRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The gateway-pool retirement request is invalid.", nil)
		return
	}
	value, err := admin.Retire(r.Context(), r.PathValue("id"), input.ExpectedVersion, platformPolicyActor(r), input.Reason)
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
func (s *Server) listGatewayPoolEvents(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireGatewayPools(w, r)
	if !ok {
		return
	}
	request, err := httpx.ParsePage(r, 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE", "The gateway-pool event page request is invalid.", nil)
		return
	}
	page, err := admin.EventsPage(r.Context(), r.PathValue("id"), request.Limit, request.Cursor)
	if errors.Is(err, sender.ErrInvalidPaginationCursor) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The gateway-pool event page cursor is invalid.", nil)
		return
	}
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}

func (s *Server) getGatewayNode(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) {
		return
	}
	value, err := s.deps.SenderGovernance.Store.GetNode(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

type gatewayNodeTransitionRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason"`
}

func (s *Server) transitionGatewayNode(w http.ResponseWriter, r *http.Request) {
	if !s.requireSenderGovernance(w, r) || !s.requirePlatformPolicyStepUp(w, r, "gateway_node.transition") {
		return
	}
	var input gatewayNodeTransitionRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The gateway-node transition is invalid.", nil)
		return
	}
	status := strings.ToUpper(strings.TrimSpace(r.PathValue("action")))
	switch status {
	case "DRAIN":
		status = "DRAINING"
	case "OFFLINE":
		status = "OFFLINE"
	case "RETIRE":
		status = "RETIRED"
	default:
		httpx.WriteError(w, r, http.StatusNotFound, "GATEWAY_NODE_ACTION_UNKNOWN", "The gateway-node action is unknown.", nil)
		return
	}
	value, err := s.deps.SenderGovernance.TransitionNode(r.Context(), r.PathValue("id"), input.ExpectedVersion, status, platformPolicyActor(r), input.Reason)
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
func (s *Server) listGatewayRuntimeEvents(w http.ResponseWriter, r *http.Request) {
	if s.deps.GatewayRuntime == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "GATEWAY_RUNTIME_UNAVAILABLE", "Gateway runtime registration is unavailable.", nil)
		return
	}
	request, err := httpx.ParsePage(r, 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE", "The gateway runtime-event page request is invalid.", nil)
		return
	}
	page, err := s.deps.GatewayRuntime.EventsPage(r.Context(), r.PathValue("id"), request.Limit, request.Cursor)
	if errors.Is(err, sender.ErrInvalidPaginationCursor) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The gateway runtime-event page cursor is invalid.", nil)
		return
	}
	if err != nil {
		s.writeSenderError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}
func (s *Server) registerGatewayRuntime(w http.ResponseWriter, r *http.Request) {
	if s.deps.GatewayRuntime == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "GATEWAY_RUNTIME_UNAVAILABLE", "Gateway runtime registration is unavailable.", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "GATEWAY_RUNTIME_BODY_INVALID", "The gateway runtime report could not be read.", nil)
		return
	}
	value, err := s.deps.GatewayRuntime.Register(r.Context(), r.PathValue("id"), r.Header.Get(sender.RuntimeTimestampHeader), r.Header.Get(sender.RuntimeNonceHeader), r.Header.Get(sender.RuntimeSignatureHeader), raw)
	if err != nil {
		switch {
		case errors.Is(err, sender.ErrRuntimeAuthentication):
			httpx.WriteError(w, r, http.StatusUnauthorized, "GATEWAY_RUNTIME_AUTHENTICATION_FAILED", "The gateway runtime signature or timestamp is invalid.", nil)
		case errors.Is(err, sender.ErrRuntimeReplay):
			httpx.WriteError(w, r, http.StatusConflict, "GATEWAY_RUNTIME_REPLAY", "The gateway runtime report nonce has already been used.", nil)
		case errors.Is(err, sender.ErrRuntimeDrift), errors.Is(err, sender.ErrSenderConflict):
			httpx.WriteError(w, r, http.StatusConflict, "GATEWAY_RUNTIME_DRIFT", "The running gateway does not match the governed node or pool configuration.", nil)
		case errors.Is(err, sender.ErrSenderNotFound):
			httpx.WriteError(w, r, http.StatusNotFound, "GATEWAY_NODE_NOT_FOUND", "The governed gateway node does not exist.", nil)
		default:
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "GATEWAY_RUNTIME_REJECTED", err.Error(), nil)
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
