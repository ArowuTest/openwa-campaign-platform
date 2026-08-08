package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/operations"
	"campaign-platform/internal/privacy"
	"campaign-platform/internal/shared/httpx"
)

func (s *Server) privacyServiceAvailable(w http.ResponseWriter, r *http.Request) bool {
	if s.deps.PrivacyCases == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "PRIVACY_UNAVAILABLE", "Privacy case management is unavailable.", nil)
		return false
	}
	return true
}

func (s *Server) createPrivacyCase(w http.ResponseWriter, r *http.Request) {
	if !s.privacyServiceAvailable(w, r) {
		return
	}
	var input struct {
		Type             privacy.CaseType `json:"type"`
		MSISDN           string           `json:"msisdn"`
		OrganisationID   string           `json:"organisationId,omitempty"`
		Reason           string           `json:"reason"`
		RequestedChanges any              `json:"requestedChanges,omitempty"`
		DueAt            *time.Time       `json:"dueAt,omitempty"`
	}
	if err := httpx.DecodeJSON(w, r, 96<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The privacy case request is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	result, err := s.deps.PrivacyCases.Create(r.Context(), privacy.CreateInput{Type: input.Type, MSISDN: input.MSISDN, OrganisationID: input.OrganisationID, Reason: input.Reason, RequestedChanges: input.RequestedChanges, DueAt: input.DueAt, CreatedBy: principal.User.ID}, r.Header.Get("X-Request-ID"))
	if err != nil {
		writePrivacyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, result)
}

func (s *Server) listPrivacyCases(w http.ResponseWriter, r *http.Request) {
	if !s.privacyServiceAvailable(w, r) {
		return
	}
	query := privacy.Query{Status: privacy.CaseStatus(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))), Type: privacy.CaseType(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("type")))), AssignedTo: strings.TrimSpace(r.URL.Query().Get("assignedTo"))}
	var err error
	query.Limit, err = optionalPositiveIntQuery(r, "limit", 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_LIMIT", "The page limit must be between 1 and 500.", nil)
		return
	}
	if cursor := strings.TrimSpace(r.URL.Query().Get("after")); cursor != "" {
		parts := strings.SplitN(cursor, "|", 2)
		if len(parts) != 2 {
			httpx.WriteError(w, r, http.StatusBadRequest, "PRIVACY_CURSOR_INVALID", "The privacy case cursor is invalid.", nil)
			return
		}
		createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "PRIVACY_CURSOR_INVALID", "The privacy case cursor is invalid.", nil)
			return
		}
		query.AfterCreatedAt, query.AfterID = &createdAt, parts[1]
	}
	page, err := s.deps.PrivacyCases.List(r.Context(), query)
	if err != nil {
		writePrivacyError(w, r, err)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if err := s.deps.PrivacyCases.RecordAccess(r.Context(), principal.User.ID, "PRIVACY_CASE_SEARCHED", "", r.Header.Get("X-Request-ID"), map[string]any{"status": query.Status, "type": query.Type, "assignedTo": query.AssignedTo, "resultCount": len(page.Items)}); err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextAfter)
}

func (s *Server) getPrivacyCase(w http.ResponseWriter, r *http.Request) {
	if !s.privacyServiceAvailable(w, r) {
		return
	}
	result, err := s.deps.PrivacyCases.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writePrivacyError(w, r, err)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if err := s.deps.PrivacyCases.RecordAccess(r.Context(), principal.User.ID, "PRIVACY_CASE_VIEWED", result.ID, r.Header.Get("X-Request-ID"), map[string]any{"type": result.Type, "status": result.Status}); err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) listPrivacyCaseEvents(w http.ResponseWriter, r *http.Request) {
	if !s.privacyServiceAvailable(w, r) {
		return
	}
	request, err := httpx.ParsePage(r, 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE", "The privacy-case event page request is invalid.", nil)
		return
	}
	page, err := s.deps.PrivacyCases.EventsPage(r.Context(), r.PathValue("id"), request.Limit, request.Cursor)
	if errors.Is(err, privacy.ErrInvalidEventCursor) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The privacy-case event page cursor is invalid.", nil)
		return
	}
	if err != nil {
		writePrivacyError(w, r, err)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if err := s.deps.PrivacyCases.RecordAccess(r.Context(), principal.User.ID, "PRIVACY_CASE_HISTORY_VIEWED", r.PathValue("id"), r.Header.Get("X-Request-ID"), map[string]any{"eventCount": len(page.Items)}); err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}

