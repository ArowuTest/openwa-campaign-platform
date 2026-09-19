package httpserver

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"campaign-platform/internal/audit"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/jobs"
	"campaign-platform/internal/operations"
	"campaign-platform/internal/shared/httpx"
)

func (s *Server) operationsDashboard(w http.ResponseWriter, r *http.Request) {
	if s.deps.Operations == nil {
		httpx.WriteError(w, r, 503, "OPERATIONS_UNAVAILABLE", "Operations reporting is unavailable.", nil)
		return
	}
	v, err := s.deps.Operations.Dashboard(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, v)
}
func (s *Server) listOperationsIncidents(w http.ResponseWriter, r *http.Request) {
	if s.deps.Operations == nil {
		httpx.WriteError(w, r, 503, "OPERATIONS_UNAVAILABLE", "Operations reporting is unavailable.", nil)
		return
	}
	limit, err := optionalPositiveIntQuery(r, "limit", 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_LIMIT", "The page limit must be between 1 and 500.", nil)
		return
	}
	page, err := s.deps.Operations.ListIncidentsPage(r.Context(), operations.IncidentStatus(strings.ToUpper(r.URL.Query().Get("status"))), limit, strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		if errors.Is(err, operations.ErrInvalidIncidentCursor) {
			httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The incident page cursor is invalid.", nil)
			return
		}
		s.internalError(w, r, err)
		return
	}
	httpx.WriteList(w, 200, page.Items, len(page.Items), page.NextCursor)
}

type createIncidentRequest struct {
	CampaignID      string              `json:"campaignId"`
	SenderSessionID string              `json:"senderSessionId"`
	Category        string              `json:"category"`
	Severity        operations.Severity `json:"severity"`
	Summary         string              `json:"summary"`
	Detail          string              `json:"detail"`
}

func (s *Server) createOperationsIncident(w http.ResponseWriter, r *http.Request) {
	var in createIncidentRequest
	if err := httpx.DecodeJSON(w, r, 128<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The incident request is invalid.", nil)
		return
	}
	p, _ := identity.PrincipalFromContext(r.Context())
	v, err := s.deps.Operations.CreateIncident(r.Context(), operations.Incident{CampaignID: in.CampaignID, SenderSessionID: in.SenderSessionID, Category: in.Category, Severity: in.Severity, Summary: in.Summary, Detail: in.Detail}, p.User.ID, r.Header.Get("X-Request-ID"))
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 201, v)
}

type updateIncidentRequest struct {
	ExpectedVersion int64                     `json:"expectedVersion"`
	Status          operations.IncidentStatus `json:"status"`
	OwnerID         string                    `json:"ownerId"`
	Resolution      string                    `json:"resolution"`
}

func (s *Server) updateOperationsIncident(w http.ResponseWriter, r *http.Request) {
	var in updateIncidentRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The incident update is invalid.", nil)
		return
	}
	p, _ := identity.PrincipalFromContext(r.Context())
	v, err := s.deps.Operations.UpdateIncident(r.Context(), r.PathValue("id"), in.ExpectedVersion, in.Status, in.OwnerID, in.Resolution, p.User.ID, r.Header.Get("X-Request-ID"))
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, v)
}
func (s *Server) getCampaignReport(w http.ResponseWriter, r *http.Request) {
	v, err := s.deps.Operations.CampaignReport(r.Context(), r.PathValue("id"))
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, v)
}

func (s *Server) getCampaignFinancialReconciliation(w http.ResponseWriter, r *http.Request) {
	v, err := s.deps.Operations.CampaignFinancialReconciliation(r.Context(), r.PathValue("id"))
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (s *Server) getOrganisationPerformanceReport(w http.ResponseWriter, r *http.Request) {
	v, err := s.deps.Operations.OrganisationPerformanceReport(r.Context(), r.PathValue("id"))
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

type exportRequestInput struct {
	Kind            string         `json:"kind"`
	ObjectID        string         `json:"objectId"`
	Format          string         `json:"format"`
	Reason          string         `json:"reason"`
	Criteria        map[string]any `json:"criteria,omitempty"`
	TemplateVersion string         `json:"templateVersion,omitempty"`
	WatermarkText   string         `json:"watermarkText,omitempty"`
}

func (s *Server) requestExport(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(p.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for export actions.", nil)
		return
	}
	var in exportRequestInput
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The export request is invalid.", nil)
		return
	}
	v, err := s.deps.Operations.RequestExportWithOptions(r.Context(), in.Kind, in.ObjectID, in.Format, in.Reason, p.User.ID, r.Header.Get("X-Request-ID"), operations.ExportOptions{Criteria: in.Criteria, TemplateVersion: in.TemplateVersion, WatermarkText: in.WatermarkText})
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 202, v)
}

