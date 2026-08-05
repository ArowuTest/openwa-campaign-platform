package gateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"campaign-platform/internal/delivery"
)

const (
	TimestampHeader = "X-Gateway-Timestamp"
	SignatureHeader = "X-Gateway-Signature"
)

var (
	ErrCallbackSecretRequired = errors.New("gateway callback secret is required")
	ErrTimestampRequired      = errors.New("gateway callback timestamp is required")
	ErrTimestampInvalid       = errors.New("gateway callback timestamp is invalid")
	ErrTimestampOutsideWindow = errors.New("gateway callback timestamp is outside the accepted window")
	ErrSignatureRequired      = errors.New("gateway callback signature is required")
	ErrSignatureInvalid       = errors.New("gateway callback signature is invalid")
	ErrCallbackInvalid        = errors.New("gateway callback event is invalid")
)

// CallbackEvent is the versioned internal provider-event contract. clientReference
// is the campaign-recipient identifier assigned by the control plane; the gateway
// must never infer or replace it with a provider identifier.
type CallbackEvent struct {
	SchemaVersion     string             `json:"schemaVersion"`
	EventID           string             `json:"eventId"`
	EventType         delivery.EventType `json:"eventType"`
	SessionID         string             `json:"sessionId"`
	ClientReference   string             `json:"clientReference"`
	ProviderMessageID string             `json:"providerMessageId,omitempty"`
	OccurredAt        time.Time          `json:"occurredAt"`
	ErrorCode         string             `json:"errorCode,omitempty"`
	ErrorDetail       string             `json:"errorDetail,omitempty"`
}

func (e CallbackEvent) Validate(now time.Time, maximumFutureDrift time.Duration) error {
	if e.SchemaVersion != "1.0" {
		return fmt.Errorf("%w: unsupported schemaVersion %q", ErrCallbackInvalid, e.SchemaVersion)
	}
	if !bounded(e.EventID, 1, 200) {
		return fmt.Errorf("%w: eventId must contain 1-200 characters", ErrCallbackInvalid)
	}
	if !bounded(e.SessionID, 1, 200) {
		return fmt.Errorf("%w: sessionId must contain 1-200 characters", ErrCallbackInvalid)
	}
	if strings.TrimSpace(e.ClientReference) != "" && !bounded(e.ClientReference, 1, 200) {
		return fmt.Errorf("%w: clientReference must contain no more than 200 characters", ErrCallbackInvalid)
	}
	if len(e.ProviderMessageID) > 500 {
		return fmt.Errorf("%w: providerMessageId exceeds 500 characters", ErrCallbackInvalid)
	}
	if len(e.ErrorCode) > 128 || len(e.ErrorDetail) > 2048 {
		return fmt.Errorf("%w: error metadata exceeds the permitted length", ErrCallbackInvalid)
	}
	if e.OccurredAt.IsZero() {
		return fmt.Errorf("%w: occurredAt is required", ErrCallbackInvalid)
	}
	if maximumFutureDrift <= 0 {
		maximumFutureDrift = 5 * time.Minute
	}
	if e.OccurredAt.After(now.UTC().Add(maximumFutureDrift)) {
		return fmt.Errorf("%w: occurredAt is unreasonably far in the future", ErrCallbackInvalid)
	}
	switch e.EventType {
	case delivery.EventGatewayAccepted, delivery.EventSent, delivery.EventDelivered, delivery.EventRead:
		if strings.TrimSpace(e.ProviderMessageID) == "" {
			return fmt.Errorf("%w: providerMessageId is required for %s", ErrCallbackInvalid, e.EventType)
		}
	case delivery.EventFailedRetryable, delivery.EventFailedPermanent, delivery.EventUnknown:
		if strings.TrimSpace(e.ErrorCode) == "" {
			return fmt.Errorf("%w: errorCode is required for %s", ErrCallbackInvalid, e.EventType)
		}
	default:
		return fmt.Errorf("%w: unsupported provider event type %q", ErrCallbackInvalid, e.EventType)
	}
	if strings.TrimSpace(e.ClientReference) == "" && strings.TrimSpace(e.ProviderMessageID) == "" {
		return fmt.Errorf("%w: clientReference or providerMessageId is required", ErrCallbackInvalid)
	}
	return nil
}

func (e CallbackEvent) DeliveryEvent() delivery.Event {
	return delivery.Event{
		DeduplicationKey:  "gateway:" + strings.TrimSpace(e.EventID),
		Type:              e.EventType,
		ProviderEventID:   strings.TrimSpace(e.EventID),
		ProviderMessageID: strings.TrimSpace(e.ProviderMessageID),
		ErrorCode:         strings.TrimSpace(e.ErrorCode),
		ErrorDetail:       strings.TrimSpace(e.ErrorDetail),
		OccurredAt:        e.OccurredAt.UTC(),
	}
}

// DecodeCallback rejects unknown fields and trailing JSON so newly introduced
// provider fields cannot be silently ignored by an older control plane.
func DecodeCallback(body []byte) (CallbackEvent, error) {
	var event CallbackEvent
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return CallbackEvent{}, fmt.Errorf("%w: %v", ErrCallbackInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return CallbackEvent{}, fmt.Errorf("%w: trailing JSON content", ErrCallbackInvalid)
	}
	return event, nil
}

// VerifyCallback authenticates the exact raw request body. The timestamp is part
// of the MAC and is constrained to a short acceptance window to limit replay.
func VerifyCallback(secret []byte, timestampHeader, signatureHeader string, body []byte, now time.Time, maximumSkew time.Duration) error {
	if len(secret) < 32 {
		return ErrCallbackSecretRequired
	}
	timestampHeader = strings.TrimSpace(timestampHeader)
	if timestampHeader == "" {
		return ErrTimestampRequired
	}
	unixSeconds, err := strconv.ParseInt(timestampHeader, 10, 64)
	if err != nil || unixSeconds <= 0 {
		return ErrTimestampInvalid
	}
	if maximumSkew <= 0 {
		maximumSkew = 5 * time.Minute
	}
	requestTime := time.Unix(unixSeconds, 0).UTC()
	delta := now.UTC().Sub(requestTime)
	if delta < 0 {
		delta = -delta
	}
	if delta > maximumSkew {
		return ErrTimestampOutsideWindow
	}

	signatureHeader = strings.TrimSpace(signatureHeader)
	if signatureHeader == "" {
		return ErrSignatureRequired
	}
	const prefix = "sha256="
	if !strings.HasPrefix(signatureHeader, prefix) {
		return ErrSignatureInvalid
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signatureHeader, prefix))
	if err != nil || len(provided) != sha256.Size {
		return ErrSignatureInvalid
	}
	expected := callbackMAC(secret, timestampHeader, body)
	if !hmac.Equal(provided, expected) {
		return ErrSignatureInvalid
	}
	return nil
}

func SignCallback(secret []byte, timestamp time.Time, body []byte) (string, string, error) {
	if len(secret) < 32 {
		return "", "", ErrCallbackSecretRequired
	}
	ts := strconv.FormatInt(timestamp.UTC().Unix(), 10)
	return ts, "sha256=" + hex.EncodeToString(callbackMAC(secret, ts, body)), nil
}

func callbackMAC(secret []byte, timestamp string, body []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return mac.Sum(nil)
}

func bounded(value string, minimum, maximum int) bool {
	length := len(strings.TrimSpace(value))
	return length >= minimum && length <= maximum
}
