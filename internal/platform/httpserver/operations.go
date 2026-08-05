package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"campaign-platform/internal/identity"
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
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	v, err := s.deps.Operations.ListIncidents(r.Context(), operations.IncidentStatus(strings.ToUpper(r.URL.Query().Get("status"))), limit)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": v, "count": len(v)})
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

type exportRequestInput struct {
	Kind     string `json:"kind"`
	ObjectID string `json:"objectId"`
	Format   string `json:"format"`
	Reason   string `json:"reason"`
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
	v, err := s.deps.Operations.RequestExport(r.Context(), in.Kind, in.ObjectID, in.Format, in.Reason, p.User.ID, r.Header.Get("X-Request-ID"))
	if err != nil {
		writeOperationsError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 202, v)
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
	case errors.Is(err, operations.ErrInvalid):
		httpx.WriteError(w, r, 422, "OPERATIONS_INVALID", "The operations request failed validation.", nil)
	default:
		httpx.WriteError(w, r, 422, "OPERATIONS_REJECTED", "The operations request was rejected.", map[string]any{"detail": err.Error()})
	}
}

func (s *Server) searchAuditEvents(w http.ResponseWriter, r *http.Request) {
	if s.deps.Operations == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "OPERATIONS_UNAVAILABLE", "Audit search is unavailable.", nil)
		return
	}
	after, _ := strconv.ParseUint(r.URL.Query().Get("afterSequence"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.deps.Operations.SearchAudit(r.Context(), after, limit)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) listDeliveryExceptions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.deps.Operations.ListExceptions(r.Context(), r.URL.Query().Get("campaignId"), limit)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}
