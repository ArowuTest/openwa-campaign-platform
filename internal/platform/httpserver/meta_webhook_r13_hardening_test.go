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

	"campaign-platform/internal/consent"
	"campaign-platform/internal/delivery"
	"campaign-platform/internal/metacloud"
)

func TestMetaWebhookRejectsFarFutureInboundBeforeWindowAuthority(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	recipient := delivery.Recipient{ID: "recipient-future-inbound", ContactID: "contact-future-inbound", CampaignID: "campaign-future-inbound", Status: delivery.StatusGatewayAccepted, UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	ledger := consent.NewLedgerService(consent.NewMemoryLedgerRepository())
	windows := &recordingMetaConversationWindows{}
	processor := &consent.OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Clock: func() time.Time { return now }}
	sender := metacloud.Sender{ID: "meta-sender-future-inbound", OrganisationID: "org-future-inbound", WABAID: "waba-future-inbound", PhoneNumberID: "phone-future-inbound", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: sender}, MetaWebhookMaxBody: 1 << 20,
		MetaWebhookDeliveries: pendingStatusInboundResolver{recipient: recipient}, DeliveryEvents: deliveries,
		OptOutProcessor: processor, MetaConversationWindows: windows, MetaConversationWindow: 24 * time.Hour,
	}).Handler()
	body := []byte(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":"waba-future-inbound","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-future-inbound"},"messages":[{"from":"2348012345678","id":"wamid.in.future","timestamp":"%d","type":"text","text":{"body":"STOP"}}]}}]}]}`, now.Add(24*time.Hour).Unix()))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	req.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook("meta-app-secret-0123456789", body))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnprocessableEntity || !strings.Contains(res.Body.String(), "META_WEBHOOK_TIMESTAMP_INVALID") {
		t.Fatalf("future inbound response=%d body=%s", res.Code, res.Body.String())
	}
	if len(windows.observations) != 0 {
		t.Fatalf("future inbound minted window authority: %#v", windows.observations)
	}
	events, err := ledger.Events(context.Background(), recipient.ContactID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("future inbound mutated consent ledger: %#v", events)
	}
}

func TestMetaWebhookFutureStatusDoesNotBlockValidInboundStop(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	recipient := delivery.Recipient{ID: "recipient-skew-stop", ContactID: "contact-skew-stop", CampaignID: "campaign-skew-stop", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.skew", UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	ledger := consent.NewLedgerService(consent.NewMemoryLedgerRepository())
	windows := &recordingMetaConversationWindows{}
	processor := &consent.OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Clock: func() time.Time { return now }}
	sender := metacloud.Sender{ID: "meta-sender-skew-stop", OrganisationID: "org-skew-stop", WABAID: "waba-skew-stop", PhoneNumberID: "phone-skew-stop", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: sender}, MetaWebhookMaxBody: 1 << 20,
		MetaWebhookDeliveries: pendingStatusInboundResolver{recipient: recipient}, DeliveryEvents: deliveries,
		OptOutProcessor: processor, MetaConversationWindows: windows, MetaConversationWindow: 24 * time.Hour,
	}).Handler()
	body := []byte(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":"waba-skew-stop","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-skew-stop"},"statuses":[{"id":"wamid.skew","status":"sent","timestamp":"%d"}],"messages":[{"from":"2348012345678","id":"wamid.in.skew-stop","timestamp":"%d","type":"text","text":{"body":"STOP"}}]}}]}]}`, now.Add(24*time.Hour).Unix(), now.Unix()))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	req.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook("meta-app-secret-0123456789", body))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnprocessableEntity || !strings.Contains(res.Body.String(), "META_WEBHOOK_TIMESTAMP_INVALID") {
		t.Fatalf("mixed skew response=%d body=%s", res.Code, res.Body.String())
	}
	if len(windows.observations) != 1 || windows.observations[0].ProviderMessageID != "wamid.in.skew-stop" {
		t.Fatalf("bad status blocked valid window evidence: %#v", windows.observations)
	}
	events, err := ledger.Events(context.Background(), recipient.ContactID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventType != "SUPPRESSION_CREATED" {
		t.Fatalf("bad status blocked STOP: %#v", events)
	}
	stored, err := deliveries.Get(context.Background(), recipient.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != delivery.StatusGatewayAccepted || stored.LastEventAt != nil {
		t.Fatalf("skew status mutated delivery: %#v", stored)
	}
}

func TestMetaWebhookFutureInboundDoesNotBlockLaterValidStop(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	recipient := delivery.Recipient{ID: "recipient-mixed-inbound", ContactID: "contact-mixed-inbound", CampaignID: "campaign-mixed-inbound", Status: delivery.StatusGatewayAccepted, UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	ledger := consent.NewLedgerService(consent.NewMemoryLedgerRepository())
	windows := &recordingMetaConversationWindows{}
	processor := &consent.OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Clock: func() time.Time { return now }}
	sender := metacloud.Sender{ID: "meta-sender-mixed-inbound", OrganisationID: "org-mixed-inbound", WABAID: "waba-mixed-inbound", PhoneNumberID: "phone-mixed-inbound", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: sender}, MetaWebhookMaxBody: 1 << 20, MetaWebhookDeliveries: pendingStatusInboundResolver{recipient: recipient}, DeliveryEvents: deliveries, OptOutProcessor: processor, MetaConversationWindows: windows, MetaConversationWindow: 24 * time.Hour}).Handler()
	body := []byte(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":"waba-mixed-inbound","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-mixed-inbound"},"messages":[{"from":"2348012345678","id":"wamid.in.future-first","timestamp":"%d","type":"text","text":{"body":"hello"}},{"from":"2348012345678","id":"wamid.in.valid-stop","timestamp":"%d","type":"text","text":{"body":"STOP"}}]}}]}]}`, now.Add(24*time.Hour).Unix(), now.Unix()))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	req.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook("meta-app-secret-0123456789", body))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mixed inbound response=%d body=%s", res.Code, res.Body.String())
	}
	if len(windows.observations) != 1 || windows.observations[0].ProviderMessageID != "wamid.in.valid-stop" {
		t.Fatalf("bad inbound blocked valid window: %#v", windows.observations)
	}
	events, err := ledger.Events(context.Background(), recipient.ContactID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventType != "SUPPRESSION_CREATED" {
		t.Fatalf("bad inbound blocked valid STOP: %#v", events)
	}
}
