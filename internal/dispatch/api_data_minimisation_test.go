package dispatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGatewayDeliveryPayloadIsTransportMinimised(t *testing.T) {
	now := time.Date(2026, 8, 8, 22, 0, 0, 0, time.UTC)
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":true,"providerMessageId":"provider-min-1","acceptedAt":"2026-08-08T22:00:00Z"}`))
	}))
	defer server.Close()

	request := validGatewayRequest(now)
	request.ClientReference = "recipient-1"
	request.MediaURL = "https://media.internal/object"
	gateway := &HTTPGateway{BaseURL: server.URL, CommandSecret: testGatewaySecret, Client: server.Client(), Clock: func() time.Time { return now }, Nonce: func() (string, error) { return "minimise-nonce-1234567890", nil }}
	if _, err := gateway.Send(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"idempotencyKey": true, "provider": true, "engine": true,
		"gatewayPoolId": true, "gatewayPoolVersion": true, "gatewayAdapterVersion": true,
		"gatewayNodeId": true, "gatewayNodeVersion": true, "sessionId": true,
		"sessionLeaseVersion": true, "sessionConfigurationVersion": true, "authorityExpiresAt": true,
		"routeReference": true, "recipientMsisdn": true, "messageType": true, "body": true,
		"mediaUrl": true, "clientReference": true,
	}
	for key := range captured {
		if !allowed[key] {
			t.Fatalf("gateway delivery payload contains non-transport field %q: %#v", key, captured[key])
		}
	}
	for _, forbidden := range []string{
		"consent", "consentGrant", "consentEvidence", "suppression", "reportedAge",
		"gender", "country", "state", "lga", "preferredLanguage", "attributes",
	} {
		if _, exists := captured[forbidden]; exists {
			t.Fatalf("gateway delivery payload leaked %s", forbidden)
		}
	}
	if captured["recipientMsisdn"] != request.RecipientE164 || captured["clientReference"] != request.ClientReference {
		t.Fatalf("required transport correlation fields missing: %#v", captured)
	}
}
