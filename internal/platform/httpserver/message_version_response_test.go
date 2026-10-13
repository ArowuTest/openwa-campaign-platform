package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/message"
)

func TestMessageVersionBrowserRepresentationNeverExposesPrivateObjectKey(t *testing.T) {
	repository := message.NewMemoryRepository()
	service := message.NewService(repository)
	created, err := service.CreateDraft(context.Background(), message.Input{
		CampaignID: "campaign-message-privacy",
		Type:       message.TypeImageCaption,
		Body:       "Governed image",
		Media: &message.Media{
			AssetID: "asset-1", ObjectKey: "private/trusted-assets/message/asset-1/creative.png",
			SHA256: strings.Repeat("a", 64), MediaType: "image/png", Size: 1234, ScanStatus: "CLEAN",
		},
		CreatedBy:      "maker",
		IdempotencyKey: "message-privacy-test-0001",
	})
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{deps: Dependencies{Messages: service}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/campaigns/campaign-message-privacy/message-versions?limit=20", nil)
	request.SetPathValue("id", "campaign-message-privacy")
	response := httptest.NewRecorder()
	server.listMessageVersions(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	for _, forbidden := range []string{"objectKey", "private/trusted-assets/", "message-privacy-test-0001"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("private internal field leaked (%s): %s", forbidden, response.Body.String())
		}
	}
	for _, required := range []string{created.ID, "asset-1", strings.Repeat("a", 64), "image/png", "CLEAN"} {
		if !strings.Contains(response.Body.String(), required) {
			t.Fatalf("safe evidence %q missing: %s", required, response.Body.String())
		}
	}

	now := time.Now().UTC()
	approved := created
	approved.Status = message.StatusApproved
	approved.ApprovedBy = "checker"
	approved.ApprovedAt = &now
	public := publicMessageVersion(approved)
	if public.Media == nil || public.Media.AssetID != "asset-1" || public.Media.MediaType != "image/png" {
		t.Fatalf("safe message media representation incomplete: %+v", public.Media)
	}
}
