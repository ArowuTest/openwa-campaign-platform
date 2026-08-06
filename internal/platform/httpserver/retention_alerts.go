package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"campaign-platform/internal/operations"
	"campaign-platform/internal/retention"
	"campaign-platform/internal/shared/httpx"
)

type retentionRequest struct {
	Name              string               `json:"name"`
	ObjectType        retention.ObjectType `json:"objectType"`
	Action            retention.Action     `json:"action"`
	ScopeType         retention.ScopeType  `json:"scopeType"`
	ScopeID           string               `json:"scopeId"`
	RetentionDays     int                  `json:"retentionDays"`
	RespectLegalHolds bool                 `json:"respectLegalHolds"`
	EffectiveFrom     *time.Time           `json:"effectiveFrom"`
	EffectiveTo       *time.Time           `json:"effectiveTo"`
	ExpectedVersion   int64                `json:"expectedVersion"`
	Approve           bool                 `json:"approve"`
	Reason            string               `json:"reason"`
}

func (s *Server) requireRetention(w http.ResponseWriter, r *http.Request) (*retention.Administration, bool) {
	if s.deps.Retention == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "RETENTION_GOVERNANCE_UNAVAILABLE", "Retention governance is unavailable.", nil)
		return nil, false
	}
	return s.deps.Retention, true
}
func (s *Server) listRetentionPolicies(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireRetention(w, r)
	if !ok {
		return
	}
	limit, err := parseOptionalPositiveInt(r.URL.Query().Get("limit"), 100, 500)
	if err != nil {
		httpx.WriteError(w, r, 400, "INVALID_LIMIT", "The page limit is invalid.", nil)
		return
	}
	status := retention.Status(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))))
	if !validRetentionPolicyStatus(status) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_STATUS", "The retention policy status is invalid.", nil)
		return
	}
	v, err := a.List(r.Context(), status, limit)
	if err != nil {
		s.writeRetentionError(w, r, err)
		return
	}
	httpx.WriteListAuto(w, 200, v)
}
func (s *Server) getRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireRetention(w, r)
	if !ok {
		return
	}
	v, err := a.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeRetentionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, v)
}
func (s *Server) createRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireRetention(w, r)
	if !ok {
		return
	}
	var in retentionRequest
	if httpx.DecodeJSON(w, r, 64<<10, &in) != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The retention policy request is invalid.", nil)
		return
	}
	v := retention.Policy{Name: in.Name, ObjectType: in.ObjectType, Action: in.Action, ScopeType: in.ScopeType, ScopeID: in.ScopeID, RetentionDays: in.RetentionDays, RespectLegalHolds: in.RespectLegalHolds, EffectiveTo: in.EffectiveTo}
	if in.EffectiveFrom != nil {
		v.EffectiveFrom = in.EffectiveFrom.UTC()
	}
	out, err := a.Create(r.Context(), v, platformPolicyActor(r), in.Reason)
	if err != nil {
		s.writeRetentionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 201, out)
}
func (s *Server) submitRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireRetention(w, r)
	if !ok {
		return
	}
	var in retentionRequest
	if httpx.DecodeJSON(w, r, 32<<10, &in) != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The retention submission is invalid.", nil)
		return
	}
	out, err := a.Submit(r.Context(), r.PathValue("id"), in.ExpectedVersion, platformPolicyActor(r), in.Reason)
	if err != nil {
		s.writeRetentionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
func (s *Server) decideRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireRetention(w, r)
	if !ok || !s.requirePlatformPolicyStepUp(w, r, "retention.decision") {
		return
	}
	var in retentionRequest
	if httpx.DecodeJSON(w, r, 32<<10, &in) != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The retention decision is invalid.", nil)
		return
	}
	out, err := a.Decide(r.Context(), r.PathValue("id"), in.ExpectedVersion, in.Approve, platformPolicyActor(r), in.Reason)
	if err != nil {
		s.writeRetentionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
func (s *Server) retireRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireRetention(w, r)
	if !ok || !s.requirePlatformPolicyStepUp(w, r, "retention.retire") {
		return
	}
	var in retentionRequest
	if httpx.DecodeJSON(w, r, 32<<10, &in) != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The retention retirement is invalid.", nil)
		return
	}
	out, err := a.Retire(r.Context(), r.PathValue("id"), in.ExpectedVersion, platformPolicyActor(r), in.Reason)
	if err != nil {
		s.writeRetentionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
func (s *Server) listRetentionPolicyEvents(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireRetention(w, r)
	if !ok {
		return
	}
	v, err := a.Events(r.Context(), r.PathValue("id"), 500)
	if err != nil {
		s.writeRetentionError(w, r, err)
		return
	}
	httpx.WriteListAuto(w, 200, v)
}
func (s *Server) listRetentionJobs(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireRetention(w, r)
	if !ok {
		return
	}
	limit, err := parseOptionalPositiveInt(r.URL.Query().Get("limit"), 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_LIMIT", "The retention-job page limit is invalid.", nil)
		return
	}
	status := retention.JobStatus(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))))
	if !validRetentionJobStatus(status) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_STATUS", "The retention-job status is invalid.", nil)
		return
	}
	v, err := a.Jobs(r.Context(), status, limit)
	if err != nil {
		s.writeRetentionError(w, r, err)
		return
	}
	httpx.WriteListAuto(w, 200, v)
}