func (s *Server) listExports(w http.ResponseWriter, r *http.Request) {
	if s.deps.Operations == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "OPERATIONS_UNAVAILABLE", "Export operations are unavailable.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	query := operations.ExportQuery{Status: operations.ExportStatus(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))), Kind: strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("kind")))}
	var err error
	query.Limit, err = optionalPositiveIntQuery(r, "limit", 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_LIMIT", "The page limit must be between 1 and 500.", nil)
		return
	}
	if !principal.User.HasPermission("export.approve") {
		query.RequestedBy = principal.User.ID
	}
	if cursor := strings.TrimSpace(r.URL.Query().Get("after")); cursor != "" {
		parts := strings.SplitN(cursor, "|", 2)
		if len(parts) != 2 {
			httpx.WriteError(w, r, http.StatusBadRequest, "EXPORT_CURSOR_INVALID", "The export cursor is invalid.", nil)
			return
		}
		parsed, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "EXPORT_CURSOR_INVALID", "The export cursor is invalid.", nil)
			return
		}
		query.AfterCreatedAt, query.AfterID = &parsed, parts[1]
	}
	page, err := s.deps.Operations.ListExports(r.Context(), query)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextAfter)
}

func (s *Server) getExport(w http.ResponseWriter, r *http.Request) {
	if s.deps.Operations == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "OPERATIONS_UNAVAILABLE", "Export operations are unavailable.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	export, err := s.deps.Operations.GetExport(r.Context(), r.PathValue("id"))
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	if export.RequestedBy != principal.User.ID && !principal.User.HasPermission("export.approve") {
		httpx.WriteError(w, r, http.StatusForbidden, "EXPORT_ACCESS_DENIED", "You are not permitted to view this export.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, export)
}

func (s *Server) authorizeExportDownload(w http.ResponseWriter, r *http.Request) {
	if s.deps.Operations == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "OPERATIONS_UNAVAILABLE", "Export operations are unavailable.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for export downloads.", nil)
		return
	}
	export, err := s.deps.Operations.GetExport(r.Context(), r.PathValue("id"))
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	if export.RequestedBy != principal.User.ID && !principal.User.HasPermission("export.approve") {
		httpx.WriteError(w, r, http.StatusForbidden, "EXPORT_ACCESS_DENIED", "You are not permitted to download this export.", nil)
		return
	}
	var input struct {
		TTLSeconds int `json:"ttlSeconds,omitempty"`
	}
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(w, r, 16<<10, &input); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The download-authorisation request is invalid.", nil)
			return
		}
	}
	ttl := time.Duration(input.TTLSeconds) * time.Second
	authorisation, err := s.deps.Operations.AuthorizeDownload(r.Context(), export.ID, principal.User.ID, r.Header.Get("X-Request-ID"), ttl)
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, authorisation)
}

