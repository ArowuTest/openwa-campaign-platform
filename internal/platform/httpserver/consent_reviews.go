package httpserver

import (
	"errors"
	"net/http"
	"time"

	"campaign-platform/internal/consent"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/shared/httpx"
)

func (s *Server) getConsentReview(w http.ResponseWriter, r *http.Request) {
	entity, err := s.deps.ConsentReviews.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, consent.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CONSENT_REVIEW_NOT_FOUND", "The consent review was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, entity)
}

func (s *Server) submitConsentReview(w http.ResponseWriter, r *http.Request) {
	var input consent.SubmitInput
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The consent-review submission is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.ActorID = principal.User.ID
	entity, err := s.deps.ConsentReviews.Submit(r.Context(), r.PathValue("id"), input)
	writeConsentReviewMutation(w, r, entity, err, "CONSENT_SUBMISSION_INVALID", "The consent review could not be submitted.")
}

func (s *Server) revokeConsentReview(w http.ResponseWriter, r *http.Request) {
	var input consent.RevokeInput
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The consent-review revocation is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "MFA_STEP_UP_REQUIRED", "Recent MFA verification is required.", nil)
		return
	}
	input.ActorID = principal.User.ID
	entity, err := s.deps.ConsentReviews.Revoke(r.Context(), r.PathValue("id"), input)
	writeConsentReviewMutation(w, r, entity, err, "CONSENT_REVOCATION_INVALID", "The consent review could not be revoked.")
}

type consentRevisionRequest struct {
	ExpectedVersion int64               `json:"expectedVersion"`
	Reason          string              `json:"reason"`
	Review          consent.CreateInput `json:"review"`
}

func (s *Server) createConsentReviewRevision(w http.ResponseWriter, r *http.Request) {
	var request consentRevisionRequest
	if err := httpx.DecodeJSON(w, r, 2<<20, &request); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The consent-review revision is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	request.Review.CreatedBy = principal.User.ID
	request.Review.ParentReviewID = r.PathValue("id")
	request.Review.RevisionReason = request.Reason
	entity, err := s.deps.ConsentReviews.CreateRevision(r.Context(), r.PathValue("id"), request.ExpectedVersion, request.Review)
	if errors.Is(err, consent.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CONSENT_REVIEW_NOT_FOUND", "The consent review was not found.", nil)
		return
	}
	if errors.Is(err, consent.ErrConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "CONSENT_VERSION_CONFLICT", "The consent review changed; reload before creating a revision.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CONSENT_REVISION_INVALID", "The consent-review revision was rejected.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, entity)
}

func writeConsentReviewMutation(w http.ResponseWriter, r *http.Request, entity consent.Review, err error, code, message string) {
	if errors.Is(err, consent.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CONSENT_REVIEW_NOT_FOUND", "The consent review was not found.", nil)
		return
	}
	if errors.Is(err, consent.ErrConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "CONSENT_VERSION_CONFLICT", "The consent review changed; reload and try again.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, code, message, map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, entity)
}
