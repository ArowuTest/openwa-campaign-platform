package metacloud

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testCredentials(t *testing.T) *CredentialSet {
	t.Helper()
	set, err := ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"0123456789abcdef0123456789abcdef","verifyToken":"verify-token-123456"}]`)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestClientSendsBearerMessageToPhoneNumberEndpoint(t *testing.T) {
	var gotAuth, gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"messages":[{"id":"wamid.123"}]}`)
	}))
	defer server.Close()
	client := &Client{Credentials: testCredentials(t), BaseURL: server.URL}
	result, err := client.SendMessage(context.Background(), MessageRequest{CredentialKey: "meta-ng", GraphAPIVersion: "v23.0", PhoneNumberID: "987654321", Payload: map[string]any{"messaging_product": "whatsapp", "to": "2348000000000", "type": "template"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.MessageID != "wamid.123" || gotAuth != "Bearer token-value-abcdefghijklmnopqrstuvwxyz" || gotPath != "/v23.0/987654321/messages" || !strings.Contains(gotBody, `"messaging_product":"whatsapp"`) {
		t.Fatalf("unexpected send result=%#v auth=%q path=%q body=%s", result, gotAuth, gotPath, gotBody)
	}
}

func TestClientRejectsRedirectWithoutForwardingBearer(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("Bearer credential forwarded across redirect")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/captured", http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client := &Client{Credentials: testCredentials(t), BaseURL: source.URL}
	_, err := client.SendMessage(context.Background(), MessageRequest{CredentialKey: "meta-ng", GraphAPIVersion: "v23.0", PhoneNumberID: "987654321", Payload: map[string]any{"messaging_product": "whatsapp"}})
	var apiErr APIError
	if !errors.As(err, &apiErr) || targetHits.Load() != 0 {
		t.Fatalf("redirect handling err=%v targetHits=%d", err, targetHits.Load())
	}
}

func TestClientClassifies429AsRetryableAndAmbiguous5xxAsUnknown(t *testing.T) {
	status := atomic.Int32{}
	status.Store(http.StatusTooManyRequests)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(int(status.Load()))
		_, _ = io.WriteString(w, `{"error":{"message":"provider unavailable"}}`)
	}))
	defer server.Close()
	client := &Client{Credentials: testCredentials(t), BaseURL: server.URL}
	req := MessageRequest{CredentialKey: "meta-ng", GraphAPIVersion: "v23.0", PhoneNumberID: "987654321", Payload: map[string]any{"messaging_product": "whatsapp"}}
	_, err := client.SendMessage(context.Background(), req)
	var apiErr APIError
	if !errors.As(err, &apiErr) || apiErr.Safety != SafetySafeToRetry || apiErr.RetryAfter != 2*time.Second {
		t.Fatalf("429 classification=%#v err=%v", apiErr, err)
	}
	status.Store(http.StatusInternalServerError)
	_, err = client.SendMessage(context.Background(), req)
	if !errors.As(err, &apiErr) || apiErr.Safety != SafetyOutcomeUnknown {
		t.Fatalf("500 classification=%#v err=%v", apiErr, err)
	}
	status.Store(http.StatusServiceUnavailable)
	_, err = client.SendMessage(context.Background(), req)
	if !errors.As(err, &apiErr) || apiErr.Safety != SafetyOutcomeUnknown {
		t.Fatalf("503 classification=%#v err=%v", apiErr, err)
	}
}

func TestClientTreatsTransportFailureAndMalformedSuccessAsUnknown(t *testing.T) {
	client := &Client{Credentials: testCredentials(t), HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })}, BaseURL: "http://127.0.0.1:1"}
	req := MessageRequest{CredentialKey: "meta-ng", GraphAPIVersion: "v23.0", PhoneNumberID: "987654321", Payload: map[string]any{"messaging_product": "whatsapp"}}
	_, err := client.SendMessage(context.Background(), req)
	var apiErr APIError
	if !errors.As(err, &apiErr) || apiErr.Safety != SafetyOutcomeUnknown {
		t.Fatalf("transport classification=%#v err=%v", apiErr, err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"messages":[]}`) }))
	defer server.Close()
	client = &Client{Credentials: testCredentials(t), BaseURL: server.URL}
	_, err = client.SendMessage(context.Background(), req)
	if !errors.As(err, &apiErr) || apiErr.Safety != SafetyOutcomeUnknown {
		t.Fatalf("malformed success classification=%#v err=%v", apiErr, err)
	}
}

func TestClientBoundsProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 2048))
	}))
	defer server.Close()
	client := &Client{Credentials: testCredentials(t), BaseURL: server.URL, MaximumResponseBytes: 128}
	_, err := client.SendMessage(context.Background(), MessageRequest{CredentialKey: "meta-ng", GraphAPIVersion: "v23.0", PhoneNumberID: "987654321", Payload: map[string]any{"messaging_product": "whatsapp"}})
	var apiErr APIError
	if !errors.As(err, &apiErr) || apiErr.Safety != SafetyOutcomeUnknown {
		t.Fatalf("oversized response classification=%#v err=%v", apiErr, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClientVerifySenderMatchesPhoneWithinGovernedWABA(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v23.0/waba-1/phone_numbers" || r.Header.Get("Authorization") == "" {
			t.Fatalf("unexpected verification request: %s auth=%q", r.URL.String(), r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"phone-other","display_phone_number":"+1 555 000 0000","verified_name":"Other","quality_rating":"GREEN"},{"id":"phone-1","display_phone_number":"+234 801 234 5678","verified_name":"Groove","quality_rating":"YELLOW"}]}`)
	}))
	defer server.Close()
	client := &Client{Credentials: testCredentials(t), BaseURL: server.URL}
	result, err := client.VerifySender(context.Background(), Sender{CredentialKey: "meta-ng", GraphAPIVersion: "v23.0", WABAID: "waba-1", PhoneNumberID: "phone-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.PhoneNumberID != "phone-1" || result.VerifiedName != "Groove" || result.QualityRating != "YELLOW" {
		t.Fatalf("verification=%#v", result)
	}
}

func TestClientVerifySenderRejectsWABAPhoneMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"phone-other","display_phone_number":"+1 555 000 0000","verified_name":"Other","quality_rating":"GREEN"}]}`)
	}))
	defer server.Close()
	client := &Client{Credentials: testCredentials(t), BaseURL: server.URL}
	_, err := client.VerifySender(context.Background(), Sender{CredentialKey: "meta-ng", GraphAPIVersion: "v23.0", WABAID: "waba-1", PhoneNumberID: "phone-1"})
	var apiErr APIError
	if !errors.As(err, &apiErr) || apiErr.Safety != SafetyPermanent {
		t.Fatalf("mismatch err=%#v", err)
	}
}