func (s *Server) downloadExport(w http.ResponseWriter, r *http.Request) {
	if s.deps.Operations == nil || s.deps.MediaObjects == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "EXPORT_DOWNLOAD_UNAVAILABLE", "Export download is unavailable.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "EXPORT_TOKEN_REQUIRED", "A download token is required.", nil)
		return
	}
	exportID := r.PathValue("id")
	requestID := r.Header.Get("X-Request-ID")
	export, err := s.deps.Operations.GetExport(r.Context(), exportID)
	if err != nil {
		_ = s.deps.Operations.RecordDownloadOutcome(r.Context(), exportID, principal.User.ID, requestID, "FAILURE", "EXPORT_NOT_AVAILABLE", nil)
		writeOperationsError(w, r, err)
		return
	}
	if export.RequestedBy != principal.User.ID && !principal.User.HasPermission("export.approve") {
		_ = s.deps.Operations.RecordDownloadOutcome(r.Context(), exportID, principal.User.ID, requestID, "FAILURE", "ACCESS_DENIED", nil)
		httpx.WriteError(w, r, http.StatusForbidden, "EXPORT_ACCESS_DENIED", "You are not permitted to download this export.", nil)
		return
	}
	object, metadata, err := s.deps.MediaObjects.Open(r.Context(), export.ObjectKey)
	if err != nil {
		_ = s.deps.Operations.RecordDownloadOutcome(r.Context(), exportID, principal.User.ID, requestID, "FAILURE", "OBJECT_OPEN_FAILED", nil)
		s.internalError(w, r, err)
		return
	}
	defer object.Close()
	if metadata.SHA256 != export.SHA256 || metadata.Size != export.SizeBytes {
		_ = s.deps.Operations.RecordDownloadOutcome(r.Context(), exportID, principal.User.ID, requestID, "FAILURE", "OBJECT_INTEGRITY_MISMATCH", map[string]any{"expectedSize": export.SizeBytes, "actualSize": metadata.Size})
		s.internalError(w, r, errors.New("export object integrity mismatch"))
		return
	}
	export, grant, err := s.deps.Operations.ConsumeDownload(r.Context(), exportID, token, principal.User.ID, requestID)
	if err != nil {
		_ = s.deps.Operations.RecordDownloadOutcome(r.Context(), exportID, principal.User.ID, requestID, "FAILURE", "AUTHORISATION_REJECTED", nil)
		writeOperationsError(w, r, err)
		return
	}
	filename := fmt.Sprintf("export-%s.%s", export.ID, strings.ToLower(export.Format))
	w.Header().Set("Content-Type", export.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(export.SizeBytes, 10))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	written, copyErr := io.Copy(w, object)
	if copyErr != nil {
		_ = s.deps.Operations.RecordDownloadOutcome(r.Context(), export.ID, principal.User.ID, requestID, "FAILURE", "STREAM_INTERRUPTED", map[string]any{"grantId": grant.ID, "bytesWritten": written})
		if s.logger != nil {
			s.logger.Error("stream export", "exportId", export.ID, "error", copyErr)
		}
		return
	}
	if err := s.deps.Operations.RecordDownloadOutcome(r.Context(), export.ID, principal.User.ID, requestID, "SUCCESS", "", map[string]any{"grantId": grant.ID, "bytesWritten": written, "sha256": export.SHA256}); err != nil && s.logger != nil {
		s.logger.Error("audit export download completion", "exportId", export.ID, "error", err)
	}
}

func (s *Server) revokeExport(w http.ResponseWriter, r *http.Request) {
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required to revoke an export.", nil)
		return
	}
	var input struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		Reason          string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, 32<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The export revocation request is invalid.", nil)
		return
	}
	result, err := s.deps.Operations.RevokeExport(r.Context(), r.PathValue("id"), input.ExpectedVersion, input.Reason, principal.User.ID, r.Header.Get("X-Request-ID"))
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

type exportDecisionInput struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Approve         bool   `json:"approve"`
	Reason          string `json:"reason"`
}

func (s *Server) decideExport(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(p.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for export actions.", nil)
		return
	}
	var in exportDecisionInput
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The export decision is invalid.", nil)
		return
	}
	v, err := s.deps.Operations.DecideExport(r.Context(), r.PathValue("id"), in.ExpectedVersion, in.Approve, in.Reason, p.User.ID, r.Header.Get("X-Request-ID"))
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, v)
}
func writeOperationsError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, operations.ErrNotFound):
		httpx.WriteError(w, r, 404, "OPERATIONS_NOT_FOUND", "The requested operations record was not found.", nil)
	case errors.Is(err, operations.ErrConflict):
		httpx.WriteError(w, r, 409, "OPERATIONS_CONFLICT", "The record changed or is no longer eligible for this action.", nil)
	case errors.Is(err, operations.ErrApprovalRequired):
		httpx.WriteError(w, r, 409, "DUPLICATE_RISK_APPROVAL_REQUIRED", "Retry can duplicate a message if provider submission cannot be disproven. Review provider evidence and explicitly accept the duplicate-send risk before retrying.", map[string]any{"risk": "POSSIBLE_DUPLICATE_SEND", "approvalField": "duplicateRiskAccepted"})
	case errors.Is(err, operations.ErrInvalid):
		httpx.WriteError(w, r, 422, "OPERATIONS_INVALID", "The operations request failed validation.", nil)
	default:
		httpx.WriteError(w, r, http.StatusInternalServerError, "OPERATIONS_INTERNAL", "The operations request could not be completed.", nil)
	}
}

