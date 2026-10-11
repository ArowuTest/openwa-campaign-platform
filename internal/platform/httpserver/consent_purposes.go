package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"campaign-platform/internal/consent"
	"campaign-platform/internal/shared/httpx"
)

func (s *Server) listConsentPurposes(w http.ResponseWriter, r *http.Request) {
	if s.deps.ConsentPurposes == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CONSENT_PURPOSES_UNAVAILABLE", "Consent-purpose inventory is unavailable.", nil)
		return
	}
	request, err := httpx.ParsePage(r, 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE", "The consent-purpose page request is invalid.", nil)
		return
	}
	activeOnly := true
	if raw := strings.TrimSpace(r.URL.Query().Get("includeInactive")); raw != "" {
		includeInactive, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_INCLUDE_INACTIVE", "includeInactive must be true or false.", nil)
			return
		}
		activeOnly = !includeInactive
	}
	page, err := s.deps.ConsentPurposes.ListPage(
		r.Context(),
		strings.TrimSpace(r.URL.Query().Get("organisationId")),
		strings.TrimSpace(r.URL.Query().Get("consentReviewId")),
		activeOnly,
		request.Limit,
		request.Cursor,
	)
	if errors.Is(err, consent.ErrInvalidPurposeListCursor) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The consent-purpose page cursor is invalid.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}
