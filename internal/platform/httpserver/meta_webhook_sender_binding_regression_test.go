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

type metaWebhookDeliveryResolverStub struct {
	expectedSenderID string
	deliveries       *delivery.Service
}

func (r metaWebhookDeliveryResolverStub) ResolveMetaWebhookRecipient(ctx context.Context, sender metacloud.Sender, providerMessageID string) (delivery.Recipient, error) {
	if sender.ID != r.expectedSenderID {
		return delivery.Recipient{}, metacloud.ErrWebhookSenderMismatch
	}
	return r.deliveries.GetByProviderMessageID(ctx, providerMessageID)
}

func (r metaWebhookDeliveryResolverStub) ResolveMetaWebhookInboundRecipient(ctx context.Context, sender metacloud.Sender, _ string) (delivery.Recipient, error) {
	if sender.ID != r.expectedSenderID {
		return delivery.Recipient{}, delivery.ErrRecipientNotFound
	}
	return r.deliveries.GetByProviderMessageID(ctx, "wamid.1")
}

func TestCouncilMetaInboundCannotSuppressRecipientBoundToDifferentSender(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 13, 7, 30, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "recipient-victim", ContactID: "contact-victim", CampaignID: "campaign-victim", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.victim", UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	ledger := consent.NewLedgerService(consent.NewMemoryLedgerRepository())
	processor := &consent.OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Clock: func() time.Time { return now }}
	callbackSender := metacloud.Sender{ID: "meta-sender-a", WABAID: "waba-a", PhoneNumberID: "phone-a", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: callbackSender},
		MetaWebhookDeliveries: metaWebhookDeliveryResolverStub{expectedSenderID: "meta-sender-b", deliveries: deliveries},
		DeliveryEvents:        deliveries, OptOutProcessor: processor,
	}).Handler()
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-a","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-a"},"messages":[{"from":"2348012345678","id":"wamid.inbound","timestamp":"1786586406","type":"text","context":{"id":"wamid.victim"},"text":{"body":"STOP"}}]}}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	req.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook("meta-app-secret-0123456789", body))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"ignoredInbound":1`) {
		t.Fatalf("callback=%d %s", res.Code, res.Body.String())
	}
	events, err := ledger.Events(context.Background(), recipient.ContactID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("cross-sender inbound created suppression evidence: %#v", events)
	}
}

func TestCouncilMetaQuotedInboundRequiresFromToMatchQuotedContact(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 13, 7, 30, 0, 0, time.UTC)
	fromRecipient := delivery.Recipient{ID: "recipient-from", ContactID: "contact-from", CampaignID: "campaign-a", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.1", UpdatedAt: now}
	quotedRecipient := delivery.Recipient{ID: "recipient-quoted", ContactID: "contact-quoted", CampaignID: "campaign-b", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.victim", UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(fromRecipient, quotedRecipient))
	ledger := consent.NewLedgerService(consent.NewMemoryLedgerRepository())
	processor := &consent.OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Clock: func() time.Time { return now }}
	callbackSender := metacloud.Sender{ID: "meta-sender-a", WABAID: "waba-a", PhoneNumberID: "phone-a", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: callbackSender},
		MetaWebhookDeliveries: metaWebhookDeliveryResolverStub{expectedSenderID: "meta-sender-a", deliveries: deliveries},
		DeliveryEvents:        deliveries, OptOutProcessor: processor,
	}).Handler()
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-a","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-a"},"messages":[{"from":"2348012345678","id":"wamid.inbound.mismatch","timestamp":"1786586406","type":"text","context":{"id":"wamid.victim"},"text":{"body":"STOP"}}]}}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	req.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook("meta-app-secret-0123456789", body))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"ignoredInbound":1`) {
		t.Fatalf("callback=%d %s", res.Code, res.Body.String())
	}
	events, err := ledger.Events(context.Background(), quotedRecipient.ContactID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("quoted STOP suppressed a contact that did not match From: %#v", events)
	}
}

func TestCouncilMetaUnquotedInboundCannotSuppressRecipientBoundToDifferentSender(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 13, 7, 30, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "recipient-victim", ContactID: "contact-victim", CampaignID: "campaign-victim", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.1", UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	ledger := consent.NewLedgerService(consent.NewMemoryLedgerRepository())
	processor := &consent.OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Clock: func() time.Time { return now }}
	callbackSender := metacloud.Sender{ID: "meta-sender-a", WABAID: "waba-a", PhoneNumberID: "phone-a", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: callbackSender},
		MetaWebhookDeliveries: metaWebhookDeliveryResolverStub{expectedSenderID: "meta-sender-b", deliveries: deliveries},
		DeliveryEvents:        deliveries, OptOutProcessor: processor,
	}).Handler()
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-a","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-a"},"messages":[{"from":"2348012345678","id":"wamid.inbound.unquoted","timestamp":"1786586406","type":"text","text":{"body":"STOP"}}]}}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	req.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook("meta-app-secret-0123456789", body))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"ignoredInbound":1`) {
		t.Fatalf("callback=%d %s", res.Code, res.Body.String())
	}
	events, err := ledger.Events(context.Background(), recipient.ContactID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("cross-sender unquoted inbound created suppression evidence: %#v", events)
	}
}

func TestCouncilMetaStatusCannotMutateRecipientBoundToDifferentSender(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 13, 7, 30, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "recipient-victim", ContactID: "contact-victim", CampaignID: "campaign-victim", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.victim", UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	callbackSender := metacloud.Sender{ID: "meta-sender-a", WABAID: "waba-a", PhoneNumberID: "phone-a", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: callbackSender},
		MetaWebhookDeliveries: metaWebhookDeliveryResolverStub{expectedSenderID: "meta-sender-b", deliveries: deliveries}, DeliveryEvents: deliveries,
	}).Handler()
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-a","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-a"},"statuses":[{"id":"wamid.victim","status":"read","timestamp":"1786586405"}]}}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	req.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook("meta-app-secret-0123456789", body))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"ignoredStatuses":1`) {
		t.Fatalf("callback=%d %s", res.Code, res.Body.String())
	}
	stored, err := deliveries.Get(context.Background(), recipient.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != delivery.StatusGatewayAccepted {
		t.Fatalf("cross-sender callback mutated victim: %#v", stored)
	}
}
