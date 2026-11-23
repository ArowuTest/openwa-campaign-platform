package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"campaign-platform/internal/consent"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/organisation"
)

type consentCreateFailingRepository struct{ *consent.MemoryRepository }

func (r consentCreateFailingRepository) Create(context.Context, consent.Review) error {
	return errors.New("pq: server unavailable; internal credentials must never reach client")
}
func consentCreateRequest(t *testing.T, service *consent.Service, payload map[string]any) (int, map[string]any) {
	t.Helper()
	content, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/consent-reviews", bytes.NewReader(content))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(identity.WithPrincipal(req.Context(), identity.Principal{User: identity.User{ID: "maker"}}))
	recorder := httptest.NewRecorder()
	s := &Server{deps: Dependencies{ConsentReviews: service}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	s.createConsentReview(recorder, req)
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("HTTP %d not JSON: %s", recorder.Code, recorder.Body.String())
	}
	return recorder.Code, body
}

func TestConsentCreateErrorContract(t *testing.T) {
	base := map[string]any{"organisationId": "00000000-0000-4000-8000-000000000001", "name": "Synthetic Review", "channel": "WHATSAPP", "scope": "ORGANISATION"}
	tests := []struct {
		name, field, reason string
		mutate              func(map[string]any)
	}{
		{"sample contact PII", "sampleReviewNotes", "sample review notes must not contain email addresses or telephone numbers", func(p map[string]any) { p["sampleReviewNotes"] = "Called 12345678901" }},
		{"external checksum", "externalEvidenceReferences", "SHA-256", func(p map[string]any) {
			p["externalEvidenceReferences"] = []map[string]string{{"reference": "consent-proof.pdf", "sha256Checksum": "not-a-checksum"}}
		}},
		{"invalid scope", "scope", "invalid consent-review scope", func(p map[string]any) { p["scope"] = "GLOBAL" }},
		{"malformed organisation reference", "organisationId", "valid UUID", func(p map[string]any) { p["organisationId"] = "not-a-uuid" }},
		{"malformed trusted evidence reference", "evidenceAssetIds", "valid UUID", func(p map[string]any) { p["evidenceAssetIds"] = []string{"not-a-uuid"} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{}
			for k, v := range base {
				payload[k] = v
			}
			tc.mutate(payload)
			code, body := consentCreateRequest(t, consent.NewService(consent.NewMemoryRepository()), payload)
			if code != 422 {
				t.Fatalf("expected 422, got %d: %#v", code, body)
			}
			details, ok := body["details"].(map[string]any)
			if !ok {
				t.Fatalf("missing safe details: %#v", body)
			}
			fields, ok := details["fields"].(map[string]any)
			if !ok {
				t.Fatalf("missing field-error contract: %#v", body)
			}
			got, _ := fields[tc.field].(string)
			if !strings.Contains(got, tc.reason) {
				t.Fatalf("expected safe field %s containing %q, got %#v", tc.field, tc.reason, body)
			}
		})
	}
	t.Run("unavailable repository returns redacted 500", func(t *testing.T) {
		service := consent.NewService(consentCreateFailingRepository{MemoryRepository: consent.NewMemoryRepository()})
		code, body := consentCreateRequest(t, service, base)
		if code != 500 || body["error"] != "INTERNAL_ERROR" {
			t.Fatalf("expected redacted 500, got %d: %#v", code, body)
		}
		if strings.Contains(strings.ToLower(strings.TrimSpace(asJSON(body))), "pq:") || strings.Contains(asJSON(body), "credentials") {
			t.Fatalf("database details leaked: %#v", body)
		}
	})
	t.Run("inactive organisation returns actionable 409", func(t *testing.T) {
		orgs := organisation.NewService(organisation.NewMemoryRepository())
		org, err := orgs.Create(context.Background(), organisation.CreateInput{LegalName: "Synthetic Test Organisation", CountryISO2: "NG"})
		if err != nil {
			t.Fatal(err)
		}
		payload := map[string]any{}
		for k, v := range base {
			payload[k] = v
		}
		payload["organisationId"] = org.ID
		service := consent.NewService(consent.NewMemoryRepository()).WithOrganisationReader(orgs)
		code, body := consentCreateRequest(t, service, payload)
		if code != 409 || body["error"] != "ORGANISATION_NOT_ACTIVE" {
			t.Fatalf("expected 409 active-org gate, got %d: %#v", code, body)
		}
	})
	t.Run("healthy draft without evidence still saves", func(t *testing.T) {
		code, body := consentCreateRequest(t, consent.NewService(consent.NewMemoryRepository()), base)
		if code != 201 || body["status"] != "DRAFT" {
			t.Fatalf("expected draft 201, got %d: %#v", code, body)
		}
	})
}
func asJSON(v any) string { data, _ := json.Marshal(v); return string(data) }