func (s *Server) assignPrivacyCase(w http.ResponseWriter, r *http.Request) {
	if !s.privacyServiceAvailable(w, r) {
		return
	}
	var input struct {
		Assignee        string `json:"assignee"`
		Reason          string `json:"reason"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if err := httpx.DecodeJSON(w, r, 32<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The privacy assignment is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	result, err := s.deps.PrivacyCases.Assign(r.Context(), r.PathValue("id"), input.Assignee, input.Reason, principal.User.ID, r.Header.Get("X-Request-ID"), input.ExpectedVersion)
	if err != nil {
		writePrivacyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) submitPrivacyCase(w http.ResponseWriter, r *http.Request) {
	if !s.privacyServiceAvailable(w, r) {
		return
	}
	var input struct {
		Reason          string `json:"reason"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if err := httpx.DecodeJSON(w, r, 32<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The privacy submission is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	result, err := s.deps.PrivacyCases.Submit(r.Context(), r.PathValue("id"), input.Reason, principal.User.ID, r.Header.Get("X-Request-ID"), input.ExpectedVersion)
	if err != nil {
		writePrivacyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) decidePrivacyCase(w http.ResponseWriter, r *http.Request) {
	if !s.privacyServiceAvailable(w, r) {
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for privacy decisions.", nil)
		return
	}
	var input struct {
		Approve         bool   `json:"approve"`
		Reason          string `json:"reason"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if err := httpx.DecodeJSON(w, r, 32<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The privacy decision is invalid.", nil)
		return
	}
	result, err := s.deps.PrivacyCases.Decide(r.Context(), r.PathValue("id"), input.Approve, input.Reason, principal.User.ID, r.Header.Get("X-Request-ID"), input.ExpectedVersion)
	if err != nil {
		writePrivacyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) executePrivacyCase(w http.ResponseWriter, r *http.Request) {
	if !s.privacyServiceAvailable(w, r) {
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for privacy execution.", nil)
		return
	}
	var input struct {
		Reason          string `json:"reason"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if err := httpx.DecodeJSON(w, r, 32<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The privacy execution request is invalid.", nil)
		return
	}
	result, err := s.deps.PrivacyCases.Execute(r.Context(), r.PathValue("id"), input.Reason, principal.User.ID, r.Header.Get("X-Request-ID"), input.ExpectedVersion)
	if err != nil {
		writePrivacyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) requestPrivacyCaseExport(w http.ResponseWriter, r *http.Request) {
	if !s.privacyServiceAvailable(w, r) || s.deps.Operations == nil {
		if s.deps.Operations == nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "EXPORT_UNAVAILABLE", "Privacy export is unavailable.", nil)
		}
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for privacy exports.", nil)
		return
	}
	var input struct {
		Format string `json:"format"`
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, 16<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The privacy export request is invalid.", nil)
		return
	}
	envelope, err := s.deps.PrivacyCases.ExportEnvelope(r.Context(), r.PathValue("id"), principal.User.ID, r.Header.Get("X-Request-ID"))
	if err != nil {
		writePrivacyError(w, r, err)
		return
	}
	result, err := s.deps.Operations.RequestExportWithOptions(r.Context(), "PRIVACY_PACKAGE", r.PathValue("id"), input.Format, input.Reason, principal.User.ID, r.Header.Get("X-Request-ID"), operations.ExportOptions{TemplateVersion: "PRIVACY-PACKAGE-V1", WatermarkText: "CONFIDENTIAL DATA SUBJECT PACKAGE", FrozenPayload: envelope})
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, result)
}

func (s *Server) createPrivacyLegalHold(w http.ResponseWriter, r *http.Request) {
	if !s.privacyServiceAvailable(w, r) {
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for legal holds.", nil)
		return
	}
	var input struct {
		MSISDN         string     `json:"msisdn"`
		OrganisationID string     `json:"organisationId,omitempty"`
		Scope          string     `json:"scope"`
		Reason         string     `json:"reason"`
		ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
	}
	if err := httpx.DecodeJSON(w, r, 32<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The legal hold request is invalid.", nil)
		return
	}
	result, err := s.deps.PrivacyCases.CreateLegalHold(r.Context(), input.MSISDN, input.OrganisationID, input.Scope, input.Reason, principal.User.ID, r.Header.Get("X-Request-ID"), input.ExpiresAt)
	if err != nil {
		writePrivacyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, result)
}

func (s *Server) submitPrivacyLegalHold(w http.ResponseWriter, r *http.Request) {
	if !s.privacyServiceAvailable(w, r) {
		return
	}
	var input struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		Reason          string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, 16<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The legal hold submission is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	result, err := s.deps.PrivacyCases.SubmitLegalHold(r.Context(), r.PathValue("id"), input.Reason, principal.User.ID, r.Header.Get("X-Request-ID"), input.ExpectedVersion)
	if err != nil {
		writePrivacyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) decidePrivacyLegalHold(w http.ResponseWriter, r *http.Request) {
	if !s.privacyServiceAvailable(w, r) {
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for legal hold decisions.", nil)
		return
	}
	var input struct {
		Approve         bool   `json:"approve"`
		ExpectedVersion int64  `json:"expectedVersion"`
		Reason          string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, 16<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The legal hold decision is invalid.", nil)
		return
	}
	result, err := s.deps.PrivacyCases.DecideLegalHold(r.Context(), r.PathValue("id"), input.Approve, input.Reason, principal.User.ID, r.Header.Get("X-Request-ID"), input.ExpectedVersion)
	if err != nil {
		writePrivacyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) listPrivacyLegalHoldEvents(w http.ResponseWriter, r *http.Request) {
	if !s.privacyServiceAvailable(w, r) {
		return
	}
	request, err := httpx.ParsePage(r, 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE", "The privacy legal-hold event page request is invalid.", nil)
		return
	}
	page, err := s.deps.PrivacyCases.LegalHoldEventsPage(r.Context(), r.PathValue("id"), request.Limit, request.Cursor)
	if errors.Is(err, privacy.ErrInvalidEventCursor) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The privacy legal-hold event page cursor is invalid.", nil)
		return
	}
	if err != nil {
		writePrivacyError(w, r, err)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if err := s.deps.PrivacyCases.RecordAccess(r.Context(), principal.User.ID, "PRIVACY_LEGAL_HOLD_HISTORY_VIEWED", r.PathValue("id"), r.Header.Get("X-Request-ID"), map[string]any{"eventCount": len(page.Items)}); err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}

func (s *Server) listPrivacyLegalHolds(w http.ResponseWriter, r *http.Request) {
	if !s.privacyServiceAvailable(w, r) {
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required to search legal holds.", nil)
		return
	}
	request, err := httpx.ParsePage(r, 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE", "The legal-hold page request is invalid.", nil)
		return
	}
	activeOnly, err := optionalBoolQuery(r, "activeOnly")
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "PRIVACY_ACTIVE_FILTER_INVALID", "The activeOnly filter must be true or false.", nil)
		return
	}
	page, err := s.deps.PrivacyCases.ListLegalHoldsPage(r.Context(), r.URL.Query().Get("msisdn"), activeOnly, request.Limit, request.Cursor)
	if errors.Is(err, privacy.ErrInvalidLegalHoldListCursor) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The legal-hold page cursor is invalid.", nil)
		return
	}
	if err != nil {
		writePrivacyError(w, r, err)
		return
	}
	if err := s.deps.PrivacyCases.RecordAccess(r.Context(), principal.User.ID, "PRIVACY_LEGAL_HOLDS_SEARCHED", "", r.Header.Get("X-Request-ID"), map[string]any{"activeOnly": activeOnly, "resultCount": len(page.Items)}); err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}

func (s *Server) releasePrivacyLegalHold(w http.ResponseWriter, r *http.Request) {
	if !s.privacyServiceAvailable(w, r) {
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required to release a legal hold.", nil)
		return
	}
	var input struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		Reason          string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, 16<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The legal hold release is invalid.", nil)
		return
	}
	result, err := s.deps.PrivacyCases.ReleaseLegalHold(r.Context(), r.PathValue("id"), input.Reason, principal.User.ID, r.Header.Get("X-Request-ID"), input.ExpectedVersion)
	if err != nil {
		writePrivacyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func writePrivacyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, privacy.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "PRIVACY_NOT_FOUND", "The requested privacy record was not found.", nil)
	case errors.Is(err, privacy.ErrConflict):
		httpx.WriteError(w, r, http.StatusConflict, "PRIVACY_CONFLICT", "The privacy record changed or is not eligible for this action.", nil)
	case errors.Is(err, privacy.ErrLegalHold):
		httpx.WriteError(w, r, http.StatusConflict, "PRIVACY_LEGAL_HOLD", "The requested privacy action is blocked by an active legal hold.", nil)
	case errors.Is(err, privacy.ErrInvalid):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "PRIVACY_INVALID", "The privacy request failed validation.", nil)
	default:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "PRIVACY_REJECTED", "The privacy request was rejected.", map[string]any{"detail": err.Error()})
	}
}
