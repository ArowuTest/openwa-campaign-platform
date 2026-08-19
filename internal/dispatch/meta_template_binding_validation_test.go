package dispatch

import (
	"strings"
	"testing"

	"campaign-platform/internal/metacloud"
	"campaign-platform/internal/provider"
)

func TestValidateMetaTemplateEvidenceRejectsTypedMediaHeaderForTextMessage(t *testing.T) {
	hash := strings.Repeat("a", 64)
	evidence := metaMaterialEvidence{
		DefinitionCapabilities:   []string{string(provider.CapabilitySendTemplate)},
		TemplateStatus:           "APPROVED",
		BindingComponentHash:     hash,
		TemplateComponentHash:    hash,
		BindingTemplateName:      "approved_template",
		BindingLanguage:          "en",
		BindingMediaHeaderType:   "",
		BindingComponentBindings: []metacloud.ComponentBinding{{Type: "HEADER", SubType: "IMAGE", Index: 0}},
	}
	if err := validateMetaTemplateEvidence(evidence, "text"); err == nil {
		t.Fatal("typed media header was accepted for canonical text message")
	}
}
