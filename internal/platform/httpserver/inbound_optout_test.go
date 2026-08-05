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
	"campaign-platform/internal/gateway"
)

func TestSignedInboundStopCreatesIdempotentSuppression(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	secret := bytes.Repeat([]byte{9}, 32)
	recipient := delivery.Recipient{ID: "recipient-1", ContactID: "00000000-0000-4000-8000-000000000010", Status: delivery.StatusDelivered, UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	ledger := consent.NewLedgerService(consent.NewMemoryLedgerRepository())
	processor := &consent.OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Policy: consent.DefaultOptOutPolicy(), Clock: func() time.Time { return now }}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{OptOutProcessor: processor, GatewayCallbackSecret: secret, GatewayCallbackMaxSkew: 5 * time.Minute}).Handler()

	body := []byte(`{"schemaVersion":"1.0","eventId":"reply-1","sessionId":"session-1","clientReference":"recipient-1","providerMessageId":"provider-reply-1","messageText":"STOP","occurredAt":"` + now.Format(time.RFC3339) + `"}`)
	call := func(payload []byte) *httptest.ResponseRecorder {
		timestamp, signature, err := gateway.SignCallback(secret, now, payload)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/internal/gateway/inbound", bytes.NewReader(payload))
		request.Header.Set(gateway.TimestampHeader, timestamp)
		request.Header.Set(gateway.SignatureHeader, signature)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	first := call(body)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"recognisedOptOut":true`) || !strings.Contains(first.Body.String(), `"replayed":false`) {
		t.Fatalf("first inbound: %d %s", first.Code, first.Body.String())
	}
	replay := call(body)
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), `"replayed":true`) {
		t.Fatalf("replay inbound: %d %s", replay.Code, replay.Body.String())
	}
	events, err := ledger.Events(context.Background(), recipient.ContactID, 10)
	if err != nil || len(events) != 1 || events[0].EventType != "SUPPRESSION_CREATED" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestInboundNonStopDoesNotCreateSuppression(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	secret := bytes.Repeat([]byte{8}, 32)
	deliveries := delivery.NewService(delivery.NewMemoryRepository(delivery.Recipient{ID: "recipient-2", ContactID: "contact-2", Status: delivery.StatusDelivered, UpdatedAt: now}))
	ledger := consent.NewLedgerService(consent.NewMemoryLedgerRepository())
	processor := &consent.OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Clock: func() time.Time { return now }}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{OptOutProcessor: processor, GatewayCallbackSecret: secret, GatewayCallbackMaxSkew: 5 * time.Minute}).Handler()
	body := []byte(`{"schemaVersion":"1.0","eventId":"reply-2","sessionId":"session-1","clientReference":"recipient-2","messageText":"do not stop","occurredAt":"` + now.Format(time.RFC3339) + `"}`)
	timestamp, signature, _ := gateway.SignCallback(secret, now, body)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/internal/gateway/inbound", bytes.NewReader(body))
	request.Header.Set(gateway.TimestampHeader, timestamp)
	request.Header.Set(gateway.SignatureHeader, signature)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"recognisedOptOut":false`) {
		t.Fatalf("non-stop inbound: %d %s", response.Code, response.Body.String())
	}
	events, _ := ledger.Events(context.Background(), "contact-2", 10)
	if len(events) != 0 {
		t.Fatalf("unexpected consent events: %+v", events)
	}
}
