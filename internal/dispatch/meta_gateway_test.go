package dispatch

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/metacloud"
)

type metaClientStub struct {
	request metacloud.MessageRequest
	result  metacloud.MessageResult
	err     error
	calls   int
}

func (m *metaClientStub) SendMessage(_ context.Context, request metacloud.MessageRequest) (metacloud.MessageResult, error) {
	m.calls++
	m.request = request
	return m.result, m.err
}

func validMetaGatewayRequest() GatewayRequest {
	return GatewayRequest{IdempotencyKey: "meta-idem-1234567890", Provider: "META", Engine: "CLOUD_API",
		MetaSenderID: "meta-sender-1", MetaSenderVersion: 4, MetaCredentialKey: "meta-ng", MetaGraphAPIVersion: "v23.0",
		MetaPhoneNumberID: "123456789", MetaRepresentation: MetaRepresentationTemplate,
		RecipientE164: "+2348012345678", MessageType: "image",
		MediaURL: "https://media.example.test/image.jpg", MetaTemplateName: "promo_offer", MetaTemplateLanguage: "en_US",
		MetaBodyParameters: []string{"Ada", "ABC123"}, ClientReference: "recipient-1"}
}
func TestMetaGatewayBuildsTemplatePayloadAndReturnsAcceptedMessage(t *testing.T) {
	client := &metaClientStub{result: metacloud.MessageResult{MessageID: "wamid.1"}}
	gateway := &MetaGateway{Client: client, Clock: func() time.Time { return time.Date(2026, 8, 12, 4, 0, 0, 0, time.UTC) }}
	result, err := gateway.Send(context.Background(), validMetaGatewayRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted || result.ProviderMessageID != "wamid.1" || client.calls != 1 {
		t.Fatalf("result=%#v calls=%d", result, client.calls)
	}
	payload, ok := client.request.Payload.(map[string]any)
	if !ok || payload["type"] != "template" || payload["to"] != "2348012345678" {
		t.Fatalf("unexpected Meta payload: %#v", client.request.Payload)
	}
	if client.request.CredentialKey != "meta-ng" || client.request.PhoneNumberID != "123456789" {
		t.Fatalf("unexpected Meta request: %#v", client.request)
	}
}

func TestMetaGatewayMapsProviderSafetyWithoutCrossTransportRetry(t *testing.T) {
	client := &metaClientStub{err: metacloud.APIError{Code: "META_HTTP_429", Safety: metacloud.SafetySafeToRetry, RetryAfter: 2 * time.Second}}
	_, err := (&MetaGateway{Client: client}).Send(context.Background(), validMetaGatewayRequest())
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Safety != FailureSafeToRetry || gatewayErr.RetryAfter != 2*time.Second {
		t.Fatalf("retry mapping=%#v err=%v", gatewayErr, err)
	}
	client.err = metacloud.APIError{Code: "META_TRANSPORT_UNKNOWN", Safety: metacloud.SafetyOutcomeUnknown}
	_, err = (&MetaGateway{Client: client}).Send(context.Background(), validMetaGatewayRequest())
	if !errors.As(err, &gatewayErr) || gatewayErr.Safety != FailureOutcomeUnknown {
		t.Fatalf("unknown mapping=%#v err=%v", gatewayErr, err)
	}
}

func TestMetaGatewayRendersTypedTemplateComponents(t *testing.T) {
	client := &metaClientStub{result: metacloud.MessageResult{MessageID: "wamid.typed"}}
	request := validMetaGatewayRequest()
	request.MetaComponents = []MetaTemplateComponent{
		{Type: "HEADER", SubType: "IMAGE", Parameters: []MetaTemplateParameter{{Type: "IMAGE", Link: "https://media.example.test/image.jpg"}}},
		{Type: "BODY", SubType: "TEXT", Parameters: []MetaTemplateParameter{{Type: "TEXT", Text: "Ada"}, {Type: "TEXT", Text: "ABC123"}}},
	}
	request.MetaBodyParameters = nil
	request.MediaURL = ""
	if _, err := (&MetaGateway{Client: client}).Send(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	payload, ok := client.request.Payload.(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload type: %#v", client.request.Payload)
	}
	template, ok := payload["template"].(map[string]any)
	if !ok {
		t.Fatalf("template payload missing: %#v", payload)
	}
	components, ok := template["components"].([]any)
	if !ok || len(components) != 2 {
		t.Fatalf("typed components not rendered: %#v", template["components"])
	}
}

func TestMetaGatewayBuildsFreeFormTextPayload(t *testing.T) {
	now := time.Date(2026, 8, 15, 14, 0, 0, 0, time.UTC)
	client := &metaClientStub{result: metacloud.MessageResult{MessageID: "wamid.free.text"}}
	request := validMetaGatewayRequest()
	request.MetaRepresentation = MetaRepresentationFreeForm
	request.MetaFreeFormEligibleUntil = now.Add(time.Hour)
	request.MessageType = "text"
	request.Body = "Hello Ada"
	request.MediaURL = ""
	request.MetaTemplateName = ""
	request.MetaTemplateLanguage = ""
	request.MetaBodyParameters = nil
	request.MetaComponents = nil
	if _, err := (&MetaGateway{Client: client, Clock: func() time.Time { return now }}).Send(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	payload, ok := client.request.Payload.(map[string]any)
	if !ok || payload["type"] != "text" {
		t.Fatalf("unexpected free-form text payload: %#v", client.request.Payload)
	}
	text, ok := payload["text"].(map[string]any)
	if !ok || text["body"] != "Hello Ada" {
		t.Fatalf("unexpected free-form text body: %#v", payload["text"])
	}
	if _, exists := payload["template"]; exists {
		t.Fatalf("free-form payload contained template representation: %#v", payload)
	}
}

func TestMetaGatewayBuildsFreeFormMediaPayload(t *testing.T) {
	now := time.Date(2026, 8, 15, 14, 0, 0, 0, time.UTC)
	client := &metaClientStub{result: metacloud.MessageResult{MessageID: "wamid.free.image"}}
	request := validMetaGatewayRequest()
	request.MetaRepresentation = MetaRepresentationFreeForm
	request.MetaFreeFormEligibleUntil = now.Add(time.Hour)
	request.Body = "Campaign image caption"
	request.MetaTemplateName = ""
	request.MetaTemplateLanguage = ""
	request.MetaBodyParameters = nil
	request.MetaComponents = nil
	if _, err := (&MetaGateway{Client: client, Clock: func() time.Time { return now }}).Send(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	payload, ok := client.request.Payload.(map[string]any)
	if !ok || payload["type"] != "image" {
		t.Fatalf("unexpected free-form image payload: %#v", client.request.Payload)
	}
	image, ok := payload["image"].(map[string]any)
	if !ok || image["link"] != "https://media.example.test/image.jpg" || image["caption"] != "Campaign image caption" {
		t.Fatalf("unexpected free-form image body: %#v", payload["image"])
	}
}

func TestMetaGatewayRejectsRepresentationMixing(t *testing.T) {
	client := &metaClientStub{result: metacloud.MessageResult{MessageID: "wamid.should-not-send"}}
	request := validMetaGatewayRequest()
	request.MetaRepresentation = MetaRepresentationFreeForm
	if _, err := (&MetaGateway{Client: client}).Send(context.Background(), request); err == nil {
		t.Fatal("free-form request carrying template evidence was accepted")
	}
	if client.calls != 0 {
		t.Fatalf("invalid mixed representation reached Meta client %d times", client.calls)
	}
	request = validMetaGatewayRequest()
	request.MetaRepresentation = ""
	if _, err := (&MetaGateway{Client: client}).Send(context.Background(), request); err == nil {
		t.Fatal("Meta request without explicit representation was accepted")
	}
}

func TestMetaPreflightDoesNotReapplyTwentyFourHourPolicyFromGatewayClock(t *testing.T) {
	materialNow := time.Date(2026, 8, 15, 15, 0, 0, 0, time.UTC)
	request := validMetaGatewayRequest()
	request.MetaRepresentation = MetaRepresentationFreeForm
	request.MetaTemplateName = ""
	request.MetaTemplateLanguage = ""
	request.MetaBodyParameters = nil
	request.MetaComponents = nil
	request.MessageType = "text"
	request.Body = "authorised free form"
	request.MediaURL = ""
	request.MetaFreeFormEligibleUntil = materialNow.Add(24 * time.Hour)
	gateway := &MetaGateway{Client: &metaClientStub{}, Clock: func() time.Time { return materialNow.Add(-time.Second) }}
	if err := gateway.Preflight(context.Background(), request); err != nil {
		t.Fatalf("preflight reapplied a second 24h-from-now policy to already-clamped material: %v", err)
	}
}

func TestMetaGatewayRejectsAnyOpenWAAuthorityEvidence(t *testing.T) {
	base := validMetaGatewayRequest()
	tests := map[string]func(*GatewayRequest){
		"gateway pool version":          func(v *GatewayRequest) { v.GatewayPoolVersion = 1 },
		"gateway adapter version":       func(v *GatewayRequest) { v.GatewayAdapterVersion = "0.13.0" },
		"gateway node version":          func(v *GatewayRequest) { v.GatewayNodeVersion = 1 },
		"session lease version":         func(v *GatewayRequest) { v.SessionLeaseVersion = 1 },
		"session configuration version": func(v *GatewayRequest) { v.SessionConfigurationVersion = 1 },
		"authority expiry":              func(v *GatewayRequest) { v.AuthorityExpiresAt = time.Now().UTC().Add(time.Minute) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			request := base
			mutate(&request)
			client := &metaClientStub{result: metacloud.MessageResult{MessageID: "must-not-send"}}
			_, err := (&MetaGateway{Client: client}).Send(context.Background(), request)
			if err == nil || client.calls != 0 {
				t.Fatalf("Meta gateway accepted %s: err=%v calls=%d", name, err, client.calls)
			}
		})
	}
}
