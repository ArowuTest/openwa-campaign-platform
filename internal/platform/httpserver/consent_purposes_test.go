package httpserver

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/consent"
)

func TestListConsentPurposesFiltersByOrganisationReviewAndActiveState(t *testing.T) {
	now := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	repo := consent.NewMemoryPurposeRepository(
		consent.Purpose{ID: "00000000-0000-4000-8000-000000000001", OrganisationID: "org-1", ConsentReviewID: "review-1", Code: "MARKETING", Name: "Marketing", Channel: "WHATSAPP", WordingVersion: "v1", Active: true, CreatedAt: now, UpdatedAt: now},
		consent.Purpose{ID: "00000000-0000-4000-8000-000000000002", OrganisationID: "org-1", ConsentReviewID: "review-1", Code: "OLD", Name: "Old", Channel: "WHATSAPP", WordingVersion: "v1", Active: false, CreatedAt: now.Add(-time.Minute), UpdatedAt: now},
		consent.Purpose{ID: "00000000-0000-4000-8000-000000000003", OrganisationID: "org-2", ConsentReviewID: "review-2", Code: "OTHER", Name: "Other", Channel: "WHATSAPP", WordingVersion: "v1", Active: true, CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now},
	)
	server := &Server{deps: Dependencies{ConsentPurposes: &consent.PurposeService{Repository: repo}}}
	req := httptest.NewRequest("GET", "/api/v1/consent-purposes?organisationId=org-1&consentReviewId=review-1&limit=100", nil)
	rr := httptest.NewRecorder()
	server.listConsentPurposes(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Items []consent.Purpose `json:"items"`
		Count int               `json:"count"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Items[0].Code != "MARKETING" || body.Count != 1 {
		t.Fatalf("body=%+v", body)
	}
}

func TestListConsentPurposesRejectsInvalidIncludeInactive(t *testing.T) {
	server := &Server{deps: Dependencies{ConsentPurposes: &consent.PurposeService{Repository: consent.NewMemoryPurposeRepository()}}}
	req := httptest.NewRequest("GET", "/api/v1/consent-purposes?includeInactive=maybe", nil)
	rr := httptest.NewRecorder()
	server.listConsentPurposes(rr, req)
	if rr.Code != 400 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
