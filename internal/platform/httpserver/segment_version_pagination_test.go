package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"campaign-platform/internal/segment"
	"campaign-platform/internal/shared/httpx"
)

type segmentVersionPaginationRepository struct {
	items []segment.DefinitionVersion
}

func (*segmentVersionPaginationRepository) Create(context.Context, segment.Definition, string) (segment.Definition, error) {
	return segment.Definition{}, nil
}
func (*segmentVersionPaginationRepository) Get(context.Context, string) (segment.Definition, error) {
	return segment.Definition{}, nil
}
func (*segmentVersionPaginationRepository) List(context.Context, string, int) ([]segment.Definition, error) {
	return nil, nil
}
func (*segmentVersionPaginationRepository) Update(context.Context, segment.Definition, int64, string) (segment.Definition, error) {
	return segment.Definition{}, nil
}
func (*segmentVersionPaginationRepository) Versions(context.Context, string, int) ([]segment.DefinitionVersion, error) {
	return nil, nil
}
func (r *segmentVersionPaginationRepository) VersionsPage(_ context.Context, _ string, limit int, _ int64) ([]segment.DefinitionVersion, error) {
	if len(r.items) > limit {
		return append([]segment.DefinitionVersion(nil), r.items[:limit]...), nil
	}
	return append([]segment.DefinitionVersion(nil), r.items...), nil
}

func TestSegmentVersionsReturnCursorContinuation(t *testing.T) {
	repo := &segmentVersionPaginationRepository{items: []segment.DefinitionVersion{
		{SegmentID: "segment-a", Version: 3},
		{SegmentID: "segment-a", Version: 2},
		{SegmentID: "segment-a", Version: 1},
	}}
	server := &Server{deps: Dependencies{SegmentDefinitions: &segment.DefinitionService{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/segments/segment-a/versions?limit=2", nil)
	request.SetPathValue("id", "segment-a")
	response := httptest.NewRecorder()
	server.listSegmentVersions(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected segment-version page: %+v body=%s", page, response.Body.String())
	}
}
