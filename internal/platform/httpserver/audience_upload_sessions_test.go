package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/audience/importer"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/storage"
)

func uploadPartChecksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func uploadSessionHTTPServer(t *testing.T) (http.Handler, string, *importer.UploadSessionService) {
	t.Helper()
	hash, err := identity.HashPassword("a-very-long-development-password")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{
		ID:           "55555555-5555-4555-8555-555555555555",
		Email:        "upload-operator@example.test",
		DisplayName:  "Upload Operator",
		Status:       identity.StatusActive,
		PasswordHash: hash,
		Permissions:  map[string]struct{}{"*": {}},
	}
	identityService := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := identityService.Login(context.Background(), user.Email, "a-very-long-development-password")
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 6, 9, 30, 0, 0, time.UTC)
	service := &importer.UploadSessionService{
		Repository:  importer.NewMemoryUploadSessionRepository(),
		Store:       store,
		PartSize:    5 << 20,
		MaxFileSize: 2 << 30,
		SessionTTL:  24 * time.Hour,
		Clock:       func() time.Time { return now },
	}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Identity:               identityService,
		AudienceUploadSessions: service,
	}).Handler()
	return handler, login.SessionToken, service
}

func createUploadSessionHTTP(t *testing.T, handler http.Handler, token, requestKey string, expectedBytes int64) *httptest.ResponseRecorder {
	t.Helper()
	payload := map[string]any{
		"organisationId":     "11111111-1111-4111-8111-111111111111",
		"consentReviewId":    "22222222-2222-4222-8222-222222222222",
		"purposeId":          "33333333-3333-4333-8333-333333333333",
		"channel":            "WHATSAPP",
		"wordingVersion":     "v1",
		"sourceName":         "Two million recipient source",
		"sourceSystem":       "client-secure-export",
		"defaultCountryIso2": "NG",
		"originalFilename":   "audience.csv",
		"templateVersion":    "v1",
		"mapping": map[string]any{
			"msisdn":  "msisdn",
			"country": "country",
			"state":   "state",
			"lga":     "lga",
			"age":     "age",
			"gender":  "gender",
		},
		"updatePolicy":  "NEWEST_SOURCE",
		"expectedBytes": expectedBytes,
	}
	raw, _ := json.Marshal(payload)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/audience-import-upload-sessions", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Idempotency-Key", requestKey)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestAudienceUploadSessionHTTPResumesPartsWithoutLeakingStorageKeys(t *testing.T) {
	handler, token, _ := uploadSessionHTTPServer(t)
	first := createUploadSessionHTTP(t, handler, token, "http-large-upload-request-0001", (5<<20)+1)
	if first.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", first.Code, first.Body.String())
	}
	if strings.Contains(first.Body.String(), "imports/uploads") || strings.Contains(first.Body.String(), "http-large-upload-request-0001") {
		t.Fatalf("response leaked storage/request identity: %s", first.Body.String())
	}
	var created struct {
		Transport string                 `json:"transport"`
		Session   importer.UploadSession `json:"session"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Transport != "RELAY" || created.Session.PartCount != 2 || created.Session.ID == "" {
		t.Fatalf("unexpected create response: %+v", created)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/v1/audience-import-upload-sessions/"+created.Session.ID, nil)
	get.Header.Set("Authorization", "Bearer "+token)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), "\"uploadedParts\":0") {
		t.Fatalf("initial get: %d %s", getResponse.Code, getResponse.Body.String())
	}

	part1 := bytes.Repeat([]byte("a"), 5<<20)
	put := httptest.NewRequest(http.MethodPut, "/api/v1/audience-import-upload-sessions/"+created.Session.ID+"/parts/1", bytes.NewReader(part1))
	put.Header.Set("Authorization", "Bearer "+token)
	put.Header.Set("Content-Type", "application/octet-stream")
	put.Header.Set("X-Content-SHA256", uploadPartChecksum(part1))
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, put)
	if putResponse.Code != http.StatusOK || !strings.Contains(putResponse.Body.String(), "\"changed\":true") {
		t.Fatalf("part 1: %d %s", putResponse.Code, putResponse.Body.String())
	}

	replay := httptest.NewRequest(http.MethodPut, "/api/v1/audience-import-upload-sessions/"+created.Session.ID+"/parts/1", bytes.NewReader(part1))
	replay.Header.Set("Authorization", "Bearer "+token)
	replay.Header.Set("Content-Type", "application/octet-stream")
	replay.Header.Set("X-Content-SHA256", uploadPartChecksum(part1))
	replayResponse := httptest.NewRecorder()
	handler.ServeHTTP(replayResponse, replay)
	if replayResponse.Code != http.StatusOK || !strings.Contains(replayResponse.Body.String(), "\"changed\":false") {
		t.Fatalf("exact replay: %d %s", replayResponse.Code, replayResponse.Body.String())
	}

	part2 := []byte("z")
	put2 := httptest.NewRequest(http.MethodPut, "/api/v1/audience-import-upload-sessions/"+created.Session.ID+"/parts/2", bytes.NewReader(part2))
	put2.Header.Set("Authorization", "Bearer "+token)
	put2.Header.Set("Content-Type", "application/octet-stream")
	put2.Header.Set("X-Content-SHA256", uploadPartChecksum(part2))
	put2Response := httptest.NewRecorder()
	handler.ServeHTTP(put2Response, put2)
	if put2Response.Code != http.StatusOK {
		t.Fatalf("part 2: %d %s", put2Response.Code, put2Response.Body.String())
	}

	get = httptest.NewRequest(http.MethodGet, "/api/v1/audience-import-upload-sessions/"+created.Session.ID, nil)
	get.Header.Set("Authorization", "Bearer "+token)
	getResponse = httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	var current struct {
		Session importer.UploadSession `json:"session"`
	}
	if err := json.Unmarshal(getResponse.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current.Session.UploadedParts != 2 || current.Session.UploadedBytes != (5<<20)+1 {
		t.Fatalf("progress mismatch: %+v", current.Session)
	}

	rawComplete, _ := json.Marshal(map[string]any{"expectedVersion": current.Session.Version})
	complete := httptest.NewRequest(http.MethodPost, "/api/v1/audience-import-upload-sessions/"+created.Session.ID+"/complete", bytes.NewReader(rawComplete))
	complete.Header.Set("Authorization", "Bearer "+token)
	complete.Header.Set("Content-Type", "application/json")
	completeResponse := httptest.NewRecorder()
	handler.ServeHTTP(completeResponse, complete)
	if completeResponse.Code != http.StatusOK || !strings.Contains(completeResponse.Body.String(), "\"state\":\"UPLOADED\"") {
		t.Fatalf("complete: %d %s", completeResponse.Code, completeResponse.Body.String())
	}
}

func TestAudienceUploadSessionHTTPRejectsOversizedPartAndSupportsAbort(t *testing.T) {
	handler, token, service := uploadSessionHTTPServer(t)
	first := createUploadSessionHTTP(t, handler, token, "http-large-upload-request-0002", 9)
	if first.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", first.Code, first.Body.String())
	}
	var created struct {
		Session importer.UploadSession `json:"session"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	oversized := []byte("1234567890")
	put := httptest.NewRequest(http.MethodPut, "/api/v1/audience-import-upload-sessions/"+created.Session.ID+"/parts/1", bytes.NewReader(oversized))
	put.Header.Set("Authorization", "Bearer "+token)
	put.Header.Set("Content-Type", "application/octet-stream")
	put.Header.Set("X-Content-SHA256", uploadPartChecksum(oversized))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, put)
	if response.Code != http.StatusRequestEntityTooLarge && response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("oversized part: %d %s", response.Code, response.Body.String())
	}
	fresh, err := service.Get(context.Background(), created.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.UploadedParts != 0 {
		t.Fatalf("oversized part recorded progress: %+v", fresh)
	}

	abortRaw, _ := json.Marshal(map[string]any{"expectedVersion": fresh.Version, "reason": "operator selected the wrong source file"})
	abort := httptest.NewRequest(http.MethodPost, "/api/v1/audience-import-upload-sessions/"+created.Session.ID+"/abort", bytes.NewReader(abortRaw))
	abort.Header.Set("Authorization", "Bearer "+token)
	abort.Header.Set("Content-Type", "application/json")
	abortResponse := httptest.NewRecorder()
	handler.ServeHTTP(abortResponse, abort)
	if abortResponse.Code != http.StatusOK || !strings.Contains(abortResponse.Body.String(), "\"state\":\"ABORTED\"") {
		t.Fatalf("abort: %d %s", abortResponse.Code, abortResponse.Body.String())
	}
}

