package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/provider"
	"campaign-platform/internal/shared/httpx"
)

func (s *Server) listProviderCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.deps.ProviderCapabilities == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "PROVIDER_CAPABILITY_UNAVAILABLE", "Provider capability administration is not configured.", nil)
		return
	}
	items, err := s.deps.ProviderCapabilities.Store.List(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteListAuto(w, http.StatusOK, items)
}

func (s *Server) createProviderCapability(w http.ResponseWriter, r *http.Request) {
	if s.deps.ProviderCapabilities == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "PROVIDER_CAPABILITY_UNAVAILABLE", "Provider capability administration is not configured.", nil)
		return
	}
	var input provider.Definition
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The provider capability request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	value, err := s.deps.ProviderCapabilities.CreateDraft(r.Context(), input, principal.User.ID, input.Reason)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "PROVIDER_CAPABILITY_INVALID", "The provider capability definition could not be created.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, value)
}

func (s *Server) submitProviderCapability(w http.ResponseWriter, r *http.Request) {
	if s.deps.ProviderCapabilities == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "PROVIDER_CAPABILITY_UNAVAILABLE", "Provider capability administration is not configured.", nil)
		return
	}
	var input struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		Reason          string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The provider capability submission is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	value, err := s.deps.ProviderCapabilities.Submit(r.Context(), r.PathValue("id"), input.ExpectedVersion, principal.User.ID, input.Reason)
	if errors.Is(err, provider.ErrConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "PROVIDER_CAPABILITY_CONFLICT", "The provider capability definition changed; reload before submitting.", nil)
		return
	}
	if errors.Is(err, provider.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "PROVIDER_CAPABILITY_NOT_FOUND", "The provider capability definition was not found.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "PROVIDER_CAPABILITY_INVALID", "The provider capability submission was rejected.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (s *Server) decideProviderCapability(w http.ResponseWriter, r *http.Request) {
	if s.deps.ProviderCapabilities == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "PROVIDER_CAPABILITY_UNAVAILABLE", "Provider capability administration is not configured.", nil)
		return
	}
	var input struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		Approve         bool   `json:"approve"`
		Reason          string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The provider capability decision is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "MFA_STEP_UP_REQUIRED", "Recent MFA verification is required.", nil)
		return
	}
	value, err := s.deps.ProviderCapabilities.Decide(r.Context(), r.PathValue("id"), input.ExpectedVersion, input.Approve, principal.User.ID, input.Reason)
	if errors.Is(err, provider.ErrConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "PROVIDER_CAPABILITY_CONFLICT", "The provider capability definition changed; reload before deciding.", nil)
		return
	}
	if errors.Is(err, provider.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "PROVIDER_CAPABILITY_NOT_FOUND", "The provider capability definition was not found.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "PROVIDER_CAPABILITY_INVALID", "The provider capability decision was rejected.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (s *Server) resolveProviderCapability(w http.ResponseWriter, r *http.Request) {
	if s.deps.ProviderCapabilities == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "PROVIDER_CAPABILITY_UNAVAILABLE", "Provider capability administration is not configured.", nil)
		return
	}
	var input struct {
		Provider     string                `json:"provider"`
		Channel      provider.Channel      `json:"channel"`
		Engine       string                `json:"engine"`
		At           *time.Time            `json:"at,omitempty"`
		Capabilities []provider.Capability `json:"capabilities,omitempty"`
	}
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The provider capability resolution request is invalid.", nil)
		return
	}
	at := time.Now().UTC()
	if input.At != nil {
		at = input.At.UTC()
	}
	value, err := s.deps.ProviderCapabilities.Require(r.Context(), strings.TrimSpace(input.Provider), input.Channel, strings.TrimSpace(input.Engine), at, input.Capabilities)
	if errors.Is(err, provider.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "PROVIDER_CAPABILITY_NOT_FOUND", "No active provider capability definition matched the requested route.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "PROVIDER_CAPABILITY_MISMATCH", "The provider route does not satisfy the requested capabilities.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (s *Server) retireProviderCapability(w http.ResponseWriter, r *http.Request) {
	if s.deps.ProviderCapabilities == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "PROVIDER_CAPABILITY_UNAVAILABLE", "Provider capability administration is not configured.", nil)
		return
	}
	var input struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		Reason          string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The provider capability retirement request is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "MFA_STEP_UP_REQUIRED", "Recent MFA verification is required.", nil)
		return
	}
	value, err := s.deps.ProviderCapabilities.Retire(r.Context(), r.PathValue("id"), input.ExpectedVersion, principal.User.ID, input.Reason)
	if errors.Is(err, provider.ErrConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "PROVIDER_CAPABILITY_CONFLICT", "The provider capability definition changed; reload before retiring.", nil)
		return
	}
	if errors.Is(err, provider.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "PROVIDER_CAPABILITY_NOT_FOUND", "The provider capability definition was not found.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "PROVIDER_CAPABILITY_INVALID", "The provider capability definition could not be retired.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (s *Server) listProviderCapabilityEvents(w http.ResponseWriter, r *http.Request) {
	if s.deps.ProviderCapabilities == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "PROVIDER_CAPABILITY_UNAVAILABLE", "Provider capability administration is not configured.", nil)
		return
	}
	items, err := s.deps.ProviderCapabilities.ListEvents(r.Context(), r.PathValue("id"))
	if errors.Is(err, provider.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "PROVIDER_CAPABILITY_NOT_FOUND", "The provider capability definition was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteListAuto(w, http.StatusOK, items)
}
