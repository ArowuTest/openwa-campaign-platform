package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/audience/importer"
	"campaign-platform/internal/identity"
)

func TestAudienceImportPreviewCanBeCancelledThroughAPI(t *testing.T) {
	hash, err := identity.HashPassword("Correct-Horse-Import-Cancel-17")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{ID: "import-operator", Email: "import-operator@example.test", DisplayName: "Import Operator", Status: identity.StatusActive, PasswordHash: hash, Permissions: map[string]struct{}{"audience.write": {}}}
	ids := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := ids.Login(context.Background(), user.Email, "Correct-Horse-Import-Cancel-17")
	if err != nil {
		t.Fatal(err)
	}
	repo := importer.NewMemoryImportRepository()
	batch := importer.ImportBatch{ID: "import-1", Status: importer.ImportPreviewReady, Version: 4, UploadedBy: "maker", UpdatedAt: time.Now().UTC()}
	if _, _, err := repo.Create(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	imports := &importer.ImportService{Repository: repo}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{Identity: ids, AudienceImports: imports}).Handler()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/audience-imports/import-1/cancel", strings.NewReader(`{"expectedVersion":4,"reason":"preview contains the wrong source"}`))
	request.Header.Set("Authorization", "Bearer "+login.SessionToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	stored, err := imports.Get(context.Background(), batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != importer.ImportCancelled || stored.Version != 5 {
		t.Fatalf("unexpected cancelled import: %+v", stored)
	}
}