func (s *Server) searchAuditEvents(w http.ResponseWriter, r *http.Request) {
	if s.deps.Operations == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "OPERATIONS_UNAVAILABLE", "Audit search is unavailable.", nil)
		return
	}
	query := audit.Query{ActorID: strings.TrimSpace(r.URL.Query().Get("actorId")), Action: strings.TrimSpace(r.URL.Query().Get("action")), ObjectType: strings.TrimSpace(r.URL.Query().Get("objectType")), ObjectID: strings.TrimSpace(r.URL.Query().Get("objectId")), OrganisationID: strings.TrimSpace(r.URL.Query().Get("organisationId")), Outcome: strings.TrimSpace(r.URL.Query().Get("outcome")), Sensitivity: strings.TrimSpace(r.URL.Query().Get("sensitivity")), CorrelationID: strings.TrimSpace(r.URL.Query().Get("correlationId")), IPAddress: strings.TrimSpace(r.URL.Query().Get("ipAddress"))}
	var err error
	query.AfterSequence, err = optionalUint64Query(r, "afterSequence")
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "AUDIT_CURSOR_INVALID", "The audit cursor must be a non-negative integer.", nil)
		return
	}
	query.Limit, err = optionalPositiveIntQuery(r, "limit", 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_LIMIT", "The page limit must be between 1 and 500.", nil)
		return
	}
	for key, target := range map[string]**time.Time{"from": &query.From, "to": &query.To} {
		if raw := strings.TrimSpace(r.URL.Query().Get(key)); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				httpx.WriteError(w, r, http.StatusBadRequest, "AUDIT_DATE_INVALID", "Audit date filters must use RFC3339.", map[string]any{"field": key})
				return
			}
			*target = &parsed
		}
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	page, err := s.deps.Operations.SearchAuditWithAccess(r.Context(), query, principal.User.ID, r.Header.Get("X-Request-ID"))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	nextCursor := ""
	if page.NextSequence > 0 {
		nextCursor = strconv.FormatUint(page.NextSequence, 10)
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), nextCursor)
}

func (s *Server) listDeliveryExceptions(w http.ResponseWriter, r *http.Request) {
	limit, err := optionalPositiveIntQuery(r, "limit", 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_LIMIT", "The page limit must be between 1 and 500.", nil)
		return
	}
	page, err := s.deps.Operations.ListExceptionsPage(r.Context(), r.URL.Query().Get("campaignId"), limit, strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		if errors.Is(err, operations.ErrInvalidDeliveryExceptionCursor) {
			httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The delivery-exception page cursor is invalid.", nil)
			return
		}
		s.internalError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}

type deliveryResolutionRequest struct {
	Action                operations.DeliveryResolutionAction `json:"action"`
	EvidenceRef           string                              `json:"evidenceRef"`
	Reason                string                              `json:"reason"`
	DuplicateRiskAccepted bool                                `json:"duplicateRiskAccepted"`
}

func (s *Server) resolveDeliveryException(w http.ResponseWriter, r *http.Request) {
	if s.deps.Operations == nil {
		httpx.WriteError(w, r, 503, "OPERATIONS_UNAVAILABLE", "Delivery reconciliation is unavailable.", nil)
		return
	}
	p, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, 401, "AUTHENTICATION_REQUIRED", "Authentication is required.", nil)
		return
	}
	if !identity.StepUpSatisfied(p.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, 403, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for delivery reconciliation.", nil)
		return
	}
	var in deliveryResolutionRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The delivery-resolution request is invalid.", nil)
		return
	}
	v, err := s.deps.Operations.ResolveDeliveryException(r.Context(), r.PathValue("id"), in.Action, in.EvidenceRef, in.Reason, p.User.ID, r.Header.Get("X-Request-ID"), in.DuplicateRiskAccepted)
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

type operationalJobActionRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) listOperationalJobs(w http.ResponseWriter, r *http.Request) {
	if s.deps.JobOperations == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "JOB_OPERATIONS_UNAVAILABLE", "Job operations are unavailable.", nil)
		return
	}
	limit, err := optionalPositiveIntQuery(r, "limit", 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_LIMIT", "The page limit must be between 1 and 500.", nil)
		return
	}
	var statuses []jobs.Status
	for _, value := range strings.Split(r.URL.Query().Get("status"), ",") {
		if value = strings.TrimSpace(value); value != "" {
			statuses = append(statuses, jobs.Status(strings.ToUpper(value)))
		}
	}
	var types []string
	for _, value := range strings.Split(r.URL.Query().Get("type"), ",") {
		if value = strings.TrimSpace(value); value != "" {
			types = append(types, value)
		}
	}
	page, err := s.deps.JobOperations.ListPage(r.Context(), jobs.Query{Statuses: statuses, Types: types, Limit: limit}, strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		if errors.Is(err, jobs.ErrInvalidJobCursor) {
			httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The job page cursor is invalid.", nil)
			return
		}
		s.internalError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}
