package metacloud

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

func TestVerifyWebhookSignatureUsesExactRawBody(t *testing.T) {
	secret := "meta-app-secret-0123456789"
	body := []byte(`{"object":"whatsapp_business_account","entry":[]}`)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if err := VerifyWebhookSignature(secret, signature, body); err != nil {
		t.Fatal(err)
	}
	if err := VerifyWebhookSignature(secret, signature, append(body, ' ')); err == nil {
		t.Fatal("signature accepted modified raw body")
	}
	if err := VerifyWebhookSignature(secret, "sha256=00", body); err == nil {
		t.Fatal("forged signature accepted")
	}
}

func TestVerifyWebhookChallengeRequiresSubscribeAndVerifyToken(t *testing.T) {
	credential := Credential{VerifyToken: "verify-token-012345"}
	challenge, err := VerifyWebhookChallenge(credential, "subscribe", "verify-token-012345", "1158201444")
	if err != nil || challenge != "1158201444" {
		t.Fatalf("challenge=%q err=%v", challenge, err)
	}
	for _, input := range []struct{ mode, token string }{{"other", "verify-token-012345"}, {"subscribe", "wrong-token"}} {
		if _, err := VerifyWebhookChallenge(credential, input.mode, input.token, "1158201444"); err == nil {
			t.Fatalf("invalid challenge accepted: %#v", input)
		}
	}
}

func TestDecodeWebhookNormalisesStatusesAndInboundText(t *testing.T) {
	raw := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-1","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-1"},"statuses":[{"id":"wamid.1","status":"read","timestamp":"1786586405"},{"id":"wamid.1","status":"delivered","timestamp":"1786586404"},{"id":"wamid.2","status":"failed","timestamp":"1786586403","errors":[{"code":131047,"title":"Re-engagement message","message":"outside allowed window"}]}],"messages":[{"from":"2348012345678","id":"wamid.in.1","timestamp":"1786586406","type":"text","context":{"id":"wamid.1"},"text":{"body":"STOP"}}]}}]}]}`)
	notification, err := DecodeWebhook(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(notification.Changes) != 1 {
		t.Fatalf("changes=%#v", notification.Changes)
	}
	change := notification.Changes[0]
	if change.WABAID != "waba-1" || change.PhoneNumberID != "phone-1" || len(change.Statuses) != 3 || len(change.Messages) != 1 {
		t.Fatalf("change=%#v", change)
	}
	if change.Statuses[0].Status != "read" || change.Statuses[1].Status != "delivered" || !change.Statuses[0].OccurredAt.After(change.Statuses[1].OccurredAt) {
		t.Fatalf("status order/times=%#v", change.Statuses)
	}
	if change.Statuses[2].ErrorCode != "131047" || change.Statuses[2].ErrorDetail == "" {
		t.Fatalf("failed status evidence=%#v", change.Statuses[2])
	}
	message := change.Messages[0]
	if message.MessageID != "wamid.in.1" || message.From != "+2348012345678" || message.Text != "STOP" || message.QuotedProviderMessageID != "wamid.1" {
		t.Fatalf("message=%#v", message)
	}
	want := time.Unix(1786586406, 0).UTC()
	if !message.OccurredAt.Equal(want) {
		t.Fatalf("occurred=%s want=%s", message.OccurredAt, want)
	}
}

func TestDecodeWebhookRejectsMalformedMessageEvidence(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"object":"other","entry":[]}`),
		[]byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{},"messages":[{"id":"m","from":"2348012345678","timestamp":"bad","type":"text","text":{"body":"STOP"}}]}}]}]}`),
	} {
		if _, err := DecodeWebhook(raw); err == nil {
			t.Fatalf("malformed webhook accepted: %s", raw)
		}
	}
}

func TestDecodeWebhookRejectsMalformedNestedStatusOrTextEvidence(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"status bad timestamp", `{"object":"whatsapp_business_account","entry":[{"id":"waba","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone"},"statuses":[{"id":"wamid.1","status":"delivered","timestamp":"bad"}]}}]}]}`},
		{"status empty id", `{"object":"whatsapp_business_account","entry":[{"id":"waba","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone"},"statuses":[{"id":"","status":"read","timestamp":"1786586405"}]}}]}]}`},
		{"text bad timestamp", `{"object":"whatsapp_business_account","entry":[{"id":"waba","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone"},"messages":[{"from":"2348012345678","id":"wamid.in.1","timestamp":"bad","type":"text","text":{"body":"hello"}}]}}]}]}`},
		{"text empty id", `{"object":"whatsapp_business_account","entry":[{"id":"waba","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone"},"messages":[{"from":"2348012345678","id":"","timestamp":"1786586406","type":"text","text":{"body":"hello"}}]}}]}]}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeWebhook([]byte(tt.raw)); !errors.Is(err, ErrWebhookInvalid) {
				t.Fatalf("malformed nested evidence error=%v", err)
			}
		})
	}
}

func TestDecodeWebhookIntentionallyIgnoresUnknownStatusAndNonText(t *testing.T) {
	raw := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone"},"statuses":[{"id":"wamid.1","status":"future-status","timestamp":"1786586405"}],"messages":[{"from":"2348012345678","id":"wamid.image.1","timestamp":"1786586406","type":"image"}]}}]}]}`)
	notification, err := DecodeWebhook(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(notification.Changes) != 0 {
		t.Fatalf("unsupported evidence should be ignored: %#v", notification.Changes)
	}
}
