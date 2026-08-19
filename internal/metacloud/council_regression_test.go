package metacloud

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCouncilMetaHTTP408IsOutcomeUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestTimeout)
		_, _ = io.WriteString(w, `{"error":{"message":"request timed out"}}`)
	}))
	defer server.Close()
	client := &Client{Credentials: testCredentials(t), BaseURL: server.URL}
	_, err := client.SendMessage(context.Background(), MessageRequest{CredentialKey: "meta-ng", GraphAPIVersion: "v23.0", PhoneNumberID: "987654321", Payload: map[string]any{"messaging_product": "whatsapp"}})
	var apiErr APIError
	if !errors.As(err, &apiErr) || apiErr.Safety != SafetyOutcomeUnknown {
		t.Fatalf("408 must be outcome-unknown, got %#v err=%v", apiErr, err)
	}
}
func TestCouncilMetaErrorDetailNeverCarriesRawProviderSecrets(t *testing.T) {
	raw := []byte(`{"error":{"message":"request failed access_token=secret-token-123456789","type":"OAuthException","code":190}}`)
	detail := redactedMetaDetail(raw)
	if strings.Contains(detail, "secret-token") || strings.Contains(detail, "access_token") || strings.Contains(detail, "request failed") {
		t.Fatalf("provider body leaked into error detail: %q", detail)
	}
}

func TestCouncilWebhookFailedStatusNeverPersistsRawProviderDetail(t *testing.T) {
	raw := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-1","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-1"},"statuses":[{"id":"wamid.failed","status":"failed","timestamp":"1786586403","errors":[{"code":190,"title":"OAuth access_token=secret-token-123","message":"request failed with customer data"}]}]}}]}]}`)
	notification, err := DecodeWebhook(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(notification.Changes) != 1 || len(notification.Changes[0].Statuses) != 1 {
		t.Fatalf("failed status was not decoded: %#v", notification)
	}
	status := notification.Changes[0].Statuses[0]
	if status.ErrorCode != "190" {
		t.Fatalf("numeric Meta error code was not retained: %#v", status)
	}
	if status.ErrorDetail != "Meta reported delivery failure" || strings.Contains(status.ErrorDetail, "secret-token") || strings.Contains(status.ErrorDetail, "customer data") {
		t.Fatalf("raw provider failure detail leaked into durable evidence: %q", status.ErrorDetail)
	}
}

func TestCouncilWebhookRejectsMalformedItemsEvenWhenAnotherStatusIsValid(t *testing.T) {
	raw := []byte(`{\"object\":\"whatsapp_business_account\",\"entry\":[{\"id\":\"waba-1\",\"changes\":[{\"field\":\"messages\",\"value\":{\"messaging_product\":\"whatsapp\",\"metadata\":{\"phone_number_id\":\"phone-1\"},\"statuses\":[{\"id\":\"wamid.good\",\"status\":\"delivered\",\"timestamp\":\"1786586404\"},{\"id\":\"wamid.bad\",\"status\":\"read\",\"timestamp\":\"bad\"}]}}]}]}`)
	if _, err := DecodeWebhook(raw); !errors.Is(err, ErrWebhookInvalid) {
		t.Fatalf("mixed valid/malformed webhook evidence error=%v", err)
	}
}
