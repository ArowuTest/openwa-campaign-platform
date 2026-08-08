package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"campaign-platform/internal/audience/importer"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/shared/httpx"
)

func (s *Server) downloadAudienceImportTemplate(w http.ResponseWriter, r *http.Request) {
	payload, contentType, name, err := importer.RenderTemplate(r.PathValue("version"), r.URL.Query().Get("format"))
	if err != nil {
		httpx.WriteError(w, r, 404, "IMPORT_TEMPLATE_NOT_FOUND", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(200)
	_, _ = w.Write(payload)
}

type importRollbackRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason"`
}

func (s *Server) rollbackAudienceImport(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceImportRollback == nil {
		httpx.WriteError(w, r, 503, "IMPORT_ROLLBACK_UNAVAILABLE", "Audience import rollback is unavailable.", nil)
		return
	}
	p, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(p.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, 403, "STEP_UP_REQUIRED", "Recent multi-factor verification is required.", nil)
		return
	}
	var in importRollbackRequest
	if err := httpx.DecodeJSON(w, r, 32<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The rollback request is invalid.", nil)
		return
	}
	out, err := s.deps.AudienceImportRollback.Rollback(r.Context(), r.PathValue("id"), in.ExpectedVersion, p.User.ID, in.Reason, r.Header.Get("X-Request-ID"))
	if err != nil {
		status := 422
		code := "IMPORT_ROLLBACK_REJECTED"
		if err == importer.ErrImportNotFound {
			status = 404
			code = "AUDIENCE_IMPORT_NOT_FOUND"
		}
		if err == importer.ErrImportConflict {
			status = 409
			code = "AUDIENCE_IMPORT_VERSION_CONFLICT"
		}
		if err == importer.ErrRollbackConsumed || err == importer.ErrRollbackSuperseded {
			status = 409
			code = "IMPORT_ROLLBACK_UNSAFE"
		}
		httpx.WriteError(w, r, status, code, "The audience import could not be rolled back.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, 200, out)
}

type importMappingRequest struct {
	OrganisationID  string                 `json:"organisationId,omitempty"`
	Name            string                 `json:"name"`
	SourceSystem    string                 `json:"sourceSystem,omitempty"`
	TemplateVersion string                 `json:"templateVersion"`
	Worksheet       string                 `json:"worksheet,omitempty"`
	Mapping         importer.ColumnMapping `json:"mapping"`
	EffectiveFrom   time.Time              `json:"effectiveFrom,omitempty"`
	EffectiveTo     *time.Time             `json:"effectiveTo,omitempty"`
	Reason          string                 `json:"reason"`
}
type importMappingTransition struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Approve         bool   `json:"approve,omitempty"`
	Reason          string `json:"reason"`
}

func (s *Server) mappingAdmin(w http.ResponseWriter, r *http.Request) *importer.MappingAdministration {
	if s.deps.AudienceImportMappings == nil {
		httpx.WriteError(w, r, 503, "IMPORT_MAPPING_UNAVAILABLE", "Audience import mapping administration is unavailable.", nil)
		return nil
	}
	return s.deps.AudienceImportMappings
}
func (s *Server) listAudienceImportMappings(w http.ResponseWriter, r *http.Request) {
	a := s.mappingAdmin(w, r)
	if a == nil {
		return
	}
	request, err := httpx.ParsePage(r, 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE", "The import-mapping page request is invalid.", nil)
		return
	}
	page, err := a.ListPage(r.Context(), r.URL.Query().Get("organisationId"), r.URL.Query().Get("sourceSystem"), request.Limit, request.Cursor)
	if errors.Is(err, importer.ErrInvalidMappingListCursor) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The import-mapping page cursor is invalid.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}
func (s *Server) createAudienceImportMapping(w http.ResponseWriter, r *http.Request) {
	a := s.mappingAdmin(w, r)
	if a == nil {
		return
	}
	var in importMappingRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The import mapping is invalid.", nil)
		return
	}
	p, _ := identity.PrincipalFromContext(r.Context())
	out, err := a.Create(r.Context(), importer.MappingDefinition{OrganisationID: in.OrganisationID, Name: in.Name, SourceSystem: in.SourceSystem, TemplateVersion: in.TemplateVersion, Worksheet: in.Worksheet, Mapping: in.Mapping, EffectiveFrom: in.EffectiveFrom, EffectiveTo: in.EffectiveTo}, p.User.ID, in.Reason, r.Header.Get("X-Request-ID"))
	if err != nil {
		httpx.WriteError(w, r, 422, "IMPORT_MAPPING_REJECTED", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, 201, out)
}
func (s *Server) getAudienceImportMapping(w http.ResponseWriter, r *http.Request) {
	a := s.mappingAdmin(w, r)
	if a == nil {
		return
	}
	out, err := a.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, r, 404, "IMPORT_MAPPING_NOT_FOUND", "The mapping was not found.", nil)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
func (s *Server) listAudienceImportMappingEvents(w http.ResponseWriter, r *http.Request) {
	a := s.mappingAdmin(w, r)
	if a == nil {
		return
	}
	request, err := httpx.ParsePage(r, 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE", "The mapping-event page request is invalid.", nil)
		return
	}
	page, err := a.EventsPage(r.Context(), r.PathValue("id"), request.Limit, request.Cursor)
	if errors.Is(err, importer.ErrInvalidMappingEventCursor) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The mapping-event page cursor is invalid.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, 404, "IMPORT_MAPPING_NOT_FOUND", "The mapping was not found.", nil)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}
