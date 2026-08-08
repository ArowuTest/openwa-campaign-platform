package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"campaign-platform/internal/organisation"
	"campaign-platform/internal/shared/httpx"
)

type organisationPaginationRepository struct {
	events []organisation.Event
}

func (*organisationPaginationRepository) Create(context.Context, organisation.Organisation) error {
	return nil
}
func (*organisationPaginationRepository) List(context.Context) ([]organisation.Organisation, error) {
	return nil, nil
}
func (*organisationPaginationRepository) Get(context.Context, string) (organisation.Organisation, error) {
	return organisation.Organisation{}, nil
}
func (*organisationPaginationRepository) Update(context.Context, organisation.Organisation, int64, organisation.Event) (organisation.Organisation, error) {
	return organisation.Organisation{}, nil
}
func (*organisationPaginationRepository) ListEvents(context.Context, string) ([]organisation.Event, error) {
	return nil, nil
}
func (r *organisationPaginationRepository) ListEventPage(_ context.Context, _ string, limit int, _ int64) ([]organisation.Event, error) {
	if len(r.events) > limit {
		return append([]organisation.Event(nil), r.events[:limit]...), nil
	}
	return append([]organisation.Event(nil), r.events...), nil
}

func TestOrganisationEventsReturnCursorContinuation(t *testing.T) {
	repo := &organisationPaginationRepository{events: []organisation.Event{
		{ID: "event-2", OrganisationID: "org-a", Version: 2},
		{ID: "event-3", OrganisationID: "org-a", Version: 3},
		{ID: "event-4", OrganisationID: "org-a", Version: 4},
	}}
	server := &Server{deps: Dependencies{Organisations: organisation.NewService(repo)}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/organisations/org-a/events?limit=2", nil)
	request.SetPathValue("id", "org-a")
	response := httptest.NewRecorder()
	server.listOrganisationEvents(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected organisation-event page: %+v body=%s", page, response.Body.String())
	}
}

func TestOrganisationsReturnCursorContinuation(t *testing.T) {
	repo := organisation.NewMemoryRepository()
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	for _, item := range []organisation.Organisation{
		{ID: "org-a", LegalName: "Org A", CountryISO2: "NG", CreatedAt: base, UpdatedAt: base, Version: 1},
		{ID: "org-b", LegalName: "Org B", CountryISO2: "NG", CreatedAt: base.Add(time.Minute), UpdatedAt: base.Add(time.Minute), Version: 1},
		{ID: "org-c", LegalName: "Org C", CountryISO2: "NG", CreatedAt: base.Add(2 * time.Minute), UpdatedAt: base.Add(2 * time.Minute), Version: 1},
	} {
		if err := repo.Create(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{Organisations: organisation.NewService(repo)}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/organisations?limit=2", nil)
	response := httptest.NewRecorder()
	server.listOrganisations(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var first struct {
		Items      []organisation.Organisation `json:"items"`
		Count      int                         `json:"count"`
		NextCursor string                      `json:"nextCursor"`
		HasMore    bool                        `json:"hasMore"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Count != 2 || first.NextCursor == "" || !first.HasMore || first.Items[0].ID != "org-c" || first.Items[1].ID != "org-b" {
		t.Fatalf("unexpected first organisation page: %+v body=%s", first, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/organisations?limit=2&cursor="+url.QueryEscape(first.NextCursor), nil)
	response = httptest.NewRecorder()
	server.listOrganisations(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", response.Code, response.Body.String())
	}
	var second struct {
		Items      []organisation.Organisation `json:"items"`
		NextCursor string                      `json:"nextCursor"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "org-a" || second.NextCursor != "" {
		t.Fatalf("unexpected second organisation page: %+v body=%s", second, response.Body.String())
	}
}
