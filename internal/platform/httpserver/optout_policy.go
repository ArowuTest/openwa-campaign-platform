package httpserver

import (
	"errors"
	"net/http"
	"time"

	"campaign-platform/internal/consent"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/shared/httpx"
)

type createOptOutPolicyRequest struct {
	Keywords      []string   `json:"keywords"`
	EffectiveFrom *time.Time `json:"effectiveFrom,omitempty"`
	Reason        string     `json:"reason"`
}
type transitionOptOutPolicyRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason"`
	Approve         bool   `json:"approve,omitempty"`
}

func (s *Server) listOptOutPolicies(w http.ResponseWriter, r *http.Request) {
	if s.deps.OptOutPolicies == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "OPT_OUT_POLICY_UNAVAILABLE", "Opt-out policy administration is unavailable.", nil)
		return
	}
	req, err := httpx.ParsePage(r, 100, 500)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE", "The opt-out-policy page request is invalid.", nil)
		return
	}
	page, err := s.deps.OptOutPolicies.ListPage(r.Context(), req.Limit, req.Cursor)
	if errors.Is(err, consent.ErrInvalidOptOutPolicyCursor) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_PAGE_CURSOR", "The opt-out-policy page cursor is invalid.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteList(w, http.StatusOK, page.Items, len(page.Items), page.NextCursor)
}
func (s *Server) createOptOutPolicy(w http.ResponseWriter, r *http.Request) {
	if s.deps.OptOutPolicies == nil {
		httpx.WriteError(w, r, 503, "OPT_OUT_POLICY_UNAVAILABLE", "Opt-out policy administration is unavailable.", nil)
		return
	}
	var in createOptOutPolicyRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The opt-out policy is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	p, _ := identity.PrincipalFromContext(r.Context())
	at := time.Time{}
	if in.EffectiveFrom != nil {
		at = *in.EffectiveFrom
	}
	item, err := s.deps.OptOutPolicies.CreateDraft(r.Context(), in.Keywords, at, p.User.ID, in.Reason)
	if err != nil {
		writeOptOutPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 201, item)
}
func (s *Server) submitOptOutPolicy(w http.ResponseWriter, r *http.Request) {
	var in transitionOptOutPolicyRequest
	if err := httpx.DecodeJSON(w, r, 32<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The opt-out policy transition is invalid.", nil)
		return
	}
	p, _ := identity.PrincipalFromContext(r.Context())
	item, err := s.deps.OptOutPolicies.Submit(r.Context(), r.PathValue("id"), in.ExpectedVersion, p.User.ID, in.Reason)
	if err != nil {
		writeOptOutPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, item)
}
func (s *Server) decideOptOutPolicy(w http.ResponseWriter, r *http.Request) {
	var in transitionOptOutPolicyRequest
	if err := httpx.DecodeJSON(w, r, 32<<10, &in); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The opt-out policy decision is invalid.", nil)
		return
	}
	p, _ := identity.PrincipalFromContext(r.Context())
	item, err := s.deps.OptOutPolicies.Decide(r.Context(), r.PathValue("id"), in.ExpectedVersion, in.Approve, p.User.ID, in.Reason)
	if err != nil {
		writeOptOutPolicyError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, item)
}
func writeOptOutPolicyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, consent.ErrOptOutPolicyNotFound):
		httpx.WriteError(w, r, 404, "OPT_OUT_POLICY_NOT_FOUND", "The opt-out policy was not found.", nil)
	case errors.Is(err, consent.ErrOptOutPolicyConflict):
		httpx.WriteError(w, r, 409, "OPT_OUT_POLICY_CONFLICT", "The opt-out policy changed; reload before continuing.", nil)
	case errors.Is(err, consent.ErrOptOutPolicyInvalid):
		httpx.WriteError(w, r, 422, "OPT_OUT_POLICY_INVALID", "The opt-out policy action is not permitted.", nil)
	default:
		httpx.WriteError(w, r, 500, "INTERNAL_ERROR", "The request could not be completed.", nil)
	}
}
