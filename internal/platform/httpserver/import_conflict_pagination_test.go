package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/audience/importer"
	"campaign-platform/internal/shared/httpx"
)

func TestAudienceImportConflictsReturnCursorContinuation(t *testing.T) {
	repo := importer.NewMemoryConflictRepository()
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	for _, item := range []importer.ProfileConflict{
		{ID: "conflict-a", AudienceImportID: "import-a", Status: importer.ConflictPending, CreatedAt: base},
		{ID: "conflict-b", AudienceImportID: "import-a", Status: importer.ConflictPending, CreatedAt: base.Add(time.Minute)},
		{ID: "conflict-c", AudienceImportID: "import-a", Status: importer.ConflictPending, CreatedAt: base.Add(2 * time.Minute)},
	} {
		repo.Add(item)
	}
	server := &Server{deps: Dependencies{AudienceConflicts: &importer.ConflictService{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/audience-imports/import-a/conflicts?status=PENDING&limit=2", nil)
	request.SetPathValue("id", "import-a")
	response := httptest.NewRecorder()
	server.listAudienceImportConflicts(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected conflict page: %+v body=%s", page, response.Body.String())
	}
}
