package httpserver

import (
	"errors"
	"net/http"
	"time"

	"campaign-platform/internal/audience/contactlife"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/shared/httpx"
)

func (s *Server) getContactLifecycle(w http.ResponseWriter, r *http.Request) {
	if s.deps.ContactLifecycle == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CONTACT_LIFECYCLE_UNAVAILABLE", "Contact lifecycle operations are unavailable.", nil)
		return
	}
	out, err := s.deps.ContactLifecycle.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeContactLifecycleError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) listContactLifecycleEvents(w http.ResponseWriter, r *http.Request) {
	if s.deps.ContactLifecycle == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CONTACT_LIFECYCLE_UNAVAILABLE", "Contact lifecycle operations are unavailable.", nil)
		return
	}
	limit, err := optionalPositiveIntQuery(r, "limit", 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_LIMIT", "The page limit must be between 1 and 500.", nil)
		return
	}
	out, err := s.deps.ContactLifecycle.Events(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		writeContactLifecycleError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) transitionContactLifecycle(w http.ResponseWriter, r *http.Request) {
	if s.deps.ContactLifecycle == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CONTACT_LIFECYCLE_UNAVAILABLE", "Contact lifecycle operations are unavailable.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for contact lifecycle changes.", nil)
		return
	}
	var input struct {
		Status               contactlife.Status `json:"status"`
		ProcessingRestricted bool               `json:"processingRestricted"`
		ExpectedVersion      int64              `json:"expectedVersion"`
		Reason               string             `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, 16<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The contact lifecycle request is invalid.", nil)
		return
	}
	out, err := s.deps.ContactLifecycle.Transition(r.Context(), r.PathValue("id"), input.Status, input.ProcessingRestricted, input.ExpectedVersion, principal.User.ID, input.Reason, r.Header.Get("X-Request-ID"))
	if err != nil {
		writeContactLifecycleError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func writeContactLifecycleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, contactlife.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "CONTACT_NOT_FOUND", "The contact lifecycle record was not found.", nil)
	case errors.Is(err, contactlife.ErrConflict):
		httpx.WriteError(w, r, http.StatusConflict, "CONTACT_LIFECYCLE_CONFLICT", "The contact lifecycle changed or the transition is not permitted.", nil)
	case errors.Is(err, contactlife.ErrInvalid):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CONTACT_LIFECYCLE_INVALID", "The contact lifecycle transition is invalid.", nil)
	default:
		httpx.WriteError(w, r, http.StatusInternalServerError, "CONTACT_LIFECYCLE_FAILED", "The contact lifecycle operation failed.", nil)
	}
}
