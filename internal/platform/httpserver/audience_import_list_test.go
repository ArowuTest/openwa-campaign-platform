package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/audience/importer"
)

func createImportInventoryFixture(t *testing.T, repository *importer.MemoryImportRepository, organisationID string, index int, at time.Time) importer.ImportBatch {
	t.Helper()
	batch, err := importer.NewImportBatch(importer.CreateImportInput{
		OrganisationID:        organisationID,
		ConsentReviewID:       fmt.Sprintf("review-%d", index),
		PurposeID:             fmt.Sprintf("purpose-%d", index),
		Channel:               "WHATSAPP",
		WordingVersion:        "v1",
		SourceName:            fmt.Sprintf("Source %d", index),
		SourceSystem:          "CRM",
		ObjectKey:             fmt.Sprintf("private/quarantine/%d.csv", index),
		OriginalFilename:      fmt.Sprintf("audience-%d.csv", index),
		DetectedMediaType:     "text/csv",
		FileSHA256:            fmt.Sprintf("%064x", index+100),
		ByteSize:              int64(1000 + index),
		TemplateVersion:       "v1",
		Mapping:               importer.ColumnMapping{MSISDN: "msisdn"},
		UpdatePolicy:          importer.UpdateNewestSource,
		UploadedBy:            "maker",
		ClientRequestID:       fmt.Sprintf("inventory-http-request-%016d", index),
		ContentSignatureValid: true,
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := repository.Create(context.Background(), batch); err != nil || !created {
		t.Fatalf("create fixture: created=%v err=%v", created, err)
	}
	return batch
}

func TestListAudienceImportsRequiresOrganisationAndReturnsSafePage(t *testing.T) {
	base := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	repository := importer.NewMemoryImportRepository()
	createImportInventoryFixture(t, repository, "11111111-1111-4111-8111-111111111111", 1, base.Add(time.Minute))
	createImportInventoryFixture(t, repository, "11111111-1111-4111-8111-111111111111", 2, base.Add(2*time.Minute))
	createImportInventoryFixture(t, repository, "org-2", 3, base.Add(3*time.Minute))
	server := &Server{deps: Dependencies{AudienceImports: &importer.ImportService{Repository: repository}}}

	missing := httptest.NewRecorder()
	server.listAudienceImports(missing, httptest.NewRequest("GET", "/api/v1/audience-imports", nil))
	if missing.Code != 400 {
		t.Fatalf("missing organisation status=%d body=%s", missing.Code, missing.Body.String())
	}

	invalid := httptest.NewRecorder()
	server.listAudienceImports(invalid, httptest.NewRequest("GET", "/api/v1/audience-imports?organisationId=not-a-uuid", nil))
	if invalid.Code != 400 || !strings.Contains(invalid.Body.String(), "INVALID_ORGANISATION_ID") {
		t.Fatalf("invalid organisation status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	response := httptest.NewRecorder()
	server.listAudienceImports(response, httptest.NewRequest("GET", "/api/v1/audience-imports?organisationId=11111111-1111-4111-8111-111111111111&limit=1", nil))
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Items      []importer.ImportListItem `json:"items"`
		Count      int                       `json:"count"`
		HasMore    bool                      `json:"hasMore"`
		NextCursor string                    `json:"nextCursor"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Count != 1 || len(body.Items) != 1 || body.Items[0].SourceName != "Source 2" || !body.HasMore || body.NextCursor == "" {
		t.Fatalf("body=%+v", body)
	}
	raw := response.Body.String()
	for _, forbidden := range []string{"objectKey", "mapping", "fileSha256", "clientRequestId"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, raw)
		}
	}
}

func TestListAudienceImportsRejectsMalformedCursor(t *testing.T) {
	server := &Server{deps: Dependencies{AudienceImports: &importer.ImportService{Repository: importer.NewMemoryImportRepository()}}}
	response := httptest.NewRecorder()
	server.listAudienceImports(response, httptest.NewRequest("GET", "/api/v1/audience-imports?organisationId=11111111-1111-4111-8111-111111111111&cursor=e30", nil))
	if response.Code != 400 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "INVALID_PAGE_CURSOR") {
		t.Fatalf("unexpected body=%s", response.Body.String())
	}
}
