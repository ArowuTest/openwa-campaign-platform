package httpserver

import (
	"errors"
	"net/http"
	"time"

	"campaign-platform/internal/consent"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/organisation"
	"campaign-platform/internal/shared/httpx"
	"campaign-platform/internal/shared/id"
	"campaign-platform/internal/storage"
)

// validateConsentReviewReferences rejects malformed identifiers before SQL casts
// and avoids exposing PostgreSQL errors as operator validation messages.
func validateConsentReviewReferences(input consent.CreateInput, requireOrganisation bool) *consent.InputError {
	if requireOrganisation && !id.IsUUID(input.OrganisationID) {
		return &consent.InputError{Field: "organisationId", Reason: "organisation ID must be a valid UUID"}
	}
	if input.Scope == consent.ReviewScopeCampaign && !id.IsUUID(input.CampaignID) {
		return &consent.InputError{Field: "campaignId", Reason: "campaign ID must be a valid UUID"}
	}
	for _, assetID := range input.EvidenceAssetIDs {
		if !id.IsUUID(assetID) {
			return &consent.InputError{Field: "evidenceAssetIds", Reason: "each trusted evidence asset ID must be a valid UUID"}
		}
	}
	return nil
}

// writeConsentCreationError exposes ONLY allowlisted, typed validation errors.
// All repository, network, and unexpected failures follow the safe internal error path.
func (s *Server) writeConsentCreationError(w http.ResponseWriter, r *http.Request, err error) {
	var inputError *consent.InputError
	switch {
	case errors.As(err, &inputError):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CONSENT_REVIEW_INVALID",
			"Correct the indicated consent review field.", map[string]any{"fields": map[string]string{inputError.Field: inputError.Reason}})
	case errors.Is(err, organisation.ErrNotActive):
		httpx.WriteError(w, r, http.StatusConflict, "ORGANISATION_NOT_ACTIVE",
			"The selected organisation must be active before creating a consent review.",
			map[string]any{"fields": map[string]string{"organisationId": "Select an active organisation."}})
	case errors.Is(err, organisation.ErrNotFound):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "ORGANISATION_NOT_FOUND",
			"The selected organisation was not found.",
			map[string]any{"fields": map[string]string{"organisationId": "Select an existing organisation."}})
	case errors.Is(err, storage.ErrAssetNotFound):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CONSENT_EVIDENCE_NOT_FOUND",
			"The selected trusted consent evidence asset was not found.",
			map[string]any{"fields": map[string]string{"evidenceAssetIds": "Use an existing trusted evidence asset ID, or leave it blank to save a draft."}})
	case errors.Is(err, storage.ErrAssetNotClean):
		httpx.WriteError(w, r, http.StatusConflict, "CONSENT_EVIDENCE_NOT_CLEAN",
			"Trusted consent evidence must pass malware scanning before use.",
			map[string]any{"fields": map[string]string{"evidenceAssetIds": "Choose an asset with a CLEAN scan status."}})
	case errors.Is(err, consent.ErrDuplicate):
		httpx.WriteError(w, r, http.StatusConflict, "CONSENT_REVIEW_CONFLICT",
			"A matching consent review already exists.", nil)
	default:
		s.internalError(w, r, err)
	}
}

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
	if issue := validateConsentReviewReferences(request.Review, false); issue != nil {
		s.writeConsentCreationError(w, r, issue)
		return
	}
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
		s.writeConsentCreationError(w, r, err)
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