func (s *Server) getOperationalJobSummary(w http.ResponseWriter, r *http.Request) {
	if s.deps.JobOperations == nil {
		httpx.WriteError(w, r, 503, "JOB_OPERATIONS_UNAVAILABLE", "Job operations are unavailable.", nil)
		return
	}
	v, err := s.deps.JobOperations.Summary(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, v)
}
func (s *Server) getOperationalJob(w http.ResponseWriter, r *http.Request) {
	if s.deps.JobOperations == nil {
		httpx.WriteError(w, r, 503, "JOB_OPERATIONS_UNAVAILABLE", "Job operations are unavailable.", nil)
		return
	}
	value, err := s.deps.JobOperations.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJobOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, value)
}
func (s *Server) listOperationalJobEvents(w http.ResponseWriter, r *http.Request) {
	if s.deps.JobOperations == nil {
		httpx.WriteError(w, r, 503, "JOB_OPERATIONS_UNAVAILABLE", "Job operations are unavailable.", nil)
		return
	}
	limit, err := optionalPositiveIntQuery(r, "limit", 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_LIMIT", "The page limit must be between 1 and 500.", nil)
		return
	}
	page, err := s.deps.JobOperations.EventsPage(r.Context(), r.PathValue("id"), limit, strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		if errors.Is(err, jobs.ErrInvalidAdministrationEventCursor) {
			httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The job administration-event page cursor is invalid.", nil)
			return
		}
		writeJobOperationsError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}
func (s *Server) retryOperationalJob(w http.ResponseWriter, r *http.Request) {
	s.performOperationalJobAction(w, r, true)
}
func (s *Server) cancelOperationalJob(w http.ResponseWriter, r *http.Request) {
	s.performOperationalJobAction(w, r, false)
}
func (s *Server) performOperationalJobAction(w http.ResponseWriter, r *http.Request, retry bool) {
	if s.deps.JobOperations == nil {
		httpx.WriteError(w, r, 503, "JOB_OPERATIONS_UNAVAILABLE", "Job operations are unavailable.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, 403, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for job operations.", nil)
		return
	}
	var in operationalJobActionRequest
	if err := httpx.DecodeJSON(w, r, 32<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The job action is invalid.", nil)
		return
	}
	var v jobs.Job
	var err error
	if retry {
		v, err = s.deps.JobOperations.Retry(r.Context(), r.PathValue("id"), principal.User.ID, in.Reason)
	} else {
		v, err = s.deps.JobOperations.Cancel(r.Context(), r.PathValue("id"), principal.User.ID, in.Reason)
	}
	if err != nil {
		writeJobOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, v)
}
func writeJobOperationsError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, jobs.ErrNotFound):
		httpx.WriteError(w, r, 404, "JOB_NOT_FOUND", "The job was not found.", nil)
	case errors.Is(err, jobs.ErrAdministrativeConflict):
		httpx.WriteError(w, r, 409, "JOB_STATE_CONFLICT", "The job is not eligible for this action.", nil)
	default:
		httpx.WriteError(w, r, 422, "JOB_OPERATION_REJECTED", "The job operation was rejected.", map[string]any{"detail": err.Error()})
	}
}

type reportingPrivacyPolicyRequest struct {
	OrganisationID    string     `json:"organisationId,omitempty"`
	MinimumCohortSize int        `json:"minimumCohortSize"`
	SuppressionLabel  string     `json:"suppressionLabel,omitempty"`
	ApplyGeography    bool       `json:"applyGeography"`
	ApplyDemographics bool       `json:"applyDemographics"`
	ApplyAttributes   bool       `json:"applyAttributes"`
	EffectiveFrom     time.Time  `json:"effectiveFrom,omitempty"`
	EffectiveTo       *time.Time `json:"effectiveTo,omitempty"`
	Reason            string     `json:"reason"`
}

func (s *Server) reportingPrivacyAdministration(w http.ResponseWriter, r *http.Request) *operations.ReportingPrivacyAdministration {
	if s.deps.Operations == nil || s.deps.Operations.ReportingPrivacy == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "REPORTING_PRIVACY_UNAVAILABLE", "Reporting privacy administration is unavailable.", nil)
		return nil
	}
	return s.deps.Operations.ReportingPrivacy
}

