package httpserver

import (
	"errors"
	"net/http"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/inbound"
	"campaign-platform/internal/shared/httpx"
)

type createInboundRetentionPolicyRequest struct {
	RetentionDays int       `json:"retentionDays"`
	EffectiveFrom time.Time `json:"effectiveFrom"`
	Reason        string    `json:"reason"`
}
type transitionInboundRetentionPolicyRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason"`
	Approve         bool   `json:"approve"`
}

func (s *Server) listInboundRetentionPolicies(w http.ResponseWriter, r *http.Request) {
	if s.deps.InboundRetentionPolicies == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "RETENTION_POLICY_UNAVAILABLE", "Inbound retention policy administration is unavailable.", nil)
		return
	}
	items, err := s.deps.InboundRetentionPolicies.Store.List(r.Context())
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "RETENTION_POLICY_LIST_FAILED", "Retention policies could not be listed.", nil)
		return
	}
	httpx.WriteListAuto(w, http.StatusOK, items)
}
func (s *Server) createInboundRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	principal, _ := identity.PrincipalFromContext(r.Context())
	var in createInboundRetentionPolicyRequest
	if err := httpx.DecodeJSON(w, r, 1<<20, &in); err != nil {
		return
	}
	item, err := s.deps.InboundRetentionPolicies.CreateDraft(r.Context(), in.RetentionDays, in.EffectiveFrom, principal.User.ID, in.Reason)
	if err != nil {
		writeInboundRetentionPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}
func (s *Server) submitInboundRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	principal, _ := identity.PrincipalFromContext(r.Context())
	var in transitionInboundRetentionPolicyRequest
	if err := httpx.DecodeJSON(w, r, 1<<20, &in); err != nil {
		return
	}
	item, err := s.deps.InboundRetentionPolicies.Submit(r.Context(), r.PathValue("id"), in.ExpectedVersion, principal.User.ID, in.Reason)
	if err != nil {
		writeInboundRetentionPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (s *Server) decideInboundRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	principal, _ := identity.PrincipalFromContext(r.Context())
	var in transitionInboundRetentionPolicyRequest
	if err := httpx.DecodeJSON(w, r, 1<<20, &in); err != nil {
		return
	}
	item, err := s.deps.InboundRetentionPolicies.Decide(r.Context(), r.PathValue("id"), in.ExpectedVersion, in.Approve, principal.User.ID, in.Reason)
	if err != nil {
		writeInboundRetentionPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (s *Server) reencryptInboundContent(w http.ResponseWriter, r *http.Request) {
	if s.deps.InboundRotation == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "INBOUND_REENCRYPT_UNAVAILABLE", "Inbound content re-encryption is unavailable.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	var in struct{}
	if err := httpx.DecodeJSON(w, r, 1<<20, &in); err != nil {
		return
	}
	run, err := s.deps.InboundRotation.Request(r.Context(), principal.User.ID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "INBOUND_REENCRYPT_REQUEST_FAILED", "Inbound content re-encryption could not be scheduled.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, run)
}
func (s *Server) listInboundReencryptionRuns(w http.ResponseWriter, r *http.Request) {
	if s.deps.InboundRotation == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "INBOUND_REENCRYPT_UNAVAILABLE", "Inbound content re-encryption is unavailable.", nil)
		return
	}
	items, err := s.deps.InboundRotation.List(r.Context(), 50)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "INBOUND_REENCRYPT_LIST_FAILED", "Re-encryption runs could not be listed.", nil)
		return
	}
	httpx.WriteListAuto(w, http.StatusOK, items)
}
func (s *Server) getInboundReencryptionRun(w http.ResponseWriter, r *http.Request) {
	if s.deps.InboundRotation == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "INBOUND_REENCRYPT_UNAVAILABLE", "Inbound content re-encryption is unavailable.", nil)
		return
	}
	item, err := s.deps.InboundRotation.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, inbound.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "INBOUND_REENCRYPT_RUN_NOT_FOUND", "Re-encryption run was not found.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "INBOUND_REENCRYPT_RUN_FAILED", "Re-encryption run could not be read.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func writeInboundRetentionPolicyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, inbound.ErrRetentionPolicyNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "RETENTION_POLICY_NOT_FOUND", "Retention policy was not found.", nil)
	case errors.Is(err, inbound.ErrRetentionPolicyConflict):
		httpx.WriteError(w, r, http.StatusConflict, "RETENTION_POLICY_CONFLICT", "Retention policy changed; reload before retrying.", nil)
	case errors.Is(err, inbound.ErrRetentionPolicyInvalid):
		httpx.WriteError(w, r, http.StatusBadRequest, "RETENTION_POLICY_INVALID", "Retention policy is invalid.", nil)
	default:
		httpx.WriteError(w, r, http.StatusInternalServerError, "RETENTION_POLICY_FAILED", "Retention policy operation failed.", nil)
	}
}
