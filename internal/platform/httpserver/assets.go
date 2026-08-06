package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/shared/httpx"
	"campaign-platform/internal/storage"
)

func (s *Server) uploadTrustedAsset(w http.ResponseWriter, r *http.Request) {
	if s.deps.TrustedAssets == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "TRUSTED_ASSET_UNAVAILABLE", "Trusted asset intake is unavailable.", nil)
		return
	}
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required.", nil)
		return
	}
	purpose := storage.AssetPurpose(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("purpose"))))
	required := "campaign.write"
	if purpose == storage.AssetPurposeConsentEvidence {
		required = "consent.write"
	}
	if !principal.User.HasPermission(required) {
		httpx.WriteError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "The requested asset purpose is not permitted.", nil)
		return
	}
	maximum := s.deps.TrustedAssets.MaximumBytes
	if maximum <= 0 {
		maximum = 128 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maximum+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "MULTIPART_INVALID", "The asset upload is invalid.", nil)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "FILE_REQUIRED", "A file part is required.", nil)
		return
	}
	defer file.Close()
	asset, err := s.deps.TrustedAssets.Upload(r.Context(), purpose, header.Filename, header.Header.Get("Content-Type"), principal.User.ID, file)
	if errors.Is(err, storage.ErrAssetNotClean) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "ASSET_SCAN_REJECTED", "The uploaded asset did not pass malware scanning.", map[string]any{"asset": asset})
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "ASSET_INTAKE_FAILED", "The asset could not be accepted.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, asset)
}
func (s *Server) getTrustedAsset(w http.ResponseWriter, r *http.Request) {
	if s.deps.TrustedAssets == nil || s.deps.TrustedAssets.Repository == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "TRUSTED_ASSET_UNAVAILABLE", "Trusted assets are unavailable.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !principal.User.HasPermission("campaign.read") && !principal.User.HasPermission("consent.read") {
		httpx.WriteError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Trusted asset metadata is not permitted.", nil)
		return
	}
	asset, err := s.deps.TrustedAssets.Repository.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrAssetNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "ASSET_NOT_FOUND", "The trusted asset was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("X-Asset-Version", strconv.FormatInt(asset.Version, 10))
	httpx.WriteJSON(w, http.StatusOK, asset)
}

type revokeTrustedAssetRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason"`
}

func (s *Server) revokeTrustedAsset(w http.ResponseWriter, r *http.Request) {
	if s.deps.TrustedAssets == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "TRUSTED_ASSET_UNAVAILABLE", "Trusted assets are unavailable.", nil)
		return
	}
	var input revokeTrustedAssetRequest
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The trusted asset revocation is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "MFA_STEP_UP_REQUIRED", "Recent MFA verification is required.", nil)
		return
	}
	asset, err := s.deps.TrustedAssets.Revoke(r.Context(), r.PathValue("id"), input.ExpectedVersion, principal.User.ID, input.Reason)
	if errors.Is(err, storage.ErrAssetNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "ASSET_NOT_FOUND", "The trusted asset was not found.", nil)
		return
	}
	if errors.Is(err, storage.ErrAssetConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "ASSET_VERSION_CONFLICT", "The trusted asset changed; reload before revoking.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "ASSET_REVOCATION_INVALID", "The trusted asset could not be revoked.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, asset)
}
