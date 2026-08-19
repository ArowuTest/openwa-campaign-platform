package metacloud

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	sharedcrypto "campaign-platform/internal/shared/crypto"
)

var ErrWebhookInvalid = errors.New("Meta Cloud webhook is invalid")
var ErrWebhookSignature = errors.New("Meta Cloud webhook signature is invalid")
var ErrWebhookChallenge = errors.New("Meta Cloud webhook challenge is invalid")

const WebhookSignatureHeader = "X-Hub-Signature-256"

type WebhookNotification struct {
	Changes []WebhookChange
}

type WebhookChange struct {
	WABAID        string
	PhoneNumberID string
	Statuses      []WebhookStatus
	Messages      []WebhookMessage
}
type WebhookStatus struct {
	ProviderMessageID string
	Status            string
	OccurredAt        time.Time
	ErrorCode         string
	ErrorDetail       string
}

type WebhookMessage struct {
	MessageID               string
	From                    string
	Text                    string
	QuotedProviderMessageID string
	OccurredAt              time.Time
}

func VerifyWebhookSignature(appSecret, signature string, body []byte) error {
	appSecret = strings.TrimSpace(appSecret)
	signature = strings.TrimSpace(signature)
	if len(appSecret) < 16 || !strings.HasPrefix(signature, "sha256=") || len(signature) != len("sha256=")+sha256.Size*2 {
		return ErrWebhookSignature
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil || len(provided) != sha256.Size {
		return ErrWebhookSignature
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	_, _ = mac.Write(body)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return ErrWebhookSignature
	}
	return nil
}

func VerifyWebhookChallenge(credential Credential, mode, token, challenge string) (string, error) {
	mode, token, challenge = strings.TrimSpace(mode), strings.TrimSpace(token), strings.TrimSpace(challenge)
	if mode != "subscribe" || strings.TrimSpace(credential.VerifyToken) == "" || !hmac.Equal([]byte(token), []byte(strings.TrimSpace(credential.VerifyToken))) {
		return "", ErrWebhookChallenge
	}
	if challenge == "" || len(challenge) > 256 {
		return "", ErrWebhookChallenge
	}
	for _, ch := range challenge {
		if ch < '0' || ch > '9' {
			return "", ErrWebhookChallenge
		}
	}
	return challenge, nil
}

type webhookEnvelope struct {
	Object string `json:"object"`
	Entry  []struct {
		ID      string `json:"id"`
		Changes []struct {
			Field string `json:"field"`
			Value struct {
				MessagingProduct string `json:"messaging_product"`
				Metadata         struct {
					PhoneNumberID string `json:"phone_number_id"`
				} `json:"metadata"`
				Statuses []struct {
					ID        string `json:"id"`
					Status    string `json:"status"`
					Timestamp string `json:"timestamp"`
					Errors    []struct {
						Code    int64  `json:"code"`
						Title   string `json:"title"`
						Message string `json:"message"`
					} `json:"errors"`
				} `json:"statuses"`
				Messages []struct {
					From      string `json:"from"`
					ID        string `json:"id"`
					Timestamp string `json:"timestamp"`
					Type      string `json:"type"`
					Context   struct {
						ID string `json:"id"`
					} `json:"context"`
					Text struct {
						Body string `json:"body"`
					} `json:"text"`
				} `json:"messages"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

func DecodeWebhook(raw []byte) (WebhookNotification, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var envelope webhookEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return WebhookNotification{}, ErrWebhookInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return WebhookNotification{}, ErrWebhookInvalid
	}
	if envelope.Object != "whatsapp_business_account" {
		return WebhookNotification{}, ErrWebhookInvalid
	}
	out := WebhookNotification{}
	for _, entry := range envelope.Entry {
		wabaID := strings.TrimSpace(entry.ID)
		for _, change := range entry.Changes {
			if change.Field != "messages" {
				continue
			}
			if change.Value.MessagingProduct != "whatsapp" {
				return WebhookNotification{}, ErrWebhookInvalid
			}
			phoneID := strings.TrimSpace(change.Value.Metadata.PhoneNumberID)
			if phoneID == "" && (len(change.Value.Statuses) > 0 || len(change.Value.Messages) > 0) {
				return WebhookNotification{}, ErrWebhookInvalid
			}
			normalized := WebhookChange{WABAID: wabaID, PhoneNumberID: phoneID}
			for _, status := range change.Value.Statuses {
				value, ok, err := normalizeWebhookStatus(status.ID, status.Status, status.Timestamp, status.Errors)
				if err != nil {
					return WebhookNotification{}, ErrWebhookInvalid
				}
				if ok {
					normalized.Statuses = append(normalized.Statuses, value)
				}
			}
			for _, message := range change.Value.Messages {
				if strings.ToLower(strings.TrimSpace(message.Type)) != "text" {
					continue
				}
				occurred, err := parseWebhookTimestamp(message.Timestamp)
				if err != nil || strings.TrimSpace(message.ID) == "" {
					return WebhookNotification{}, ErrWebhookInvalid
				}
				if strings.TrimSpace(message.Text.Body) == "" {
					continue
				}
				from := strings.TrimSpace(message.From)
				if !strings.HasPrefix(from, "+") {
					from = "+" + from
				}
				e164, err := sharedcrypto.NormalizeE164(from)
				if err != nil {
					continue
				}
				normalized.Messages = append(normalized.Messages, WebhookMessage{
					MessageID: strings.TrimSpace(message.ID), From: e164, Text: strings.TrimSpace(message.Text.Body),
					QuotedProviderMessageID: strings.TrimSpace(message.Context.ID), OccurredAt: occurred,
				})
			}
			if len(normalized.Statuses) > 0 || len(normalized.Messages) > 0 {
				out.Changes = append(out.Changes, normalized)
			}
		}
	}
	return out, nil
}

func normalizeWebhookStatus(id, status, timestamp string, rawErrors []struct {
	Code    int64  `json:"code"`
	Title   string `json:"title"`
	Message string `json:"message"`
}) (WebhookStatus, bool, error) {
	id = strings.TrimSpace(id)
	status = strings.ToLower(strings.TrimSpace(status))
	if id == "" {
		return WebhookStatus{}, false, ErrWebhookInvalid
	}
	occurred, err := parseWebhookTimestamp(timestamp)
	if err != nil {
		return WebhookStatus{}, false, ErrWebhookInvalid
	}
	switch status {
	case "sent", "delivered", "read":
		return WebhookStatus{ProviderMessageID: id, Status: status, OccurredAt: occurred}, true, nil
	case "failed":
		value := WebhookStatus{ProviderMessageID: id, Status: status, OccurredAt: occurred, ErrorCode: "META_DELIVERY_FAILED", ErrorDetail: "Meta reported delivery failure"}
		if len(rawErrors) > 0 {
			first := rawErrors[0]
			if first.Code != 0 {
				value.ErrorCode = strconv.FormatInt(first.Code, 10)
			}
		}
		return value, true, nil
	default:
		return WebhookStatus{}, false, nil
	}
}

func parseWebhookTimestamp(raw string) (time.Time, error) {
	seconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}, ErrWebhookInvalid
	}
	value := time.Unix(seconds, 0).UTC()
	if value.Year() < 2000 || value.Year() > 2200 {
		return time.Time{}, fmt.Errorf("%w: timestamp out of range", ErrWebhookInvalid)
	}
	return value, nil
}
