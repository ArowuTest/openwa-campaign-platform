package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"campaign-platform/internal/audience/importer"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/shared/httpx"
)

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "REQUEST_INVALID", "The request body is invalid.", map[string]any{"detail": err.Error()})
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "REQUEST_INVALID", "The request body contains trailing JSON.", nil)
		return false
	}
	return true
}

func (s *Server) createConsentGrant(w http.ResponseWriter, r *http.Request) {
	if s.deps.ConsentLedger == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CONSENT_LEDGER_UNAVAILABLE", "The consent ledger is not configured.", nil)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if err := importer.ValidateIdempotencyKey(key); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "A valid Idempotency-Key header is required.", map[string]any{"detail": err.Error()})
		return
	}
	var input consent.GrantInput
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.CreatedBy = principal.User.ID
	input.ClientRequestID = key
	grant, created, err := s.deps.ConsentLedger.CreateGrant(r.Context(), input)
	if err != nil {
		s.writeConsentLedgerError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, map[string]any{"created": created, "grant": grant})
}
func (s *Server) withdrawConsentGrant(w http.ResponseWriter, r *http.Request) {
	if s.deps.ConsentLedger == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CONSENT_LEDGER_UNAVAILABLE", "The consent ledger is not configured.", nil)
		return
	}
	var input consent.WithdrawInput
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.ActorID = principal.User.ID
	grant, err := s.deps.ConsentLedger.Withdraw(r.Context(), r.PathValue("id"), input)
	if err != nil {
		s.writeConsentLedgerError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, grant)
}
func (s *Server) createSuppression(w http.ResponseWriter, r *http.Request) {
	if s.deps.ConsentLedger == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CONSENT_LEDGER_UNAVAILABLE", "The consent ledger is not configured.", nil)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if err := importer.ValidateIdempotencyKey(key); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "A valid Idempotency-Key header is required.", map[string]any{"detail": err.Error()})
		return
	}
	var input consent.SuppressionInput
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.CreatedBy = principal.User.ID
	input.ClientRequestID = key
	value, created, err := s.deps.ConsentLedger.CreateSuppression(r.Context(), input)
	if err != nil {
		s.writeConsentLedgerError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, map[string]any{"created": created, "suppression": value})
}
func (s *Server) revokeSuppression(w http.ResponseWriter, r *http.Request) {
	if s.deps.ConsentLedger == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CONSENT_LEDGER_UNAVAILABLE", "The consent ledger is not configured.", nil)
		return
	}
	var input consent.RevokeSuppressionInput
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.ActorID = principal.User.ID
	value, err := s.deps.ConsentLedger.RevokeSuppression(r.Context(), r.PathValue("id"), input)
	if err != nil {
		s.writeConsentLedgerError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
func (s *Server) listConsentEvents(w http.ResponseWriter, r *http.Request) {
	if s.deps.ConsentLedger == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CONSENT_LEDGER_UNAVAILABLE", "The consent ledger is not configured.", nil)
		return
	}
	limit, err := optionalPositiveIntQuery(r, "limit", 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_LIMIT", "The page limit must be between 1 and 500.", nil)
		return
	}
	page, err := s.deps.ConsentLedger.EventsPage(r.Context(), strings.TrimSpace(r.URL.Query().Get("contactId")), limit, strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		if errors.Is(err, consent.ErrInvalidConsentEventCursor) {
			httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The consent-event page cursor is invalid.", nil)
			return
		}
		s.internalError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}
func (s *Server) writeConsentLedgerError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, consent.ErrGrantNotFound), errors.Is(err, consent.ErrSuppressionNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "CONSENT_RECORD_NOT_FOUND", "The consent record was not found.", nil)
	case errors.Is(err, consent.ErrLedgerConflict), errors.Is(err, consent.ErrLedgerReplayConflict):
		httpx.WriteError(w, r, http.StatusConflict, "CONSENT_LEDGER_CONFLICT", "The consent operation conflicted with existing evidence.", map[string]any{"detail": err.Error()})
	default:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CONSENT_LEDGER_REJECTED", "The consent operation was rejected.", map[string]any{"detail": err.Error()})
	}
}
