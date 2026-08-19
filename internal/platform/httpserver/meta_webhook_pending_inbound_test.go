package httpserver

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/consent"
	"campaign-platform/internal/delivery"
	"campaign-platform/internal/metacloud"
)

type pendingStatusInboundResolver struct{ recipient delivery.Recipient }

func (r pendingStatusInboundResolver) ResolveMetaWebhookRecipient(_ context.Context, _ metacloud.Sender, providerMessageID string) (delivery.Recipient, error) {
	if providerMessageID == "wamid.pending" {
		return delivery.Recipient{}, delivery.ErrRecipientNotFound
	}
	return r.recipient, nil
}
func (r pendingStatusInboundResolver) ResolveMetaWebhookInboundRecipient(_ context.Context, _ metacloud.Sender, _ string) (delivery.Recipient, error) {
	return r.recipient, nil
}

func TestMetaWebhookPendingStatusDoesNotBlockInboundStopProcessing(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 16, 21, 15, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "recipient-pending-stop", ContactID: "contact-pending-stop", CampaignID: "campaign-pending-stop", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.known", UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	ledger := consent.NewLedgerService(consent.NewMemoryLedgerRepository())
	processor := &consent.OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Clock: func() time.Time { return now }}
	windows := &recordingMetaConversationWindows{}
	sender := metacloud.Sender{ID: "meta-sender-pending-stop", OrganisationID: "org-pending-stop", WABAID: "waba-pending-stop", PhoneNumberID: "phone-pending-stop", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: sender}, MetaWebhookMaxBody: 1 << 20,
		MetaWebhookDeliveries: pendingStatusInboundResolver{recipient: recipient}, DeliveryEvents: deliveries,
		OptOutProcessor: processor, MetaConversationWindows: windows, MetaConversationWindow: 24 * time.Hour,
	}).Handler()
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-pending-stop","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-pending-stop"},"statuses":[{"id":"wamid.pending","status":"sent","timestamp":"1786914900"}],"messages":[{"from":"2348012345678","id":"wamid.in.pending-stop","timestamp":"1786914901","type":"text","text":{"body":"STOP"}}]}}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	req.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook("meta-app-secret-0123456789", body))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable || !strings.Contains(res.Body.String(), "META_WEBHOOK_RECIPIENT_PENDING") {
		t.Fatalf("pending status should retain retry response after inbound processing: %d %s", res.Code, res.Body.String())
	}
	if len(windows.observations) != 1 || windows.observations[0].ProviderMessageID != "wamid.in.pending-stop" {
		t.Fatalf("pending status blocked durable conversation-window evidence: %#v", windows.observations)
	}
	events, err := ledger.Events(context.Background(), recipient.ContactID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventType != "SUPPRESSION_CREATED" {
		t.Fatalf("pending status blocked STOP processing: %#v", events)
	}

}
