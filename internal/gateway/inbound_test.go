package gateway

import (
	"errors"
	"testing"
	"time"
)

func TestDecodeAndValidateInboundMessage(t *testing.T) {
	body := []byte(`{"schemaVersion":"1.0","eventId":"reply-1","sessionId":"session-1","clientReference":"recipient-1","providerMessageId":"provider-reply-1","messageText":"STOP","occurredAt":"2026-08-04T08:00:00Z"}`)
	event, err := DecodeInboundMessage(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := event.Validate(time.Date(2026, 8, 4, 8, 1, 0, 0, time.UTC), 5*time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestInboundMessageRejectsUnknownFieldsAndOversizeText(t *testing.T) {
	_, err := DecodeInboundMessage([]byte(`{"schemaVersion":"1.0","eventId":"reply-1","sessionId":"s","clientReference":"r","messageText":"STOP","occurredAt":"2026-08-04T08:00:00Z","unexpected":true}`))
	if !errors.Is(err, ErrCallbackInvalid) {
		t.Fatalf("unknown field accepted: %v", err)
	}
	event := InboundMessageEvent{SchemaVersion: "1.0", EventID: "reply-1", SessionID: "s", ClientReference: "r", MessageText: "", OccurredAt: time.Now().UTC()}
	if err := event.Validate(time.Now().UTC(), time.Minute); err == nil {
		t.Fatal("blank inbound message accepted")
	}
}
