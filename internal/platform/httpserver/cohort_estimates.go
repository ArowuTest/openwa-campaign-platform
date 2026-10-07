package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/shared/httpx"
)

type scheduleCohortEstimateRequest struct {
	OrganisationID  string               `json:"organisationId"`
	PurposeID       string               `json:"purposeId"`
	Channel         string               `json:"channel"`
	Definition      audiencefilter.Group `json:"definition"`
	ClientRequestID string               `json:"clientRequestId"`
}

type cohortEstimateResponse struct {
	ID              string                            `json:"id"`
	OrganisationID  string                            `json:"organisationId"`
	PurposeID       string                            `json:"purposeId"`
	Channel         string                            `json:"channel"`
	AsOf            time.Time                         `json:"asOf"`
	RequestedBy     string                            `json:"requestedBy"`
	ClientRequestID string                            `json:"clientRequestId"`
	Status          string                            `json:"status"`
	AttemptCount    int                               `json:"attemptCount"`
	MaxAttempts     int                               `json:"maxAttempts"`
	LastErrorCode   string                            `json:"lastErrorCode,omitempty"`
	Result          *cohort.Estimate                  `json:"result,omitempty"`
	Evidence        cohort.EstimateGovernanceEvidence `json:"evidence,omitempty"`
	CreatedAt       time.Time                         `json:"createdAt"`
	UpdatedAt       time.Time                         `json:"updatedAt"`
	CompletedAt     *time.Time                        `json:"completedAt,omitempty"`
}

func newCohortEstimateResponse(record cohort.EstimateJobRecord) cohortEstimateResponse {
	return cohortEstimateResponse{
		ID: record.ID, OrganisationID: record.OrganisationID, PurposeID: record.PurposeID, Channel: record.Channel,
		AsOf: record.AsOf, RequestedBy: record.RequestedBy, ClientRequestID: record.ClientRequestID,
		Status: string(record.Job.Status), AttemptCount: record.Job.AttemptCount, MaxAttempts: record.Job.MaxAttempts,
		LastErrorCode: record.Job.LastErrorCode, Result: record.Result, Evidence: record.Evidence,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, CompletedAt: record.Job.CompletedAt,
	}
}

func (s *Server) scheduleCohortEstimate(w http.ResponseWriter, r *http.Request) {
	if s.deps.CohortEstimates == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "COHORT_ESTIMATE_QUEUE_UNAVAILABLE", "Durable cohort estimation is unavailable.", nil)
		return
	}
	var input scheduleCohortEstimateRequest
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The cohort estimate request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.User.ID) == "" {
		httpx.WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "Authentication is required.", nil)
		return
	}
	if s.deps.FilterDefinitions != nil {
		if err := s.deps.FilterDefinitions.Refresh(r.Context()); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	record, created, err := s.deps.CohortEstimates.Schedule(r.Context(), cohort.EstimateJobRequest{
		OrganisationID:  input.OrganisationID,
		PurposeID:       input.PurposeID,
		Channel:         input.Channel,
		Definition:      input.Definition,
		RequestedBy:     principal.User.ID,
		ClientRequestID: input.ClientRequestID,
	}, principal.User.HasPermission)
	if errors.Is(err, cohort.ErrEstimateReplayConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "COHORT_ESTIMATE_REPLAY_CONFLICT", "The client request ID was already used for different estimate content.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COHORT_ESTIMATE_REJECTED", "The cohort estimate could not be scheduled.", map[string]any{"detail": err.Error()})
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusAccepted
	}
	httpx.WriteJSON(w, status, newCohortEstimateResponse(record))
}

func (s *Server) listCohortEstimates(w http.ResponseWriter, r *http.Request) {
	if s.deps.CohortEstimates == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "COHORT_ESTIMATE_QUEUE_UNAVAILABLE", "Durable cohort estimation is unavailable.", nil)
		return
	}
	organisationID := strings.TrimSpace(r.URL.Query().Get("organisationId"))
	if organisationID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "ORGANISATION_REQUIRED", "organisationId is required.", nil)
		return
	}
	limit, err := optionalPositiveIntQuery(r, "limit", 200)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_LIMIT", "The page limit must be between 1 and 200.", nil)
		return
	}
	page, err := s.deps.CohortEstimates.ListPage(r.Context(), organisationID, limit, strings.TrimSpace(r.URL.Query().Get("cursor")))
	if errors.Is(err, cohort.ErrInvalidEstimateCursor) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The cohort-estimate page cursor is invalid.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	items := make([]cohortEstimateResponse, 0, len(page.Items))
	for _, record := range page.Items {
		items = append(items, newCohortEstimateResponse(record))
	}
	httpx.WriteList(w, http.StatusOK, items, len(items), page.NextCursor)
}

func (s *Server) getCohortEstimate(w http.ResponseWriter, r *http.Request) {
	if s.deps.CohortEstimates == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "COHORT_ESTIMATE_QUEUE_UNAVAILABLE", "Durable cohort estimation is unavailable.", nil)
		return
	}
	organisationID := strings.TrimSpace(r.URL.Query().Get("organisationId"))
	if organisationID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "ORGANISATION_REQUIRED", "organisationId is required.", nil)
		return
	}
	record, err := s.deps.CohortEstimates.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, cohort.ErrEstimateNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "COHORT_ESTIMATE_NOT_FOUND", "The cohort estimate was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if record.OrganisationID != organisationID {
		httpx.WriteError(w, r, http.StatusNotFound, "COHORT_ESTIMATE_NOT_FOUND", "The cohort estimate was not found.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newCohortEstimateResponse(record))
}
