package gateway

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
)

func TestCallbackSignatureBindsTimestampAndRawBody(t *testing.T) {
	secret := bytes.Repeat([]byte{7}, 32)
	now := time.Unix(1_800_000_000, 0).UTC()
	body := []byte(`{"schemaVersion":"1.0","eventId":"evt-1"}`)
	timestamp, signature, err := SignCallback(secret, now, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCallback(secret, timestamp, signature, body, now.Add(time.Minute), 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := VerifyCallback(secret, timestamp, signature, append(body, ' '), now, 5*time.Minute); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("modified body accepted: %v", err)
	}
	if err := VerifyCallback(secret, timestamp, signature, body, now.Add(6*time.Minute), 5*time.Minute); !errors.Is(err, ErrTimestampOutsideWindow) {
		t.Fatalf("stale callback accepted: %v", err)
	}
}

func TestDecodeAndMapCallback(t *testing.T) {
	body := []byte(`{"schemaVersion":"1.0","eventId":"evt-1","eventType":"message.delivered","sessionId":"session-1","clientReference":"recipient-1","providerMessageId":"provider-1","occurredAt":"2026-08-04T08:00:00Z"}`)
	event, err := DecodeCallback(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := event.Validate(time.Date(2026, 8, 4, 8, 1, 0, 0, time.UTC), 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	mapped := event.DeliveryEvent()
	if mapped.Type != delivery.EventDelivered || mapped.DeduplicationKey != "gateway:evt-1" || mapped.ProviderMessageID != "provider-1" {
		t.Fatalf("unexpected mapping: %+v", mapped)
	}
}

func TestCallbackRejectsUnknownFieldAndWeakEvidence(t *testing.T) {
	_, err := DecodeCallback([]byte(`{"schemaVersion":"1.0","eventId":"evt-1","eventType":"message.delivered","sessionId":"s","clientReference":"r","occurredAt":"2026-08-04T08:00:00Z","unexpected":true}`))
	if !errors.Is(err, ErrCallbackInvalid) {
		t.Fatalf("unknown field accepted: %v", err)
	}
	event := CallbackEvent{SchemaVersion: "1.0", EventID: "evt-1", EventType: delivery.EventDelivered, SessionID: "s", ClientReference: "r", OccurredAt: time.Now().UTC()}
	if err := event.Validate(time.Now().UTC(), time.Minute); err == nil {
		t.Fatal("delivery acknowledgement without provider message id accepted")
	}
}
