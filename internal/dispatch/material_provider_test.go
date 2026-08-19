package dispatch

import (
	"testing"
	"time"
)

func TestValidateMaterialAllowsMetaWithoutBrowserSessionAuthority(t *testing.T) {
	material := Material{SenderPoolID: "pool-meta", Provider: "META", Engine: "CLOUD_API",
		MetaSenderID: "meta-sender-1", MetaSenderVersion: 4, MetaCredentialKey: "meta-ng", MetaGraphAPIVersion: "v23.0",
		MetaPhoneNumberID: "123456789", MetaRepresentation: MetaRepresentationTemplate, MetaTemplateName: "promo_offer", MetaTemplateLanguage: "en_US",
		MetaBodyParameters: []string{"Ada"}, RouteReference: "campaign-1:recipient-1", RecipientE164: "+2348012345678",
		MessageType: "text", Body: "Hello Ada"}
	if err := validateMaterial(material); err != nil {
		t.Fatalf("Meta material rejected: %v", err)
	}
}

func TestValidateMaterialKeepsOpenWAFencedSessionRequirements(t *testing.T) {
	material := Material{Provider: "OPENWA", Engine: "BAILEYS", GatewayPoolID: "gw-1", GatewayPoolVersion: 1,
		GatewayAdapterVersion: "0.13.0", RouteReference: "campaign-1:recipient-1", RecipientE164: "+2348012345678",
		MessageType: "text", Body: "hello"}
	if err := validateMaterial(material); err == nil {
		t.Fatal("OpenWA material without node/session fencing was accepted")
	}
	material.GatewayNodeID, material.GatewayNodeVersion = "node-1", 1
	material.SessionID, material.SessionLeaseVersion, material.SessionConfigurationVersion = "session-1", 1, 1
	material.AuthorityExpiresAt = time.Now().UTC().Add(time.Minute)
	if err := validateMaterial(material); err != nil {
		t.Fatalf("governed OpenWA material rejected: %v", err)
	}
}

func TestValidateMaterialAllowsTemplateTextWithoutCanonicalBody(t *testing.T) {
	material := Material{
		SenderPoolID: "pool-meta", Provider: "META", Engine: "CLOUD_API",
		MetaSenderID: "meta-sender-1", MetaSenderVersion: 4, MetaCredentialKey: "meta-ng", MetaGraphAPIVersion: "v23.0",
		MetaPhoneNumberID: "123456789", MetaRepresentation: MetaRepresentationTemplate,
		MetaTemplateName: "promo_offer", MetaTemplateLanguage: "en_US",
		RouteReference: "campaign-1:recipient-1", RecipientE164: "+2348012345678", MessageType: "text",
	}
	if err := validateMaterial(material); err != nil {
		t.Fatalf("Meta template text without canonical body rejected: %v", err)
	}
	material.MetaRepresentation = MetaRepresentationFreeForm
	material.MetaTemplateName, material.MetaTemplateLanguage = "", ""
	material.MetaFreeFormEligibleUntil = time.Now().UTC().Add(time.Hour)
	if err := validateMaterial(material); err == nil {
		t.Fatal("Meta free-form text without canonical body was accepted")
	}
}

func TestValidateMaterialRejectsOpenWAMaterialCarryingMetaAuthority(t *testing.T) {
	now := time.Now().UTC()
	material := Material{Provider: "OPENWA", Engine: "BAILEYS", GatewayPoolID: "gw-1", GatewayPoolVersion: 1,
		GatewayAdapterVersion: "0.13.0", GatewayNodeID: "node-1", GatewayNodeVersion: 1,
		SessionID: "session-1", SessionLeaseVersion: 1, SessionConfigurationVersion: 1, AuthorityExpiresAt: now.Add(time.Minute),
		RouteReference: "campaign-1:recipient-1", RecipientE164: "+2348012345678", MessageType: "text", Body: "hello",
		MetaSenderID: "meta-sender-should-not-flow", MetaSenderVersion: 1, MetaCredentialKey: "meta-secret-ref", MetaGraphAPIVersion: "v23.0", MetaPhoneNumberID: "phone-1",
	}
	if err := validateMaterial(material); err == nil {
		t.Fatal("OpenWA material carrying Meta sender/credential authority was accepted")
	}
}

func TestValidateMaterialAllowsTypedMetaTemplateMediaWithoutTopLevelURL(t *testing.T) {
	material := Material{
		SenderPoolID: "pool-meta", Provider: "META", Engine: "CLOUD_API",
		MetaSenderID: "meta-sender-1", MetaSenderVersion: 4, MetaCredentialKey: "meta-ng", MetaGraphAPIVersion: "v23.0", MetaPhoneNumberID: "123456789",
		MetaRepresentation: MetaRepresentationTemplate, MetaTemplateName: "promo_media", MetaTemplateLanguage: "en_US",
		MetaComponents: []MetaTemplateComponent{{Type: "HEADER", SubType: "IMAGE", Parameters: []MetaTemplateParameter{{Type: "IMAGE", Link: "https://media.example.test/image.jpg"}}}},
		RouteReference: "campaign-1:recipient-1", RecipientE164: "+2348012345678", MessageType: "image",
	}
	if err := validateMaterial(material); err != nil {
		t.Fatalf("typed Meta template media was rejected before gateway validation: %v", err)
	}
}

func TestValidateMaterialRejectsMetaMaterialCarryingAnyOpenWAAuthority(t *testing.T) {
	base := Material{
		SenderPoolID: "pool-meta", Provider: "META", Engine: "CLOUD_API",
		MetaSenderID: "meta-sender-1", MetaSenderVersion: 4, MetaCredentialKey: "meta-ng", MetaGraphAPIVersion: "v23.0", MetaPhoneNumberID: "123456789",
		MetaRepresentation: MetaRepresentationTemplate, MetaTemplateName: "promo_offer", MetaTemplateLanguage: "en_US",
		RouteReference: "campaign-1:recipient-1", RecipientE164: "+2348012345678", MessageType: "text",
	}
	tests := map[string]func(*Material){
		"gateway pool version":          func(v *Material) { v.GatewayPoolVersion = 1 },
		"gateway adapter version":       func(v *Material) { v.GatewayAdapterVersion = "0.13.0" },
		"gateway node version":          func(v *Material) { v.GatewayNodeVersion = 1 },
		"session lease version":         func(v *Material) { v.SessionLeaseVersion = 1 },
		"session configuration version": func(v *Material) { v.SessionConfigurationVersion = 1 },
		"authority expiry":              func(v *Material) { v.AuthorityExpiresAt = time.Now().UTC().Add(time.Minute) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			material := base
			mutate(&material)
			if err := validateMaterial(material); err == nil {
				t.Fatalf("Meta material carrying %s was accepted", name)
			}
		})
	}
}