func validRetentionPolicyStatus(v retention.Status) bool {
	switch v {
	case "", retention.StatusDraft, retention.StatusPendingApproval, retention.StatusActive, retention.StatusRejected, retention.StatusRetired:
		return true
	default:
		return false
	}
}

func validRetentionJobStatus(v retention.JobStatus) bool {
	switch v {
	case "", retention.JobPending, retention.JobClaimed, retention.JobCompleted, retention.JobFailed, retention.JobHeldReview:
		return true
	default:
		return false
	}
}
func (s *Server) writeRetentionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, retention.ErrNotFound):
		httpx.WriteError(w, r, 404, "RETENTION_NOT_FOUND", "The retention record does not exist.", nil)
	case errors.Is(err, retention.ErrConflict):
		httpx.WriteError(w, r, 409, "RETENTION_CONFLICT", "The retention record changed or cannot transition.", nil)
	case errors.Is(err, retention.ErrInvalid):
		httpx.WriteError(w, r, 422, "RETENTION_INVALID", "The retention record is invalid.", nil)
	default:
		s.internalError(w, r, err)
	}
}

type alertPolicyRequest struct {
	Name                   string                      `json:"name"`
	Metric                 operations.AlertMetric      `json:"metric"`
	Comparison             operations.Comparison       `json:"comparison"`
	Threshold              float64                     `json:"threshold"`
	Severity               operations.Severity         `json:"severity"`
	ConsecutiveEvaluations int                         `json:"consecutiveEvaluations"`
	CooldownSeconds        int                         `json:"cooldownSeconds"`
	AutoIncident           bool                        `json:"autoIncident"`
	EscalationSteps        []operations.EscalationStep `json:"escalationSteps"`
	EffectiveFrom          *time.Time                  `json:"effectiveFrom"`
	EffectiveTo            *time.Time                  `json:"effectiveTo"`
	ExpectedVersion        int64                       `json:"expectedVersion"`
	Approve                bool                        `json:"approve"`
	Reason                 string                      `json:"reason"`
}

