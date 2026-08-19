package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/metacloud"
	"campaign-platform/internal/shared/httpx"
)

type MetaSenderVerifier interface {
	VerifySender(context.Context, metacloud.Sender) (metacloud.SenderVerification, error)
}

type metaSenderRequest struct {
	OrganisationID       string     `json:"organisationId"`
	SenderPoolID         string     `json:"senderPoolId"`
	WABAID               string     `json:"wabaId"`
	PhoneNumberID        string     `json:"phoneNumberId"`
	DisplayName          string     `json:"displayName"`
	BusinessPhoneDisplay string     `json:"businessPhoneDisplay"`
	CredentialKey        string     `json:"credentialKey"`
	GraphAPIVersion      string     `json:"graphApiVersion"`
	ExpectedVersion      int64      `json:"expectedVersion"`
	Approve              bool       `json:"approve"`
	EffectiveFrom        *time.Time `json:"effectiveFrom,omitempty"`
	Reason               string     `json:"reason"`
}

func (s *Server) requireMetaSenders(w http.ResponseWriter, r *http.Request) (*metacloud.Service, bool) {
	if s.deps.MetaSenders == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "META_SENDER_GOVERNANCE_UNAVAILABLE", "Meta sender governance is unavailable.", nil)
		return nil, false
	}
	return s.deps.MetaSenders, true
}

func (s *Server) listMetaSenders(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireMetaSenders(w, r)
	if !ok {
		return
	}
	values, err := service.List(r.Context())
	if err != nil {
		s.writeMetaSenderError(w, r, err)
		return
	}
	httpx.WriteListAuto(w, http.StatusOK, values)
}

func (s *Server) getMetaSender(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireMetaSenders(w, r)
	if !ok {
		return
	}
	value, err := service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeMetaSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
func (s *Server) createMetaSender(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireMetaSenders(w, r)
	if !ok {
		return
	}
	var input metaSenderRequest
	if err := httpx.DecodeJSON(w, r, 128<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The Meta sender request is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	value, err := service.CreateDraft(r.Context(), metacloud.Sender{
		OrganisationID: input.OrganisationID, SenderPoolID: input.SenderPoolID, WABAID: input.WABAID,
		PhoneNumberID: input.PhoneNumberID, DisplayName: input.DisplayName, BusinessPhoneDisplay: input.BusinessPhoneDisplay,
		CredentialKey: input.CredentialKey, GraphAPIVersion: input.GraphAPIVersion,
	}, principal.User.ID, input.Reason)
	if err != nil {
		s.writeMetaSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, value)
}

func (s *Server) submitMetaSender(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireMetaSenders(w, r)
	if !ok {
		return
	}
	var input metaSenderRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The Meta sender submission is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	value, err := service.Submit(r.Context(), r.PathValue("id"), input.ExpectedVersion, principal.User.ID, input.Reason)
	if err != nil {
		s.writeMetaSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
func (s *Server) decideMetaSender(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireMetaSenders(w, r)
	if !ok || !s.requirePlatformPolicyStepUp(w, r, "meta_sender.decision") {
		return
	}
	var input metaSenderRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The Meta sender decision is invalid.", nil)
		return
	}
	effective := time.Time{}
	if input.EffectiveFrom != nil {
		effective = input.EffectiveFrom.UTC()
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	value, err := service.Decide(r.Context(), r.PathValue("id"), input.ExpectedVersion, input.Approve, principal.User.ID, input.Reason, effective)
	if err != nil {
		s.writeMetaSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (s *Server) retireMetaSender(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireMetaSenders(w, r)
	if !ok || !s.requirePlatformPolicyStepUp(w, r, "meta_sender.retire") {
		return
	}
	var input metaSenderRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The Meta sender retirement request is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	value, err := service.Retire(r.Context(), r.PathValue("id"), input.ExpectedVersion, principal.User.ID, input.Reason)
	if err != nil {
		s.writeMetaSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
func (s *Server) verifyMetaSender(w http.ResponseWriter, r *http.Request) {
	service, ok := s.requireMetaSenders(w, r)
	if !ok || !s.requirePlatformPolicyStepUp(w, r, "meta_sender.verify") {
		return
	}
	if s.deps.MetaVerifier == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "META_VERIFICATION_UNAVAILABLE", "Meta sender verification is unavailable.", nil)
		return
	}
	var input metaSenderRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The Meta sender verification request is invalid.", nil)
		return
	}
	current, err := service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeMetaSenderError(w, r, err)
		return
	}
	if current.Version != input.ExpectedVersion {
		s.writeMetaSenderError(w, r, metacloud.ErrConflict)
		return
	}
	verification, err := s.deps.MetaVerifier.VerifySender(r.Context(), current)
	if err != nil {
		s.writeMetaVerificationError(w, r, err)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	updated, err := service.ObserveHealth(r.Context(), current.ID, current.Version, metacloud.HealthHealthy, time.Now().UTC(), principal.User.ID, input.Reason)
	if err != nil {
		s.writeMetaSenderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sender": updated, "verification": verification})
}
func (s *Server) writeMetaSenderError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, metacloud.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "META_SENDER_NOT_FOUND", "The Meta sender was not found.", nil)
	case errors.Is(err, metacloud.ErrConflict):
		httpx.WriteError(w, r, http.StatusConflict, "META_SENDER_CONFLICT", "The Meta sender changed; reload before retrying.", nil)
	case errors.Is(err, metacloud.ErrInvalid):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "META_SENDER_INVALID", "The Meta sender operation was rejected by governance.", nil)
	default:
		s.internalError(w, r, err)
	}
}

func (s *Server) writeMetaVerificationError(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr metacloud.APIError
	if !errors.As(err, &apiErr) {
		s.internalError(w, r, err)
		return
	}
	switch apiErr.Safety {
	case metacloud.SafetySafeToRetry:
		httpx.WriteError(w, r, http.StatusBadGateway, "META_VERIFICATION_RETRYABLE", "Meta sender verification could not complete; retry later.", nil)
	case metacloud.SafetyPermanent:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "META_VERIFICATION_REJECTED", "Meta rejected the governed sender verification.", nil)
	default:
		httpx.WriteError(w, r, http.StatusBadGateway, "META_VERIFICATION_UNCERTAIN", "Meta sender verification returned an uncertain result.", nil)
	}
}

func normaliseMetaCredentialKey(value string) string { return strings.TrimSpace(value) }
