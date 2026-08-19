package httpserver

import (
	"bytes"
	"context"
	"fmt"
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

func TestMetaWebhookRejectsFarFutureStatusTimestamp(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	recipient := delivery.Recipient{ID: "recipient-future-status", ContactID: "contact-future-status", CampaignID: "campaign-future-status", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.future-status", UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	sender := metacloud.Sender{ID: "meta-sender-future-status", OrganisationID: "org-future-status", WABAID: "waba-future-status", PhoneNumberID: "phone-future-status", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: sender}, MetaWebhookMaxBody: 1 << 20,
		MetaWebhookDeliveries: metaWebhookDeliveryResolverStub{expectedSenderID: sender.ID, deliveries: deliveries}, DeliveryEvents: deliveries,
	}).Handler()
	future := now.Add(24 * time.Hour).Unix()
	body := []byte(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":"waba-future-status","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-future-status"},"statuses":[{"id":"wamid.future-status","status":"sent","timestamp":"%d"}]}}]}]}`, future))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	req.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook("meta-app-secret-0123456789", body))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnprocessableEntity || !strings.Contains(res.Body.String(), "META_WEBHOOK_TIMESTAMP_INVALID") {
		t.Fatalf("future Meta status response=%d body=%s", res.Code, res.Body.String())
	}
	stored, err := deliveries.Get(context.Background(), recipient.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != delivery.StatusGatewayAccepted || stored.LastEventAt != nil {
		t.Fatalf("future status poisoned delivery chronology: %#v", stored)
	}
}