func (s *Server) requireAlertPolicies(w http.ResponseWriter, r *http.Request) (*operations.AlertAdministration, bool) {
	if s.deps.AlertPolicies == nil {
		httpx.WriteError(w, r, 503, "ALERT_GOVERNANCE_UNAVAILABLE", "Operational alert governance is unavailable.", nil)
		return nil, false
	}
	return s.deps.AlertPolicies, true
}
func (s *Server) listAlertPolicies(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAlertPolicies(w, r)
	if !ok {
		return
	}
	status := operations.AlertPolicyStatus(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))))
	if !validAlertPolicyStatus(status) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_STATUS", "The alert-policy status is invalid.", nil)
		return
	}
	limit, err := parseOptionalPositiveInt(r.URL.Query().Get("limit"), 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_LIMIT", "The alert-policy page limit is invalid.", nil)
		return
	}
	v, err := a.ListPolicies(r.Context(), status, limit)
	if err != nil {
		s.writeOperationsAlertError(w, r, err)
		return
	}
	httpx.WriteListAuto(w, 200, v)
}
func (s *Server) createAlertPolicy(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAlertPolicies(w, r)
	if !ok {
		return
	}
	var in alertPolicyRequest
	if httpx.DecodeJSON(w, r, 64<<10, &in) != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The alert policy request is invalid.", nil)
		return
	}
	v := operations.AlertPolicy{Name: in.Name, Metric: in.Metric, Comparison: in.Comparison, Threshold: in.Threshold, Severity: in.Severity, ConsecutiveEvaluations: in.ConsecutiveEvaluations, CooldownSeconds: in.CooldownSeconds, AutoIncident: in.AutoIncident, EscalationSteps: in.EscalationSteps, EffectiveTo: in.EffectiveTo}
	if in.EffectiveFrom != nil {
		v.EffectiveFrom = in.EffectiveFrom.UTC()
	}
	out, err := a.CreatePolicy(r.Context(), v, platformPolicyActor(r), in.Reason)
	if err != nil {
		s.writeOperationsAlertError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 201, out)
}
func (s *Server) submitAlertPolicy(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAlertPolicies(w, r)
	if !ok {
		return
	}
	var in alertPolicyRequest
	if httpx.DecodeJSON(w, r, 32<<10, &in) != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The alert policy submission is invalid.", nil)
		return
	}
	out, err := a.SubmitPolicy(r.Context(), r.PathValue("id"), in.ExpectedVersion, platformPolicyActor(r), in.Reason)
	if err != nil {
		s.writeOperationsAlertError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
func (s *Server) decideAlertPolicy(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAlertPolicies(w, r)
	if !ok || !s.requirePlatformPolicyStepUp(w, r, "alert_policy.decision") {
		return
	}
	var in alertPolicyRequest
	if httpx.DecodeJSON(w, r, 32<<10, &in) != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The alert policy decision is invalid.", nil)
		return
	}
	out, err := a.DecidePolicy(r.Context(), r.PathValue("id"), in.ExpectedVersion, in.Approve, platformPolicyActor(r), in.Reason)
	if err != nil {
		s.writeOperationsAlertError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
func (s *Server) retireAlertPolicy(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAlertPolicies(w, r)
	if !ok || !s.requirePlatformPolicyStepUp(w, r, "alert_policy.retire") {
		return
	}
	var in alertPolicyRequest
	if httpx.DecodeJSON(w, r, 32<<10, &in) != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The alert policy retirement is invalid.", nil)
		return
	}
	out, err := a.RetirePolicy(r.Context(), r.PathValue("id"), in.ExpectedVersion, platformPolicyActor(r), in.Reason)
	if err != nil {
		s.writeOperationsAlertError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
func (s *Server) listAlertPolicyEvents(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAlertPolicies(w, r)
	if !ok {
		return
	}
	v, err := a.PolicyEvents(r.Context(), r.PathValue("id"), 500)
	if err != nil {
		s.writeOperationsAlertError(w, r, err)
		return
	}
	httpx.WriteListAuto(w, 200, v)
}
func (s *Server) listOperationalAlerts(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAlertPolicies(w, r)
	if !ok {
		return
	}
	status := operations.AlertStatus(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))))
	severity := operations.Severity(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("severity"))))
	if !validOperationalAlertStatus(status) || !validOperationalSeverity(severity) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_FILTER", "The alert status or severity filter is invalid.", nil)
		return
	}
	limit, err := parseOptionalPositiveInt(r.URL.Query().Get("limit"), 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_LIMIT", "The alert page limit is invalid.", nil)
		return
	}
	v, err := a.ListAlerts(r.Context(), status, severity, limit)
	if err != nil {
		s.writeOperationsAlertError(w, r, err)
		return
	}
	httpx.WriteListAuto(w, 200, v)
}
func (s *Server) acknowledgeOperationalAlert(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAlertPolicies(w, r)
	if !ok {
		return
	}
	var in alertPolicyRequest
	if httpx.DecodeJSON(w, r, 32<<10, &in) != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The alert acknowledgement is invalid.", nil)
		return
	}
	out, err := a.Acknowledge(r.Context(), r.PathValue("id"), in.ExpectedVersion, platformPolicyActor(r), in.Reason)
	if err != nil {
		s.writeOperationsAlertError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
func (s *Server) listOperationalAlertEvents(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAlertPolicies(w, r)
	if !ok {
		return
	}
	v, err := a.Store.ListAlertEvents(r.Context(), r.PathValue("id"), 500)
	if err != nil {
		s.writeOperationsAlertError(w, r, err)
		return
	}
	httpx.WriteListAuto(w, 200, v)
}
func (s *Server) evaluateOperationalAlerts(w http.ResponseWriter, r *http.Request) {
	if !s.requirePlatformPolicyStepUp(w, r, "alert.evaluate") {
		return
	}
	if s.deps.AlertEvaluator == nil {
		httpx.WriteError(w, r, 503, "ALERT_EVALUATOR_UNAVAILABLE", "Operational alert evaluation is unavailable.", nil)
		return
	}
	n, err := s.deps.AlertEvaluator.Evaluate(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	e, err := s.deps.AlertEvaluator.Escalate(r.Context(), 100)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"evaluatedChanges": n, "escalations": e})
}
func (s *Server) listOperationalNotifications(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requireAlertPolicies(w, r)
	if !ok {
		return
	}
	status := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
	if !validNotificationStatus(status) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_STATUS", "The notification status is invalid.", nil)
		return
	}
	limit, err := parseOptionalPositiveInt(r.URL.Query().Get("limit"), 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_LIMIT", "The notification page limit is invalid.", nil)
		return
	}
	v, err := a.Store.ListNotifications(r.Context(), status, limit)
	if err != nil {
		s.writeOperationsAlertError(w, r, err)
		return
	}
	httpx.WriteListAuto(w, 200, v)
}

