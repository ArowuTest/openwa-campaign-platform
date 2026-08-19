package httpserver

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

type incompleteMetaWebhookSenderStub struct{ sender metacloud.Sender }

func (s incompleteMetaWebhookSenderStub) ResolveMetaWebhookSender(context.Context, string, string, string) (metacloud.Sender, error) {
	return s.sender, nil
}

type metaWebhookSenderStub struct {
	sender metacloud.Sender
	err    error
}

func (s metaWebhookSenderStub) ResolveMetaWebhookSender(_ context.Context, credentialKey, wabaID, phoneNumberID string) (metacloud.Sender, error) {
	if s.err != nil {
		return metacloud.Sender{}, s.err
	}
	if credentialKey != s.sender.CredentialKey || wabaID != s.sender.WABAID || phoneNumberID != s.sender.PhoneNumberID {
		return metacloud.Sender{}, metacloud.ErrNotFound
	}
	sender := s.sender
	if sender.OrganisationID == "" {
		sender.OrganisationID = "org-test"
	}
	return sender, nil
}
func testMetaWebhookServer(t *testing.T) (http.Handler, *delivery.Service, *consent.LedgerService, string) {
	t.Helper()
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 13, 1, 55, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "recipient-meta-1", ContactID: "contact-meta-1", CampaignID: "campaign-meta-1", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.1", UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	ledger := consent.NewLedgerService(consent.NewMemoryLedgerRepository())
	processor := &consent.OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Clock: func() time.Time { return now }}
	sender := metacloud.Sender{ID: "meta-sender-1", WABAID: "waba-1", PhoneNumberID: "phone-1", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: sender}, MetaWebhookMaxBody: 1 << 20,
		MetaWebhookDeliveries: metaWebhookDeliveryResolverStub{expectedSenderID: sender.ID, deliveries: deliveries},
		DeliveryEvents:        deliveries, OptOutProcessor: processor,
		MetaConversationWindows: &recordingMetaConversationWindows{}, MetaConversationWindow: 24 * time.Hour,
	}).Handler()
	return handler, deliveries, ledger, "meta-app-secret-0123456789"
}

func TestMetaWebhookVerificationChallenge(t *testing.T) {
	handler, _, _, _ := testMetaWebhookServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/meta/meta-ng?hub.mode=subscribe&hub.verify_token=verify-token-012345&hub.challenge=1158201444", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "1158201444" {
		t.Fatalf("challenge response=%d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/meta/meta-ng?hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=1158201444", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("invalid token response=%d %s", response.Code, response.Body.String())
	}
}
func TestMetaWebhookProcessesOutOfOrderStatusesAndQuotedStopIdempotently(t *testing.T) {
	handler, deliveries, ledger, secret := testMetaWebhookServer(t)
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-1","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-1"},"statuses":[{"id":"wamid.1","status":"read","timestamp":"1786586405"},{"id":"wamid.1","status":"delivered","timestamp":"1786586404"}],"messages":[{"from":"2348012345678","id":"wamid.in.1","timestamp":"1786586406","type":"text","context":{"id":"wamid.1"},"text":{"body":"STOP"}}]}}]}]}`)
	call := func(payload []byte, signature string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(payload))
		request.Header.Set(metacloud.WebhookSignatureHeader, signature)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	signature := signMetaWebhook(secret, body)
	first := call(body, signature)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"recognisedOptOuts":1`) {
		t.Fatalf("first webhook=%d %s", first.Code, first.Body.String())
	}
	recipient, err := deliveries.Get(context.Background(), "recipient-meta-1")
	if err != nil || recipient.Status != delivery.StatusRead || recipient.HighestAcknowledgement != delivery.StatusRead {
		t.Fatalf("recipient=%#v err=%v", recipient, err)
	}
	events, err := ledger.Events(context.Background(), "contact-meta-1", 10)
	if err != nil || len(events) != 1 || events[0].EventType != "SUPPRESSION_CREATED" {
		t.Fatalf("consent events=%#v err=%v", events, err)
	}
	replay := call(body, signature)
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), `"replayedInbound":1`) {
		t.Fatalf("replay webhook=%d %s", replay.Code, replay.Body.String())
	}
	events, _ = ledger.Events(context.Background(), "contact-meta-1", 10)
	if len(events) != 1 {
		t.Fatalf("replay duplicated suppression: %#v", events)
	}
}
func TestMetaWebhookRejectsForgedSignatureAndOversizedBody(t *testing.T) {
	handler, _, _, secret := testMetaWebhookServer(t)
	body := []byte(`{"object":"whatsapp_business_account","entry":[]}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	request.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook(secret, append(body, ' ')))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("forged signature response=%d %s", response.Code, response.Body.String())
	}

	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	small := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{MetaCredentials: credentials, MetaWebhookMaxBody: 32}).Handler()
	oversized := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba"}]}`)
	request = httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(oversized))
	request.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook(secret, oversized))
	response = httptest.NewRecorder()
	small.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized response=%d %s", response.Code, response.Body.String())
	}
}

func signMetaWebhook(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestMetaWebhookSenderResolverFailureReturnsRetryableServerError(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials:       credentials,
		MetaWebhookSenders:    metaWebhookSenderStub{err: errors.New("database unavailable")},
		MetaWebhookDeliveries: missingMetaInboundResolver{},
		MetaWebhookMaxBody:    1 << 20,
	}).Handler()
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-1","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-1"},"statuses":[{"id":"wamid.1","status":"sent","timestamp":"1786586405"}]}}]}]}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	request.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook("meta-app-secret-0123456789", body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("resolver infrastructure failure response=%d body=%s", response.Code, response.Body.String())
	}
}

func TestMetaWebhookSenderMissingOrganisationFailsClosed(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	sender := metacloud.Sender{ID: "meta-sender-no-org", WABAID: "waba-1", PhoneNumberID: "phone-1", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{MetaCredentials: credentials, MetaWebhookSenders: incompleteMetaWebhookSenderStub{sender: sender}, MetaWebhookDeliveries: missingMetaInboundResolver{}, MetaWebhookMaxBody: 1 << 20}).Handler()
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-1","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-1"},"statuses":[{"id":"wamid.no-org","status":"delivered","timestamp":"1786586405"}]}}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	req.Header.Set(metacloud.WebhookSignatureHeader, signMetaWebhook("meta-app-secret-0123456789", body))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("incomplete sender authority response=%d body=%s", res.Code, res.Body.String())
	}
}

func TestMetaWebhookUnavailableWhenRuntimeIncomplete(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{MetaCredentials: credentials, MetaWebhookMaxBody: 1 << 20}).Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader([]byte(`{"object":"whatsapp_business_account","entry":[]}`)))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable || !strings.Contains(res.Body.String(), "META_WEBHOOK_UNAVAILABLE") {
		t.Fatalf("incomplete Meta runtime response=%d body=%s", res.Code, res.Body.String())
	}
}
