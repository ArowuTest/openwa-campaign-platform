package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// InboundMessageEvent is the signed internal callback contract for a reply
// received by a managed sender session. The control plane uses clientReference
// to resolve the authoritative campaign-recipient/contact relationship and does
// not trust a caller-supplied contact identity.
type InboundMessageEvent struct {
	SchemaVersion           string    `json:"schemaVersion"`
	EventID                 string    `json:"eventId"`
	SessionID               string    `json:"sessionId"`
	ClientReference         string    `json:"clientReference,omitempty"`
	ProviderMessageID       string    `json:"providerMessageId,omitempty"`
	QuotedProviderMessageID string    `json:"quotedProviderMessageId,omitempty"`
	SenderMSISDN            string    `json:"senderMsisdn,omitempty"`
	MessageText             string    `json:"messageText"`
	OccurredAt              time.Time `json:"occurredAt"`
}

func (e InboundMessageEvent) Validate(now time.Time, maximumFutureDrift time.Duration) error {
	if e.SchemaVersion != "1.0" {
		return fmt.Errorf("%w: unsupported schemaVersion %q", ErrCallbackInvalid, e.SchemaVersion)
	}
	if !bounded(e.EventID, 1, 200) || !bounded(e.SessionID, 1, 200) {
		return fmt.Errorf("%w: eventId and sessionId are required", ErrCallbackInvalid)
	}
	if strings.TrimSpace(e.ClientReference) == "" && strings.TrimSpace(e.QuotedProviderMessageID) == "" && strings.TrimSpace(e.SenderMSISDN) == "" {
		return fmt.Errorf("%w: a recipient correlation reference is required", ErrCallbackInvalid)
	}
	if e.ClientReference != "" && !bounded(e.ClientReference, 1, 200) {
		return fmt.Errorf("%w: clientReference is invalid", ErrCallbackInvalid)
	}
	if len(e.ProviderMessageID) > 500 || len(e.QuotedProviderMessageID) > 500 || len(e.MessageText) > 4096 {
		return fmt.Errorf("%w: inbound message metadata exceeds the permitted length", ErrCallbackInvalid)
	}
	if e.SenderMSISDN != "" && !validE164(e.SenderMSISDN) {
		return fmt.Errorf("%w: senderMsisdn must be E.164", ErrCallbackInvalid)
	}
	if strings.TrimSpace(e.MessageText) == "" {
		return fmt.Errorf("%w: messageText is required", ErrCallbackInvalid)
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
	return nil
}

func DecodeInboundMessage(body []byte) (InboundMessageEvent, error) {
	var event InboundMessageEvent
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return InboundMessageEvent{}, fmt.Errorf("%w: %v", ErrCallbackInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return InboundMessageEvent{}, fmt.Errorf("%w: trailing JSON content", ErrCallbackInvalid)
	}
	return event, nil
}

func validE164(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 9 || len(value) > 16 || value[0] != '+' || value[1] == '0' {
		return false
	}
	for _, r := range value[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