func validAlertPolicyStatus(v operations.AlertPolicyStatus) bool {
	switch v {
	case "", operations.AlertPolicyDraft, operations.AlertPolicyPending, operations.AlertPolicyActive, operations.AlertPolicyRejected, operations.AlertPolicyRetired:
		return true
	default:
		return false
	}
}

func validOperationalAlertStatus(v operations.AlertStatus) bool {
	switch v {
	case "", operations.AlertActive, operations.AlertAcknowledged, operations.AlertResolved:
		return true
	default:
		return false
	}
}

func validOperationalSeverity(v operations.Severity) bool {
	switch v {
	case "", operations.SeverityInfo, operations.SeverityWarning, operations.SeverityCritical:
		return true
	default:
		return false
	}
}

func validNotificationStatus(v string) bool {
	switch v {
	case "", "PENDING", "DELIVERED", "FAILED", "CANCELLED":
		return true
	default:
		return false
	}
}
func (s *Server) getOperationsIncidentTimeline(w http.ResponseWriter, r *http.Request) {
	if s.deps.Operations == nil {
		httpx.WriteError(w, r, 503, "OPERATIONS_UNAVAILABLE", "Operations are unavailable.", nil)
		return
	}
	v, err := s.deps.Operations.IncidentTimeline(r.Context(), r.PathValue("id"), 500)
	if err != nil {
		s.writeOperationsAlertError(w, r, err)
		return
	}
	httpx.WriteListAuto(w, 200, v)
}
func (s *Server) writeOperationsAlertError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, operations.ErrNotFound):
		httpx.WriteError(w, r, 404, "OPERATIONS_RECORD_NOT_FOUND", "The operations record does not exist.", nil)
	case errors.Is(err, operations.ErrConflict):
		httpx.WriteError(w, r, 409, "OPERATIONS_CONFLICT", "The operations record changed or cannot transition.", nil)
	case errors.Is(err, operations.ErrInvalid):
		httpx.WriteError(w, r, 422, "OPERATIONS_INVALID", "The operations record is invalid.", nil)
	default:
		s.internalError(w, r, err)
	}
}
