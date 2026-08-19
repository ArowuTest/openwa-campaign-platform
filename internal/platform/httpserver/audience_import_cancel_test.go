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

func TestAudienceImportCancelRouteCancelsPreviewBeforeMerge(t *testing.T) {
	now := time.Date(2026, 8, 8, 19, 30, 0, 0, time.UTC)
	repo := importer.NewMemoryImportRepository()
	imports := &importer.ImportService{Repository: repo, Clock: func() time.Time { return now }}
	batch, _, err := imports.Create(context.Background(), importer.CreateImportInput{
		OrganisationID: "org-1", ConsentReviewID: "review-1", PurposeID: "purpose-1", Channel: "WHATSAPP", WordingVersion: "v1",
		SourceName: "cancel-preview", ObjectKey: "imports/cancel.csv", OriginalFilename: "cancel.csv", DetectedMediaType: "text/csv",
		FileSHA256: strings.Repeat("c", 64), ByteSize: 100, TemplateVersion: "v1", Mapping: importer.ColumnMapping{MSISDN: "msisdn"},
		UploadedBy: "maker-1", ClientRequestID: "cancel-preview-request-0001", ContentSignatureValid: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	batch, err = imports.RecordScan(context.Background(), batch.ID, importer.MalwareClean, true, "", batch.Version)
	if err != nil {
		t.Fatal(err)
	}
	batch, err = repo.SetPreviewReady(batch.ID, importer.PreviewResult{UploadedRows: 1, ValidRows: 1}, batch.Version, now)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := identity.HashPassword("Correct-Horse-Import-Cancel-19")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{ID: "import-canceller", Email: "import-canceller@example.test", DisplayName: "Import Canceller", Status: identity.StatusActive, PasswordHash: hash, Permissions: map[string]struct{}{"audience.write": {}}}
	ids := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := ids.Login(context.Background(), user.Email, "Correct-Horse-Import-Cancel-19")
	if err != nil {
		t.Fatal(err)
	}
	h := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{Identity: ids, AudienceImports: imports}).Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/audience-imports/"+batch.ID+"/cancel", strings.NewReader(`{"expectedVersion":3,"reason":"preview shows the wrong audience source"}`))
	req.Header.Set("Authorization", "Bearer "+login.SessionToken)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	got, err := imports.Get(context.Background(), batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != importer.ImportCancelled || got.FailureReason != "preview shows the wrong audience source" {
		t.Fatalf("unexpected persisted import: %+v", got)
	}
}
