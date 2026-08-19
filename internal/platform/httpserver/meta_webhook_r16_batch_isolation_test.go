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

type mixedMetaWebhookSenderResolver struct{ bound metacloud.Sender }

func (r mixedMetaWebhookSenderResolver) ResolveMetaWebhookSender(_ context.Context, credentialKey, wabaID, phoneID string) (metacloud.Sender, error) {
	if credentialKey == r.bound.CredentialKey && wabaID == r.bound.WABAID && phoneID == r.bound.PhoneNumberID {
		return r.bound, nil
	}
	return metacloud.Sender{}, metacloud.ErrNotFound
}
func newR16MetaBatchHarness(t *testing.T, sender metacloud.Sender) (http.Handler, *delivery.Service, *consent.LedgerService, *recordingMetaConversationWindows, string) {
	t.Helper()
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	recipient := delivery.Recipient{ID: "recipient-r16-batch", ContactID: "contact-r16-batch", CampaignID: "campaign-r16-batch", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.r16.known", UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	ledger := consent.NewLedgerService(consent.NewMemoryLedgerRepository())
	processor := &consent.OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Clock: func() time.Time { return now }}
	windows := &recordingMetaConversationWindows{}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: mixedMetaWebhookSenderResolver{bound: sender}, MetaWebhookMaxBody: 1 << 20,
		MetaWebhookDeliveries: pendingStatusInboundResolver{recipient: recipient}, DeliveryEvents: deliveries,
		OptOutProcessor: processor, MetaConversationWindows: windows, MetaConversationWindow: 24 * time.Hour,
	}).Handler()
	return handler, deliveries, ledger, windows, "meta-app-secret-0123456789"
}

func postR16MetaBatch(handler http.Handler, secret string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	req.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook(secret, body))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}
func TestMetaWebhookReplayConflictDoesNotBlockSiblingStop(t *testing.T) {
	sender := metacloud.Sender{ID: "meta-sender-r16", OrganisationID: "org-r16", WABAID: "waba-r16", PhoneNumberID: "phone-r16", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler, deliveries, ledger, windows, secret := newR16MetaBatchHarness(t, sender)
	now := time.Now().UTC().Truncate(time.Second)
	key := "meta-status:wamid.r16.known:sent:" + fmt.Sprint(now.Unix())
	if _, _, err := deliveries.ApplyEvent(context.Background(), "recipient-r16-batch", delivery.Event{
		DeduplicationKey: key, Type: delivery.EventSent, ProviderMessageID: "wamid.r16.known",
		ErrorCode: "PRESEEDED_CONFLICT", OccurredAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":"waba-r16","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-r16"},"statuses":[{"id":"wamid.r16.known","status":"sent","timestamp":"%d"}],"messages":[{"from":"2348012345678","id":"wamid.r16.stop","timestamp":"%d","type":"text","text":{"body":"STOP"}}]}}]}]}`, now.Unix(), now.Unix()))
	res := postR16MetaBatch(handler, secret, body)
	if res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), "META_WEBHOOK_REPLAY_CONFLICT") {
		t.Fatalf("expected replay conflict response after sibling processing, got %d %s", res.Code, res.Body.String())
	}
	if len(windows.observations) != 1 || windows.observations[0].ProviderMessageID != "wamid.r16.stop" {
		t.Fatalf("status replay conflict blocked sibling window evidence: %#v", windows.observations)
	}
	events, err := ledger.Events(context.Background(), "contact-r16-batch", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventType != "SUPPRESSION_CREATED" {
		t.Fatalf("status replay conflict blocked sibling STOP: %#v", events)
	}
}

func TestMetaWebhookUnboundChangeDoesNotBlockLaterBoundStop(t *testing.T) {
	sender := metacloud.Sender{ID: "meta-sender-r16-bound", OrganisationID: "org-r16-bound", WABAID: "waba-r16-bound", PhoneNumberID: "phone-r16-bound", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler, _, ledger, windows, secret := newR16MetaBatchHarness(t, sender)
	now := time.Now().UTC().Truncate(time.Second)
	body := []byte(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":"waba-r16-unbound","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-r16-unbound"},"messages":[{"from":"2348012345678","id":"wamid.r16.unbound","timestamp":"%d","type":"text","text":{"body":"hello"}}]}}]},{"id":"waba-r16-bound","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-r16-bound"},"messages":[{"from":"2348012345678","id":"wamid.r16.bound-stop","timestamp":"%d","type":"text","text":{"body":"STOP"}}]}}]}]}`, now.Unix(), now.Unix()))
	res := postR16MetaBatch(handler, secret, body)
	if res.Code != http.StatusOK {
		t.Fatalf("bound sender work should be accepted despite unbound sibling: %d %s", res.Code, res.Body.String())
	}
	if len(windows.observations) != 1 || windows.observations[0].ProviderMessageID != "wamid.r16.bound-stop" {
		t.Fatalf("unbound sibling blocked bound conversation-window evidence: %#v", windows.observations)
	}
	events, err := ledger.Events(context.Background(), "contact-r16-batch", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventType != "SUPPRESSION_CREATED" {
		t.Fatalf("unbound sibling blocked bound STOP: %#v", events)
	}
}

func TestR17MetaWebhookConversationWindowConflictStillProcessesStop(t *testing.T) {
	sender := metacloud.Sender{ID: "meta-sender-r17-window", OrganisationID: "org-r17-window", WABAID: "waba-r17-window", PhoneNumberID: "phone-r17-window", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler, _, ledger, windows, secret := newR16MetaBatchHarness(t, sender)
	windows.err = metacloud.ErrConversationWindowConflict
	now := time.Now().UTC().Truncate(time.Second)
	body := []byte(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":"waba-r17-window","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-r17-window"},"messages":[{"from":"2348012345678","id":"wamid.r17.window-stop","timestamp":"%d","type":"text","text":{"body":"STOP"}}]}}]}]}`, now.Unix()))
	res := postR16MetaBatch(handler, secret, body)
	if res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), "META_WEBHOOK_REPLAY_CONFLICT") {
		t.Fatalf("expected replay conflict after suppression processing, got %d %s", res.Code, res.Body.String())
	}
	events, err := ledger.Events(context.Background(), "contact-r16-batch", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventType != "SUPPRESSION_CREATED" {
		t.Fatalf("conversation-window conflict blocked independently authoritative STOP suppression: %#v", events)
	}
}
