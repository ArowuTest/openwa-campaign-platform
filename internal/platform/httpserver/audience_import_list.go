package httpserver

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"campaign-platform/internal/audience/importer"
	"campaign-platform/internal/shared/httpx"
)

var audienceImportOrganisationIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func (s *Server) listAudienceImports(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceImports == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_IMPORTS_UNAVAILABLE", "Audience import inventory is unavailable.", nil)
		return
	}
	organisationID := strings.TrimSpace(r.URL.Query().Get("organisationId"))
	if organisationID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "ORGANISATION_REQUIRED", "organisationId is required for audience import inventory.", nil)
		return
	}
	if !audienceImportOrganisationIDPattern.MatchString(organisationID) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_ORGANISATION_ID", "organisationId must be a canonical UUID.", nil)
		return
	}
	request, err := httpx.ParsePage(r, 50, 200)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE", "The audience-import page request is invalid.", nil)
		return
	}
	page, err := s.deps.AudienceImports.ListPage(r.Context(), organisationID, request.Limit, request.Cursor)
	if errors.Is(err, importer.ErrInvalidImportListCursor) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The audience-import page cursor is invalid.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}