func (s *Server) submitAudienceImportMapping(w http.ResponseWriter, r *http.Request) {
	a := s.mappingAdmin(w, r)
	if a == nil {
		return
	}
	var in importMappingTransition
	if err := httpx.DecodeJSON(w, r, 32<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The mapping transition is invalid.", nil)
		return
	}
	p, _ := identity.PrincipalFromContext(r.Context())
	out, err := a.Submit(r.Context(), r.PathValue("id"), in.ExpectedVersion, p.User.ID, in.Reason, r.Header.Get("X-Request-ID"))
	if err != nil {
		httpx.WriteError(w, r, 409, "IMPORT_MAPPING_TRANSITION_REJECTED", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
func (s *Server) decideAudienceImportMapping(w http.ResponseWriter, r *http.Request) {
	a := s.mappingAdmin(w, r)
	if a == nil {
		return
	}
	p, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(p.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, 403, "STEP_UP_REQUIRED", "Recent multi-factor verification is required.", nil)
		return
	}
	var in importMappingTransition
	if err := httpx.DecodeJSON(w, r, 32<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The mapping decision is invalid.", nil)
		return
	}
	out, err := a.Decide(r.Context(), r.PathValue("id"), in.ExpectedVersion, in.Approve, p.User.ID, in.Reason, r.Header.Get("X-Request-ID"))
	if err != nil {
		httpx.WriteError(w, r, 409, "IMPORT_MAPPING_DECISION_REJECTED", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
func (s *Server) retireAudienceImportMapping(w http.ResponseWriter, r *http.Request) {
	a := s.mappingAdmin(w, r)
	if a == nil {
		return
	}
	p, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(p.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, 403, "STEP_UP_REQUIRED", "Recent multi-factor verification is required.", nil)
		return
	}
	var in importMappingTransition
	if err := httpx.DecodeJSON(w, r, 32<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The mapping retirement is invalid.", nil)
		return
	}
	out, err := a.Retire(r.Context(), r.PathValue("id"), in.ExpectedVersion, p.User.ID, in.Reason, r.Header.Get("X-Request-ID"))
	if err != nil {
		httpx.WriteError(w, r, 409, "IMPORT_MAPPING_RETIREMENT_REJECTED", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
func (s *Server) resolveAudienceImportMapping(w http.ResponseWriter, r *http.Request) {
	a := s.mappingAdmin(w, r)
	if a == nil {
		return
	}
	at := time.Now().UTC()
	if raw := strings.TrimSpace(r.URL.Query().Get("at")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			httpx.WriteError(w, r, 400, "IMPORT_MAPPING_TIME_INVALID", "The resolution time is invalid.", nil)
			return
		}
		at = parsed
	}
	out, err := a.Resolve(r.Context(), r.URL.Query().Get("organisationId"), r.URL.Query().Get("sourceSystem"), r.URL.Query().Get("name"), at)
	if err != nil {
		httpx.WriteError(w, r, 404, "IMPORT_MAPPING_NOT_FOUND", "No active mapping matched the request.", nil)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
