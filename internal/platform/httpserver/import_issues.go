package httpserver

import (
	"campaign-platform/internal/audience/importer"
	"errors"
	"io"
	"net/http"
	"os"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/shared/httpx"
)

func (s *Server) downloadAudienceImportIssues(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceImportIssues == nil || s.deps.AudienceImports == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "IMPORT_ISSUES_UNAVAILABLE", "Audience import issue export is unavailable.", nil)
		return
	}
	importID := r.PathValue("id")
	if _, err := s.deps.AudienceImports.Get(r.Context(), importID); err != nil {
		if errors.Is(err, importer.ErrImportNotFound) {
			httpx.WriteError(w, r, http.StatusNotFound, "AUDIENCE_IMPORT_NOT_FOUND", "The audience import was not found.", nil)
		} else {
			s.internalError(w, r, err)
		}
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	temporary, err := os.CreateTemp("", "audience-import-issues-*.csv")
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err = s.deps.AudienceImportIssues.WriteCSV(r.Context(), importID, principal.User.ID, r.Header.Get("X-Request-ID"), temporary); err != nil {
		temporary.Close()
		s.internalError(w, r, err)
		return
	}
	if _, err = temporary.Seek(0, io.SeekStart); err != nil {
		temporary.Close()
		s.internalError(w, r, err)
		return
	}
	stat, err := temporary.Stat()
	if err != nil {
		temporary.Close()
		s.internalError(w, r, err)
		return
	}
	defer temporary.Close()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audience-import-issues.csv"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "audience-import-issues.csv", stat.ModTime(), temporary)
}
