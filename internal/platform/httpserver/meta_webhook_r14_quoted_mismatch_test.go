package httpserver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/consent"
	"campaign-platform/internal/delivery"
	"campaign-platform/internal/metacloud"
)

type quotedMismatchFromResolver struct {
	recipient delivery.Recipient
}

func (r quotedMismatchFromResolver) ResolveMetaWebhookRecipient(context.Context, metacloud.Sender, string) (delivery.Recipient, error) {
	return delivery.Recipient{}, metacloud.ErrWebhookSenderMismatch
}

func (r quotedMismatchFromResolver) ResolveMetaWebhookInboundRecipient(context.Context, metacloud.Sender, string) (delivery.Recipient, error) {
	return r.recipient, nil
}

func TestMetaQuotedSenderMismatchFallsBackToAuthenticatedFromForStop(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	recipient := delivery.Recipient{
		ID: "recipient-quote-fallback", ContactID: "contact-quote-fallback", CampaignID: "campaign-quote-fallback",
		Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.current-sender", UpdatedAt: now,
	}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	ledger := consent.NewLedgerService(consent.NewMemoryLedgerRepository())
	windows := &recordingMetaConversationWindows{}
	processor := &consent.OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Clock: func() time.Time { return now }}
	sender := metacloud.Sender{
		ID: "meta-sender-quote-fallback", OrganisationID: "org-quote-fallback", WABAID: "waba-quote-fallback",
		PhoneNumberID: "phone-quote-fallback", CredentialKey: "meta-ng", Status: metacloud.StatusActive,
	}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: sender}, MetaWebhookMaxBody: 1 << 20,
		MetaWebhookDeliveries: quotedMismatchFromResolver{recipient: recipient}, DeliveryEvents: deliveries, OptOutProcessor: processor,
		MetaConversationWindows: windows, MetaConversationWindow: 24 * time.Hour,
	}).Handler()
	body := []byte(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":"waba-quote-fallback","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-quote-fallback"},"messages":[{"from":"2348012345678","id":"wamid.in.quote-fallback","timestamp":"%d","type":"text","context":{"id":"wamid.other-sender"},"text":{"body":"STOP"}}]}}]}]}`, now.Unix()))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	req.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook("meta-app-secret-0123456789", body))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("quoted mismatch response=%d body=%s", res.Code, res.Body.String())
	}
	if len(windows.observations) != 1 || windows.observations[0].ContactID != recipient.ContactID {
		t.Fatalf("quoted mismatch lost authenticated From window: %#v", windows.observations)
	}
	events, err := ledger.Events(context.Background(), recipient.ContactID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventType != "SUPPRESSION_CREATED" {
		t.Fatalf("quoted mismatch lost authenticated STOP: %#v", events)
	}
}
