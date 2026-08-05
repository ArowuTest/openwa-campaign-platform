package httpserver

import (
	"errors"
	"net/http"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/shared/httpx"
)

func (s *Server) listInternalUsers(w http.ResponseWriter, r *http.Request) {
	if s.deps.IdentityAdministration == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "IDENTITY_ADMINISTRATION_UNAVAILABLE", "Identity administration is unavailable.", nil)
		return
	}
	items, err := s.deps.IdentityAdministration.List(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) createInternalUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireIdentityAdministrationStepUp(w, r, "identity.user.create") {
		return
	}
	var input identity.CreateAccountInput
	if err := httpx.DecodeJSON(w, r, 128<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The internal-user request is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.ActorID = principal.User.ID
	result, err := s.deps.IdentityAdministration.Create(r.Context(), input)
	if err != nil {
		s.writeIdentityAdministrationError(w, r, err)
		return
	}
	// The TOTP secret is returned once for controlled enrolment and is never retrievable later.
	httpx.WriteJSON(w, http.StatusCreated, result)
}

func (s *Server) updateInternalUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireIdentityAdministrationStepUp(w, r, "identity.user.update") {
		return
	}
	var input identity.UpdateAccountInput
	if err := httpx.DecodeJSON(w, r, 128<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The internal-user update is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.ActorID = principal.User.ID
	result, err := s.deps.IdentityAdministration.Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		s.writeIdentityAdministrationError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) resetInternalUserCredentials(w http.ResponseWriter, r *http.Request) {
	if !s.requireIdentityAdministrationStepUp(w, r, "identity.credentials.reset") {
		return
	}
	var input identity.ResetCredentialInput
	if err := httpx.DecodeJSON(w, r, 128<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The credential-reset request is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.ActorID = principal.User.ID
	result, err := s.deps.IdentityAdministration.ResetCredentials(r.Context(), r.PathValue("id"), input)
	if err != nil {
		s.writeIdentityAdministrationError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

type unlockAccountRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason"`
}

func (s *Server) unlockInternalUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireIdentityAdministrationStepUp(w, r, "identity.user.unlock") {
		return
	}
	var input unlockAccountRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The unlock request is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	result, err := s.deps.IdentityAdministration.Unlock(r.Context(), r.PathValue("id"), input.ExpectedVersion, principal.User.ID, input.Reason)
	if err != nil {
		s.writeIdentityAdministrationError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

type revokeSessionsRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) revokeInternalUserSessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireIdentityAdministrationStepUp(w, r, "identity.sessions.revoke") {
		return
	}
	var input revokeSessionsRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The session-revocation request is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	count, err := s.deps.IdentityAdministration.RevokeSessions(r.Context(), r.PathValue("id"), principal.User.ID, input.Reason)
	if err != nil {
		s.writeIdentityAdministrationError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"revoked": count})
}

func (s *Server) requireIdentityAdministrationStepUp(w http.ResponseWriter, r *http.Request, action string) bool {
	if s.deps.IdentityAdministration == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "IDENTITY_ADMINISTRATION_UNAVAILABLE", "Identity administration is unavailable.", nil)
		return false
	}
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required.", nil)
		return false
	}
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		s.deps.Identity.RecordStepUpRequired(r.Context(), principal.User.ID, action, authenticationAttempt(r, principal.User.Email))
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for this identity-administration action.", nil)
		return false
	}
	return true
}

func (s *Server) writeIdentityAdministrationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrAccountNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "USER_NOT_FOUND", "The internal user does not exist.", nil)
	case errors.Is(err, identity.ErrAccountConflict):
		httpx.WriteError(w, r, http.StatusConflict, "USER_VERSION_CONFLICT", "The internal user changed; reload before retrying.", nil)
	case errors.Is(err, identity.ErrAccountDuplicate):
		httpx.WriteError(w, r, http.StatusConflict, "USER_EMAIL_EXISTS", "An internal user already uses that email address.", nil)
	case errors.Is(err, identity.ErrLastSuperAdmin):
		httpx.WriteError(w, r, http.StatusConflict, "LAST_SUPER_ADMIN", "The last active super administrator cannot be disabled or de-roled.", nil)
	case errors.Is(err, identity.ErrUnknownRole), errors.Is(err, identity.ErrGenericSharedAccount):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDENTITY_POLICY_VIOLATION", err.Error(), nil)
	default:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IDENTITY_ADMINISTRATION_INVALID", err.Error(), nil)
	}
}
