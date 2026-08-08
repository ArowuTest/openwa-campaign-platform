package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/commercial"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/inbound"
	"campaign-platform/internal/organisation"
	"campaign-platform/internal/shared/httpx"
)

func requireContinuation(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("expected cursor continuation: %+v body=%s", page, response.Body.String())
	}
}

func TestOrganisationPoliciesReturnCursorContinuation(t *testing.T) {
	store := organisation.NewMemoryPolicyStore()
	admin := &organisation.PolicyAdministration{Store: store}
	base := time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		admin.Clock = func() time.Time { return base.Add(time.Duration(i) * time.Minute) }
		if _, err := admin.CreateDraft(context.Background(), organisation.Policy{OrganisationID: "org-a", ContactRetentionDays: 30, CampaignRetentionDays: 90}, "actor", "pagination policy"); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{OrganisationPolicies: admin}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/organisations/org-a/policies?limit=2", nil)
	req.SetPathValue("id", "org-a")
	rr := httptest.NewRecorder()
	server.listOrganisationPolicies(rr, req)
	requireContinuation(t, rr)
}

func TestCommercialRecordsReturnCursorContinuation(t *testing.T) {
	store := commercial.NewMemoryStore()
	svc := &commercial.Service{Store: store}
	base := time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		svc.Clock = func() time.Time { return base.Add(time.Duration(i) * time.Minute) }
		r := commercial.Record{CampaignID: "campaign-" + string(rune('a'+i)), OrganisationID: "org-a", QuotationReference: "q", InvoiceReference: "i", Currency: "GBP", ApprovedRecipients: 1, UnitPriceMinor: 100, TotalAmountMinor: 100}
		if _, err := svc.CreateDraft(context.Background(), r, "actor", "pagination commercial"); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{Commercial: svc}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/commercial-records?organisationId=org-a&limit=2", nil)
	rr := httptest.NewRecorder()
	server.listCommercialRecords(rr, req)
	requireContinuation(t, rr)
}

func TestConsentReviewsReturnCursorContinuation(t *testing.T) {
	repo := consent.NewMemoryRepository()
	svc := consent.NewService(repo)
	base := time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		v, err := consent.NewReview(consent.CreateInput{OrganisationID: "org-a", Name: "Review", Channel: "WHATSAPP", CreatedBy: "actor"}, base.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.Create(context.Background(), v); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{ConsentReviews: svc}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/consent-reviews?organisationId=org-a&limit=2", nil)
	rr := httptest.NewRecorder()
	server.listConsentReviews(rr, req)
	requireContinuation(t, rr)
}

func TestInboundRetentionPoliciesReturnCursorContinuation(t *testing.T) {
	store := inbound.NewMemoryRetentionPolicyStore(inbound.RetentionPolicy{})
	admin := &inbound.RetentionPolicyAdministration{Store: store}
	base := time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		admin.Clock = func() time.Time { return base.Add(time.Duration(i) * time.Minute) }
		if _, err := admin.CreateDraft(context.Background(), 30, time.Time{}, "actor", "pagination retention"); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{InboundRetentionPolicies: admin}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/inbound-retention-policies?limit=2", nil)
	rr := httptest.NewRecorder()
	server.listInboundRetentionPolicies(rr, req)
	requireContinuation(t, rr)
}

func TestOptOutPoliciesReturnCursorContinuation(t *testing.T) {
	store := consent.NewMemoryOptOutPolicyStore(consent.GovernedOptOutPolicy{})
	admin := &consent.OptOutPolicyAdministration{Store: store}
	base := time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		admin.Clock = func() time.Time { return base.Add(time.Duration(i) * time.Minute) }
		if _, err := admin.CreateDraft(context.Background(), []string{"STOP"}, time.Time{}, "actor", "pagination optout"); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{OptOutPolicies: admin}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/opt-out-policies?limit=2", nil)
	rr := httptest.NewRecorder()
	server.listOptOutPolicies(rr, req)
	requireContinuation(t, rr)
}
