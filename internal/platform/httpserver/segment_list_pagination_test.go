package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/segment"
	"campaign-platform/internal/shared/httpx"
)

func TestSegmentsReturnCursorContinuation(t *testing.T) {
	repo := segment.NewMemoryDefinitionRepository()
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	for index, item := range []segment.Definition{
		{ID: "segment-a", OrganisationID: "org-a", Name: "Segment A", UpdatedAt: base},
		{ID: "segment-b", OrganisationID: "org-a", Name: "Segment B", UpdatedAt: base.Add(time.Minute)},
		{ID: "segment-c", OrganisationID: "org-a", Name: "Segment C", UpdatedAt: base.Add(2 * time.Minute)},
	} {
		if _, err := repo.Create(httptest.NewRequest(http.MethodGet, "/", nil).Context(), item, "seed"); err != nil {
			t.Fatalf("seed segment %d: %v", index, err)
		}
	}
	server := &Server{deps: Dependencies{SegmentDefinitions: &segment.DefinitionService{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/segments?organisationId=org-a&limit=2", nil)
	response := httptest.NewRecorder()
	server.listSegments(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected segment page: %+v body=%s", page, response.Body.String())
	}
}
