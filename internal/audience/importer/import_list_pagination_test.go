package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func importListTestBatch(t *testing.T, organisationID string, index int, createdAt time.Time) ImportBatch {
	t.Helper()
	batch, err := NewImportBatch(CreateImportInput{
		OrganisationID:        organisationID,
		ConsentReviewID:       fmt.Sprintf("review-%d", index),
		PurposeID:             fmt.Sprintf("purpose-%d", index),
		Channel:               "WHATSAPP",
		WordingVersion:        "v1",
		SourceName:            fmt.Sprintf("Source %d", index),
		SourceSystem:          "CRM",
		ObjectKey:             fmt.Sprintf("quarantine/private/object-%d.csv", index),
		OriginalFilename:      fmt.Sprintf("audience-%d.csv", index),
		DetectedMediaType:     "text/csv",
		FileSHA256:            fmt.Sprintf("%064x", index+1),
		ByteSize:              int64(100 + index),
		TemplateVersion:       "v1",
		Mapping:               ColumnMapping{MSISDN: "msisdn"},
		UpdatePolicy:          UpdateNewestSource,
		UploadedBy:            "maker",
		ClientRequestID:       fmt.Sprintf("import-list-request-%016d", index),
		ContentSignatureValid: true,
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func TestImportServiceListPageIsNewestFirstScopedAndCursorSafe(t *testing.T) {
	base := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	repository := NewMemoryImportRepository()
	for index, item := range []struct {
		org string
		at  time.Time
	}{
		{"org-1", base.Add(time.Minute)},
		{"org-1", base.Add(2 * time.Minute)},
		{"org-2", base.Add(3 * time.Minute)},
		{"org-1", base.Add(4 * time.Minute)},
	} {
		batch := importListTestBatch(t, item.org, index+1, item.at)
		if _, created, err := repository.Create(context.Background(), batch); err != nil || !created {
			t.Fatalf("create %d: created=%v err=%v", index, created, err)
		}
	}
	service := &ImportService{Repository: repository}
	first, err := service.ListPage(context.Background(), "org-1", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first page=%+v", first)
	}
	if first.Items[0].SourceName != "Source 4" || first.Items[1].SourceName != "Source 2" {
		t.Fatalf("unexpected newest-first order: %+v", first.Items)
	}
	second, err := service.ListPage(context.Background(), "org-1", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].SourceName != "Source 1" || second.NextCursor != "" {
		t.Fatalf("second page=%+v", second)
	}
}

func TestImportServiceListPageRejectsMalformedCursor(t *testing.T) {
	service := &ImportService{Repository: NewMemoryImportRepository()}
	if _, err := service.ListPage(context.Background(), "", 10, "%%%"); err != ErrInvalidImportListCursor {
		t.Fatalf("expected invalid cursor, got %v", err)
	}
}

func TestImportListSummaryDoesNotExposeStorageOrMappingInternals(t *testing.T) {
	repository := NewMemoryImportRepository()
	batch := importListTestBatch(t, "org-1", 7, time.Now().UTC())
	if _, _, err := repository.Create(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	page, err := (&ImportService{Repository: repository}).ListPage(context.Background(), "org-1", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"objectKey", "mapping", "fileSha256", "clientRequestId"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("summary leaked %q: %s", forbidden, text)
		}
	}
}