type httpDirectUploadStore struct {
	storage.ObjectStore
}

func (s *httpDirectUploadStore) DirectUploadEnabled() bool { return true }

func (s *httpDirectUploadStore) CreateDirectUploadTarget(_ context.Context, _ string, expectedBytes int64, checksum string, ttl time.Duration) (storage.DirectUploadTarget, error) {
	return storage.DirectUploadTarget{
		Method: "PUT",
		URL:    "https://upload.example.test/one-part-only",
		Headers: map[string]string{
			"X-Amz-Meta-Sha256": checksum,
		},
		ExpectedBytes: expectedBytes,
		ExpiresAt:     time.Now().Add(ttl),
	}, nil
}

func TestAudienceUploadSessionHTTPDirectTargetAndConfirm(t *testing.T) {
	handler, token, service := uploadSessionHTTPServer(t)
	service.Store = &httpDirectUploadStore{ObjectStore: service.Store}

	first := createUploadSessionHTTP(t, handler, token, "http-direct-upload-request-0001", 9)
	if first.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", first.Code, first.Body.String())
	}
	var created struct {
		Transport          string                 `json:"transport"`
		Transports         []string               `json:"transports"`
		PreferredTransport string                 `json:"preferredTransport"`
		Session            importer.UploadSession `json:"session"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Transport != "DIRECT_S3" || created.PreferredTransport != "DIRECT_S3" ||
		len(created.Transports) != 2 || created.Transports[0] != "DIRECT_S3" || created.Transports[1] != "RELAY" {
		t.Fatalf("transport advertisement=%+v", created)
	}

	payload := []byte("123456789")
	checksum := uploadPartChecksum(payload)
	targetRaw, _ := json.Marshal(map[string]string{"sha256": checksum})
	targetRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/audience-import-upload-sessions/"+created.Session.ID+"/parts/1/target",
		bytes.NewReader(targetRaw),
	)
	targetRequest.Header.Set("Authorization", "Bearer "+token)
	targetRequest.Header.Set("Content-Type", "application/json")
	targetResponse := httptest.NewRecorder()
	handler.ServeHTTP(targetResponse, targetRequest)
	if targetResponse.Code != http.StatusOK {
		t.Fatalf("target: %d %s", targetResponse.Code, targetResponse.Body.String())
	}
	if strings.Contains(targetResponse.Body.String(), "super-secret") || !strings.Contains(targetResponse.Body.String(), "upload.example.test") {
		t.Fatalf("unsafe or missing direct target: %s", targetResponse.Body.String())
	}

	current, err := service.Get(context.Background(), created.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Store.Put(context.Background(), current.Parts[0].ObjectKey, bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatal(err)
	}

	confirmRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/audience-import-upload-sessions/"+created.Session.ID+"/parts/1/confirm",
		bytes.NewReader(targetRaw),
	)
	confirmRequest.Header.Set("Authorization", "Bearer "+token)
	confirmRequest.Header.Set("Content-Type", "application/json")
	confirmResponse := httptest.NewRecorder()
	handler.ServeHTTP(confirmResponse, confirmRequest)
	if confirmResponse.Code != http.StatusOK || !strings.Contains(confirmResponse.Body.String(), "\"changed\":true") {
		t.Fatalf("confirm: %d %s", confirmResponse.Code, confirmResponse.Body.String())
	}

	replayRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/audience-import-upload-sessions/"+created.Session.ID+"/parts/1/confirm",
		bytes.NewReader(targetRaw),
	)
	replayRequest.Header.Set("Authorization", "Bearer "+token)
	replayRequest.Header.Set("Content-Type", "application/json")
	replayResponse := httptest.NewRecorder()
	handler.ServeHTTP(replayResponse, replayRequest)
	if replayResponse.Code != http.StatusOK || !strings.Contains(replayResponse.Body.String(), "\"changed\":false") {
		t.Fatalf("confirm replay: %d %s", replayResponse.Code, replayResponse.Body.String())
	}
}
