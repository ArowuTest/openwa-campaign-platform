package httpserver

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/metacloud"
)

type selectiveMetaWebhookSenderResolver struct{ sender metacloud.Sender }

func (s selectiveMetaWebhookSenderResolver) ResolveMetaWebhookSender(_ context.Context, credentialKey, wabaID, phoneNumberID string) (metacloud.Sender, error) {
	if credentialKey == s.sender.CredentialKey && wabaID == s.sender.WABAID && phoneNumberID == s.sender.PhoneNumberID {
		return s.sender, nil
	}
	return metacloud.Sender{}, metacloud.ErrNotFound
}

func TestMetaWebhookSkipsUnboundSiblingWithoutBlockingBoundChange(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 12, 30, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "recipient-atomic", ContactID: "contact-atomic", CampaignID: "campaign-atomic", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.atomic", UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	sender := metacloud.Sender{ID: "meta-sender-atomic", OrganisationID: "org-atomic", WABAID: "waba-atomic", PhoneNumberID: "phone-bound", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: selectiveMetaWebhookSenderResolver{sender: sender}, MetaWebhookMaxBody: 1 << 20,
		MetaWebhookDeliveries: metaWebhookDeliveryResolverStub{expectedSenderID: sender.ID, deliveries: deliveries},
		DeliveryEvents:        deliveries,
	}).Handler()
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-atomic","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-bound"},"statuses":[{"id":"wamid.atomic","status":"delivered","timestamp":"1786797000"}]}},{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-unbound"},"statuses":[{"id":"wamid.other","status":"sent","timestamp":"1786797001"}]}}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	req.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook("meta-app-secret-0123456789", body))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("mixed bound/unbound response=%d body=%s", res.Code, res.Body.String())
	}
	value, err := deliveries.Get(context.Background(), recipient.ID)
	if err != nil {
		t.Fatal(err)
	}
	if value.Status != delivery.StatusDelivered {
		t.Fatalf("bound change was not applied while unbound sibling was ignored: %s", value.Status)
	}
}
