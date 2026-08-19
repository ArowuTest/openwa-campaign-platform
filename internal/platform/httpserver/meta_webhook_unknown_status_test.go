package httpserver

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/metacloud"
)

func TestMetaWebhookUnknownWAMIDStatusReturnsRetryableServerError(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 16, 7, 30, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "recipient-known", ContactID: "contact-known", CampaignID: "campaign-known", Status: delivery.StatusSubmitting, ProviderMessageID: "wamid.known", UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	sender := metacloud.Sender{ID: "meta-sender-known", WABAID: "waba-known", PhoneNumberID: "phone-known", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: sender}, MetaWebhookMaxBody: 1 << 20,
		MetaWebhookDeliveries: metaWebhookDeliveryResolverStub{expectedSenderID: sender.ID, deliveries: deliveries},
		DeliveryEvents:        deliveries,
	}).Handler()
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-known","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-known"},"statuses":[{"id":"wamid.not-yet-visible","status":"sent","timestamp":"1786861800"}]}}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	req.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook("meta-app-secret-0123456789", body))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("unknown WAMID response=%d body=%s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "META_WEBHOOK_RECIPIENT_PENDING") {
		t.Fatalf("unknown WAMID response did not expose bounded retry code: %s", res.Body.String())
	}
	unchanged, err := deliveries.Get(req.Context(), recipient.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != delivery.StatusSubmitting {
		t.Fatalf("unknown WAMID mutated unrelated delivery: %s", unchanged.Status)
	}
}
