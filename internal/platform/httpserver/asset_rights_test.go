package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/security/malware"
	"campaign-platform/internal/storage"
)

type trustedAssetCleanScanner struct{}

func (trustedAssetCleanScanner) Scan(context.Context, io.Reader) (malware.Result, error) {
	return malware.Result{Clean: true}, nil
}

func trustedAssetTestServer(t *testing.T) *Server {
	t.Helper()
	objects, err := storage.NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Server{deps: Dependencies{TrustedAssets: &storage.TrustedAssetService{
		Objects: objects, Repository: storage.NewMemoryAssetRepository(),
		Scanner: trustedAssetCleanScanner{}, MaximumBytes: 1 << 20,
		Clock: func() time.Time { return time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC) },
	}}}
}

func trustedAssetMultipartRequest(t *testing.T, purpose string, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="creative.txt"`)
	header.Set("Content-Type", "text/plain")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("creative payload")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/assets?purpose="+purpose, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	principal := identity.Principal{User: identity.User{
		ID:          "11111111-1111-4111-8111-111111111111",
		Permissions: map[string]struct{}{"campaign.write": {}, "campaign.read": {}, "consent.write": {}, "consent.read": {}},
	}}
	return request.WithContext(identity.WithPrincipal(request.Context(), principal))
}

func TestTrustedAssetMessageMediaRequiresRightsAndPublicResponseHidesStorageAuthority(t *testing.T) {
	server := trustedAssetTestServer(t)

	missing := httptest.NewRecorder()
	server.uploadTrustedAsset(missing, trustedAssetMultipartRequest(t, "MESSAGE_MEDIA", nil))
	if missing.Code != http.StatusUnprocessableEntity || !strings.Contains(missing.Body.String(), "ASSET_RIGHTS_INVALID") {
		t.Fatalf("missing-rights status=%d body=%s", missing.Code, missing.Body.String())
	}

	valid := httptest.NewRecorder()
	server.uploadTrustedAsset(valid, trustedAssetMultipartRequest(t, "MESSAGE_MEDIA", map[string]string{
		"rightsBasis": "LICENSED", "rightsReference": "licence/campaign-creative-001",
		"rightsExpiresAt": "2026-12-31T23:59:59Z",
	}))
	if valid.Code != http.StatusCreated {
		t.Fatalf("valid status=%d body=%s", valid.Code, valid.Body.String())
	}
	for _, forbidden := range []string{"objectKey", "trusted-assets/", "scanSignature"} {
		if strings.Contains(valid.Body.String(), forbidden) {
			t.Fatalf("private storage/scanner field leaked (%s): %s", forbidden, valid.Body.String())
		}
	}
	if !strings.Contains(valid.Body.String(), `"rightsBasis":"LICENSED"`) ||
		!strings.Contains(valid.Body.String(), `"rightsReference":"licence/campaign-creative-001"`) {
		t.Fatalf("rights evidence missing from safe response: %s", valid.Body.String())
	}

	var created trustedAssetResponse
	if err := json.Unmarshal(valid.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+created.ID, nil)
	get.SetPathValue("id", created.ID)
	principal := identity.Principal{User: identity.User{
		ID:          "11111111-1111-4111-8111-111111111111",
		Permissions: map[string]struct{}{"campaign.read": {}},
	}}
	get = get.WithContext(identity.WithPrincipal(get.Context(), principal))
	getResponse := httptest.NewRecorder()
	server.getTrustedAsset(getResponse, get)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
	if strings.Contains(getResponse.Body.String(), "objectKey") || strings.Contains(getResponse.Body.String(), "trusted-assets/") {
		t.Fatalf("private object key leaked from GET: %s", getResponse.Body.String())
	}
}

func TestTrustedAssetRejectsRightsMetadataForNonMessageEvidence(t *testing.T) {
	server := trustedAssetTestServer(t)
	response := httptest.NewRecorder()
	server.uploadTrustedAsset(response, trustedAssetMultipartRequest(t, "CONSENT_EVIDENCE", map[string]string{
		"rightsBasis": "OWNED", "rightsReference": "not-applicable",
	}))
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "ASSET_RIGHTS_INVALID") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTrustedAssetReadPermissionIsBoundToAssetPurpose(t *testing.T) {
	server := trustedAssetTestServer(t)

	uploaded := httptest.NewRecorder()
	server.uploadTrustedAsset(uploaded, trustedAssetMultipartRequest(t, "MESSAGE_MEDIA", map[string]string{
		"rightsBasis": "OWNED", "rightsReference": "ownership/message-media-001",
	}))
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", uploaded.Code, uploaded.Body.String())
	}
	var created trustedAssetResponse
	if err := json.Unmarshal(uploaded.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+created.ID, nil)
	request.SetPathValue("id", created.ID)
	request = request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{User: identity.User{
		ID:          "22222222-2222-4222-8222-222222222222",
		Permissions: map[string]struct{}{"consent.read": {}},
	}}))
	response := httptest.NewRecorder()
	server.getTrustedAsset(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("consent-only operator read message-media metadata: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTrustedAssetConsentEvidenceRequiresConsentRead(t *testing.T) {
	server := trustedAssetTestServer(t)

	uploaded := httptest.NewRecorder()
	server.uploadTrustedAsset(uploaded, trustedAssetMultipartRequest(t, "CONSENT_EVIDENCE", nil))
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", uploaded.Code, uploaded.Body.String())
	}
	var created trustedAssetResponse
	if err := json.Unmarshal(uploaded.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+created.ID, nil)
	request.SetPathValue("id", created.ID)
	request = request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{User: identity.User{
		ID:          "33333333-3333-4333-8333-333333333333",
		Permissions: map[string]struct{}{"campaign.read": {}},
	}}))
	response := httptest.NewRecorder()
	server.getTrustedAsset(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("campaign-only operator read consent evidence metadata: status=%d body=%s", response.Code, response.Body.String())
	}

	allowed := httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+created.ID, nil)
	allowed.SetPathValue("id", created.ID)
	allowed = allowed.WithContext(identity.WithPrincipal(allowed.Context(), identity.Principal{User: identity.User{
		ID:          "44444444-4444-4444-8444-444444444444",
		Permissions: map[string]struct{}{"consent.read": {}},
	}}))
	allowedResponse := httptest.NewRecorder()
	server.getTrustedAsset(allowedResponse, allowed)
	if allowedResponse.Code != http.StatusOK {
		t.Fatalf("consent-read operator could not read consent evidence metadata: status=%d body=%s", allowedResponse.Code, allowedResponse.Body.String())
	}
}