func (s *Server) listReportingPrivacyPolicies(w http.ResponseWriter, r *http.Request) {
	admin := s.reportingPrivacyAdministration(w, r)
	if admin == nil {
		return
	}
	request, err := httpx.ParsePage(r, 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE", "The reporting-privacy page request is invalid.", nil)
		return
	}
	page, err := admin.ListPage(r.Context(), strings.TrimSpace(r.URL.Query().Get("organisationId")), request.Limit, request.Cursor)
	if errors.Is(err, operations.ErrInvalidReportingPrivacyListCursor) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The reporting-privacy page cursor is invalid.", nil)
		return
	}
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}

func (s *Server) createReportingPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	admin := s.reportingPrivacyAdministration(w, r)
	if admin == nil {
		return
	}
	var in reportingPrivacyPolicyRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The reporting privacy policy is invalid.", nil)
		return
	}
	p, _ := identity.PrincipalFromContext(r.Context())
	out, err := admin.Create(r.Context(), operations.ReportingPrivacyPolicy{OrganisationID: in.OrganisationID, MinimumCohortSize: in.MinimumCohortSize, SuppressionLabel: in.SuppressionLabel, ApplyGeography: in.ApplyGeography, ApplyDemographics: in.ApplyDemographics, ApplyAttributes: in.ApplyAttributes, EffectiveFrom: in.EffectiveFrom, EffectiveTo: in.EffectiveTo}, p.User.ID, in.Reason, r.Header.Get("X-Request-ID"))
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (s *Server) getReportingPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	admin := s.reportingPrivacyAdministration(w, r)
	if admin == nil {
		return
	}
	out, err := admin.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
func (s *Server) listReportingPrivacyPolicyEvents(w http.ResponseWriter, r *http.Request) {
	admin := s.reportingPrivacyAdministration(w, r)
	if admin == nil {
		return
	}
	request, err := httpx.ParsePage(r, 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE", "The reporting-privacy event page request is invalid.", nil)
		return
	}
	page, err := admin.EventsPage(r.Context(), r.PathValue("id"), request.Limit, request.Cursor)
	if errors.Is(err, operations.ErrInvalidReportingPrivacyEventCursor) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The reporting-privacy event page cursor is invalid.", nil)
		return
	}
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}

type reportingPrivacyTransitionRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason"`
	Approve         bool   `json:"approve,omitempty"`
}

func (s *Server) submitReportingPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	admin := s.reportingPrivacyAdministration(w, r)
	if admin == nil {
		return
	}
	var in reportingPrivacyTransitionRequest
	if err := httpx.DecodeJSON(w, r, 32<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The policy transition is invalid.", nil)
		return
	}
	p, _ := identity.PrincipalFromContext(r.Context())
	out, err := admin.Submit(r.Context(), r.PathValue("id"), in.ExpectedVersion, p.User.ID, in.Reason, r.Header.Get("X-Request-ID"))
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
func (s *Server) decideReportingPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	admin := s.reportingPrivacyAdministration(w, r)
	if admin == nil {
		return
	}
	p, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(p.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, 403, "STEP_UP_REQUIRED", "Recent multi-factor verification is required.", nil)
		return
	}
	var in reportingPrivacyTransitionRequest
	if err := httpx.DecodeJSON(w, r, 32<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The policy decision is invalid.", nil)
		return
	}
	out, err := admin.Decide(r.Context(), r.PathValue("id"), in.ExpectedVersion, in.Approve, p.User.ID, in.Reason, r.Header.Get("X-Request-ID"))
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
func (s *Server) retireReportingPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	admin := s.reportingPrivacyAdministration(w, r)
	if admin == nil {
		return
	}
	p, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(p.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, 403, "STEP_UP_REQUIRED", "Recent multi-factor verification is required.", nil)
		return
	}
	var in reportingPrivacyTransitionRequest
	if err := httpx.DecodeJSON(w, r, 32<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The policy retirement is invalid.", nil)
		return
	}
	out, err := admin.Retire(r.Context(), r.PathValue("id"), in.ExpectedVersion, p.User.ID, in.Reason, r.Header.Get("X-Request-ID"))
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
func (s *Server) resolveReportingPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	admin := s.reportingPrivacyAdministration(w, r)
	if admin == nil {
		return
	}
	at := time.Now().UTC()
	if value := strings.TrimSpace(r.URL.Query().Get("at")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			httpx.WriteError(w, r, 400, "REPORTING_PRIVACY_TIME_INVALID", "The resolution time is invalid.", nil)
			return
		}
		at = parsed
	}
	out, err := admin.Resolve(r.Context(), strings.TrimSpace(r.URL.Query().Get("organisationId")), at)
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
