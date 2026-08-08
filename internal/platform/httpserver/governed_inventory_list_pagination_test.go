package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/audience/importer"
	"campaign-platform/internal/operations"
	"campaign-platform/internal/provider"
	"campaign-platform/internal/shared/httpx"
)

func TestProviderCapabilitiesReturnCursorContinuation(t *testing.T) {
	store := provider.NewMemoryStore()
	service := &provider.Service{Store: store}
	base := time.Date(2026, 8, 8, 7, 0, 0, 0, time.UTC)
	for index := 0; index < 3; index++ {
		at := base.Add(time.Duration(index) * time.Minute)
		service.Clock = func() time.Time { return at }
		if _, err := service.CreateDraft(context.Background(), provider.Definition{Provider: "OPENWA", Channel: provider.ChannelWhatsApp, Engine: "BAILEYS", AdapterVersion: "test", Capabilities: []provider.Capability{provider.CapabilitySendText}}, "actor", "pagination test"); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{ProviderCapabilities: service}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/provider-capabilities?limit=2", nil)
	response := httptest.NewRecorder()
	server.listProviderCapabilities(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected provider page: %+v body=%s", page, response.Body.String())
	}
}

func TestReportingPrivacyPoliciesReturnCursorContinuation(t *testing.T) {
	store := operations.NewMemoryReportingPrivacyStore()
	admin := &operations.ReportingPrivacyAdministration{Store: store}
	base := time.Date(2026, 8, 8, 7, 0, 0, 0, time.UTC)
	for index := 0; index < 3; index++ {
		at := base.Add(time.Duration(index) * time.Minute)
		admin.Clock = func() time.Time { return at }
		if _, err := admin.Create(context.Background(), operations.ReportingPrivacyPolicy{MinimumCohortSize: 10}, "actor", "pagination test", ""); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{Operations: &operations.Service{ReportingPrivacy: admin}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reporting-privacy-policies?limit=2", nil)
	response := httptest.NewRecorder()
	server.listReportingPrivacyPolicies(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected reporting-privacy page: %+v body=%s", page, response.Body.String())
	}
}

func TestAudienceImportMappingsReturnCursorContinuation(t *testing.T) {
	store := importer.NewMemoryMappingStore()
	admin := &importer.MappingAdministration{Store: store}
	base := time.Date(2026, 8, 8, 7, 0, 0, 0, time.UTC)
	for index, name := range []string{"Mapping A", "Mapping B", "Mapping C"} {
		at := base.Add(time.Duration(index) * time.Minute)
		admin.Clock = func() time.Time { return at }
		if _, err := admin.Create(context.Background(), importer.MappingDefinition{Name: name, SourceSystem: "CRM", TemplateVersion: "v1", Mapping: importer.ColumnMapping{MSISDN: "msisdn"}}, "actor", "pagination test", ""); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{AudienceImportMappings: admin}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audience-import-mappings?sourceSystem=CRM&limit=2", nil)
	response := httptest.NewRecorder()
	server.listAudienceImportMappings(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected mapping page: %+v body=%s", page, response.Body.String())
	}
}
