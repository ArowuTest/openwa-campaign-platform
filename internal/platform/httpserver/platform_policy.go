package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/platformpolicy"
	"campaign-platform/internal/shared/httpx"
)

func platformPolicyActor(r *http.Request) string {
	principal, _ := identity.PrincipalFromContext(r.Context())
	return principal.User.ID
}

func (s *Server) requirePlatformPolicyStepUp(w http.ResponseWriter, r *http.Request, action string) bool {
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required.", nil)
		return false
	}
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		if s.deps.Identity != nil {
			s.deps.Identity.RecordStepUpRequired(r.Context(), principal.User.ID, action, authenticationAttempt(r, principal.User.Email))
		}
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for this platform action.", nil)
		return false
	}
	return true
}

func (s *Server) maintenanceAdmission(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.deps.Maintenance == nil || r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		// Maintenance controls, provider callbacks and signed runtime heartbeats
		// must remain available so an emergency can be ended and authoritative
		// evidence can continue to arrive while new business writes are frozen.
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/maintenance-windows") ||
			strings.HasPrefix(r.URL.Path, "/api/v1/internal/gateway/events") ||
			strings.HasPrefix(r.URL.Path, "/api/v1/internal/gateway/inbound") ||
			strings.HasPrefix(r.URL.Path, "/api/v1/internal/gateway-nodes/") {
			next.ServeHTTP(w, r)
			return
		}
		if err := s.deps.Maintenance.Check(r.Context(), platformpolicy.OperationAPIWrite, platformpolicy.OperationalScope{}, time.Now().UTC()); err != nil {
			var blocked platformpolicy.BlockedError
			if errors.As(err, &blocked) {
				httpx.WriteError(w, r, http.StatusServiceUnavailable, "MAINTENANCE_MODE_ACTIVE", "The platform is currently in a governed maintenance mode.", map[string]any{"maintenanceWindowId": blocked.Window.ID, "mode": blocked.Window.Mode, "scopeType": blocked.Window.ScopeType, "scopeId": blocked.Window.ScopeID})
				return
			}
			s.internalError(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type configurationRequest struct {
	Key             string                   `json:"key"`
	ScopeType       platformpolicy.ScopeType `json:"scopeType"`
	ScopeID         string                   `json:"scopeId"`
	Value           json.RawMessage          `json:"value"`
	EffectiveFrom   *time.Time               `json:"effectiveFrom"`
	EffectiveTo     *time.Time               `json:"effectiveTo"`
	ExpectedVersion int64                    `json:"expectedVersion"`
	Approve         bool                     `json:"approve"`
	Reason          string                   `json:"reason"`
	SourceID        string                   `json:"sourceId"`
}

func (s *Server) requireConfigurations(w http.ResponseWriter, r *http.Request) (*platformpolicy.ConfigurationAdministration, bool) {
	if s.deps.Configurations == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CONFIGURATION_GOVERNANCE_UNAVAILABLE", "Platform configuration governance is unavailable.", nil)
		return nil, false
	}
	return s.deps.Configurations, true
}

func (s *Server) listPlatformConfigurations(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireConfigurations(w, r)
	if !ok {
		return
	}
	limit, err := parseOptionalPositiveInt(r.URL.Query().Get("limit"), 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_LIMIT", "The configuration page limit is invalid.", nil)
		return
	}
	scope := platformpolicy.ScopeType(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("scopeType"))))
	status := platformpolicy.LifecycleStatus(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))))
	if !validPlatformScope(scope) || !validConfigurationStatus(status) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_FILTER", "The configuration scope or status filter is invalid.", nil)
		return
	}
	items, err := admin.List(r.Context(), platformpolicy.ConfigurationQuery{Key: strings.TrimSpace(r.URL.Query().Get("key")), ScopeType: scope, ScopeID: strings.TrimSpace(r.URL.Query().Get("scopeId")), Status: status, Limit: limit})
	if err != nil {
		s.writePlatformPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) createPlatformConfiguration(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireConfigurations(w, r)
	if !ok {
		return
	}
	var input configurationRequest
	if err := httpx.DecodeJSON(w, r, 128<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The configuration request is invalid.", nil)
		return
	}
	value := platformpolicy.Configuration{Key: input.Key, ScopeType: input.ScopeType, ScopeID: input.ScopeID, Value: input.Value, EffectiveTo: input.EffectiveTo}
	if input.EffectiveFrom != nil {
		value.EffectiveFrom = input.EffectiveFrom.UTC()
	}
	created, err := admin.Create(r.Context(), value, platformPolicyActor(r), input.Reason)
	if err != nil {
		s.writePlatformPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
}

func (s *Server) submitPlatformConfiguration(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireConfigurations(w, r)
	if !ok {
		return
	}
	var input configurationRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The configuration submission is invalid.", nil)
		return
	}
	value, err := admin.Submit(r.Context(), r.PathValue("id"), input.ExpectedVersion, platformPolicyActor(r), input.Reason)
	if err != nil {
		s.writePlatformPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (s *Server) decidePlatformConfiguration(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireConfigurations(w, r)
	if !ok || !s.requirePlatformPolicyStepUp(w, r, "configuration.decision") {
		return
	}
	var input configurationRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The configuration decision is invalid.", nil)
		return
	}
	value, err := admin.Decide(r.Context(), r.PathValue("id"), input.ExpectedVersion, input.Approve, platformPolicyActor(r), input.Reason)
	if err != nil {
		s.writePlatformPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (s *Server) retirePlatformConfiguration(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireConfigurations(w, r)
	if !ok || !s.requirePlatformPolicyStepUp(w, r, "configuration.retire") {
		return
	}
	var input configurationRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The configuration retirement request is invalid.", nil)
		return
	}
	value, err := admin.Retire(r.Context(), r.PathValue("id"), input.ExpectedVersion, platformPolicyActor(r), input.Reason)
	if err != nil {
		s.writePlatformPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (s *Server) rollbackPlatformConfiguration(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireConfigurations(w, r)
	if !ok || !s.requirePlatformPolicyStepUp(w, r, "configuration.rollback") {
		return
	}
	var input configurationRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The configuration rollback request is invalid.", nil)
		return
	}
	effective := time.Time{}
	if input.EffectiveFrom != nil {
		effective = input.EffectiveFrom.UTC()
	}
	value, err := admin.Rollback(r.Context(), r.PathValue("id"), platformPolicyActor(r), input.Reason, effective)
	if err != nil {
		s.writePlatformPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, value)
}

func (s *Server) listPlatformConfigurationEvents(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireConfigurations(w, r)
	if !ok {
		return
	}
	items, err := admin.Events(r.Context(), r.PathValue("id"), 500)
	if err != nil {
		s.writePlatformPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

type maintenanceRequest struct {
	Name                string                         `json:"name"`
	Mode                platformpolicy.MaintenanceMode `json:"mode"`
	ScopeType           platformpolicy.ScopeType       `json:"scopeType"`
	ScopeID             string                         `json:"scopeId"`
	StartsAt            *time.Time                     `json:"startsAt"`
	EndsAt              *time.Time                     `json:"endsAt"`
	AllowActiveDispatch bool                           `json:"allowActiveDispatch"`
	ExpectedVersion     int64                          `json:"expectedVersion"`
	Approve             bool                           `json:"approve"`
	Reason              string                         `json:"reason"`
}

func (s *Server) requireMaintenance(w http.ResponseWriter, r *http.Request) (*platformpolicy.MaintenanceAdministration, bool) {
	if s.deps.Maintenance == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "MAINTENANCE_GOVERNANCE_UNAVAILABLE", "Maintenance governance is unavailable.", nil)
		return nil, false
	}
	return s.deps.Maintenance, true
}

func (s *Server) listMaintenanceWindows(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireMaintenance(w, r)
	if !ok {
		return
	}
	status := platformpolicy.MaintenanceStatus(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))))
	if !validMaintenanceStatus(status) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_STATUS", "The maintenance status is invalid.", nil)
		return
	}
	limit, err := parseOptionalPositiveInt(r.URL.Query().Get("limit"), 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_LIMIT", "The maintenance page limit is invalid.", nil)
		return
	}
	items, err := admin.List(r.Context(), status, limit)
	if err != nil {
		s.writePlatformPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func validPlatformScope(v platformpolicy.ScopeType) bool {
	switch v {
	case "", platformpolicy.ScopePlatform, platformpolicy.ScopeEnvironment, platformpolicy.ScopeOrganisation, platformpolicy.ScopeCampaign, platformpolicy.ScopeProvider, platformpolicy.ScopeGatewayPool, platformpolicy.ScopeSenderPool:
		return true
	default:
		return false
	}
}

func validConfigurationStatus(v platformpolicy.LifecycleStatus) bool {
	switch v {
	case "", platformpolicy.StatusDraft, platformpolicy.StatusPendingApproval, platformpolicy.StatusActive, platformpolicy.StatusRejected, platformpolicy.StatusSuperseded, platformpolicy.StatusRetired:
		return true
	default:
		return false
	}
}

func validMaintenanceStatus(v platformpolicy.MaintenanceStatus) bool {
	switch v {
	case "", platformpolicy.MaintenanceDraft, platformpolicy.MaintenancePendingApproval, platformpolicy.MaintenanceActive, platformpolicy.MaintenanceRejected, platformpolicy.MaintenanceEnded, platformpolicy.MaintenanceCancelled:
		return true
	default:
		return false
	}
}
func (s *Server) listActiveMaintenanceWindows(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireMaintenance(w, r)
	if !ok {
		return
	}
	items, err := admin.Active(r.Context(), time.Now().UTC())
	if err != nil {
		s.writePlatformPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}
func (s *Server) createMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireMaintenance(w, r)
	if !ok {
		return
	}
	var input maintenanceRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The maintenance request is invalid.", nil)
		return
	}
	value := platformpolicy.MaintenanceWindow{Name: input.Name, Mode: input.Mode, ScopeType: input.ScopeType, ScopeID: input.ScopeID, EndsAt: input.EndsAt, AllowActiveDispatch: input.AllowActiveDispatch}
	if input.StartsAt != nil {
		value.StartsAt = input.StartsAt.UTC()
	}
	created, err := admin.Create(r.Context(), value, platformPolicyActor(r), input.Reason)
	if err != nil {
		s.writePlatformPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
}
func (s *Server) submitMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireMaintenance(w, r)
	if !ok {
		return
	}
	var input maintenanceRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The maintenance submission is invalid.", nil)
		return
	}
	value, err := admin.Submit(r.Context(), r.PathValue("id"), input.ExpectedVersion, platformPolicyActor(r), input.Reason)
	if err != nil {
		s.writePlatformPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
func (s *Server) decideMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireMaintenance(w, r)
	if !ok || !s.requirePlatformPolicyStepUp(w, r, "maintenance.decision") {
		return
	}
	var input maintenanceRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The maintenance decision is invalid.", nil)
		return
	}
	value, err := admin.Decide(r.Context(), r.PathValue("id"), input.ExpectedVersion, input.Approve, platformPolicyActor(r), input.Reason)
	if err != nil {
		s.writePlatformPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
func (s *Server) endMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireMaintenance(w, r)
	if !ok || !s.requirePlatformPolicyStepUp(w, r, "maintenance.end") {
		return
	}
	var input maintenanceRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The maintenance end request is invalid.", nil)
		return
	}
	value, err := admin.End(r.Context(), r.PathValue("id"), input.ExpectedVersion, platformPolicyActor(r), input.Reason)
	if err != nil {
		s.writePlatformPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
func (s *Server) listMaintenanceEvents(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireMaintenance(w, r)
	if !ok {
		return
	}
	items, err := admin.Events(r.Context(), r.PathValue("id"), 500)
	if err != nil {
		s.writePlatformPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) writePlatformPolicyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, platformpolicy.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "PLATFORM_POLICY_NOT_FOUND", "The governed platform record does not exist.", nil)
	case errors.Is(err, platformpolicy.ErrConflict):
		httpx.WriteError(w, r, http.StatusConflict, "PLATFORM_POLICY_CONFLICT", "The governed platform record changed or is in an incompatible state.", nil)
	case errors.Is(err, platformpolicy.ErrInvalid):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "PLATFORM_POLICY_INVALID", "The governed platform record is invalid.", nil)
	default:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "PLATFORM_POLICY_REJECTED", err.Error(), nil)
	}
}

func parseOptionalPositiveInt(raw string, fallback, maximum int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > maximum {
		return 0, errors.New("invalid positive integer")
	}
	return value, nil
}
