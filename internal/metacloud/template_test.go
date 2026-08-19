package metacloud

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/message"
)

func TestDecodeTemplatePageCanonicalHashAndApprovedStatus(t *testing.T) {
	raw := []byte(`{"data":[{"id":"tpl-1","name":"promo_offer","language":"en_US","category":"MARKETING","status":"APPROVED","quality_score":{"score":"GREEN"},"components":[{"type":"BODY","text":"Hello {{1}}, your code is {{2}}"},{"type":"HEADER","format":"IMAGE"}]}]}`)
	page, err := DecodeTemplatePage(raw, "org-1", "waba-1", time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || !page[0].Sendable() || len(page[0].ComponentHash) != 64 {
		t.Fatalf("unexpected template page: %#v", page)
	}
	var components []map[string]any
	if err := json.Unmarshal(page[0].Components, &components); err != nil || len(components) != 2 {
		t.Fatalf("components not retained: %v %#v", err, components)
	}
}
func TestBuildBindingRequiresApprovedMessageAndCompatibleTemplate(t *testing.T) {
	approvedAt := time.Date(2026, 8, 12, 3, 5, 0, 0, time.UTC)
	version := message.Version{ID: "message-1", Status: message.StatusApproved, Type: message.TypeImageCaption,
		Variables: []message.Variable{{Name: "first_name"}, {Name: "code"}}, ApprovedAt: &approvedAt}
	tpl := Template{Name: "promo_offer", Language: "en_US", Status: "APPROVED", ComponentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Components: json.RawMessage(`[{"type":"HEADER","format":"IMAGE"},{"type":"BODY","text":"Hello {{1}}, code {{2}}"}]`)}
	binding, err := BuildBinding(version, tpl, []string{"first_name", "code"}, "IMAGE", "actor-1", approvedAt)
	if err != nil {
		t.Fatal(err)
	}
	if binding.TemplateName != tpl.Name || binding.MediaHeaderType != "IMAGE" || len(binding.BodyVariableNames) != 2 {
		t.Fatalf("unexpected binding: %#v", binding)
	}
	version.Status = message.StatusDraft
	if _, err = BuildBinding(version, tpl, []string{"first_name", "code"}, "IMAGE", "actor-1", approvedAt); !errors.Is(err, ErrTemplateBindingInvalid) {
		t.Fatalf("draft message binding error=%v", err)
	}
}
func TestBuildBindingRejectsVariableAndMediaMismatch(t *testing.T) {
	now := time.Date(2026, 8, 12, 3, 10, 0, 0, time.UTC)
	version := message.Version{ID: "message-2", Status: message.StatusApproved, Type: message.TypeImageCaption,
		Variables: []message.Variable{{Name: "first_name"}, {Name: "code"}}}
	tpl := Template{Name: "promo_offer", Language: "en_US", Status: "APPROVED", ComponentHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Components: json.RawMessage(`[{"type":"HEADER","format":"IMAGE"},{"type":"BODY","text":"Hello {{1}}, code {{2}}"}]`)}
	for _, input := range []struct {
		names  []string
		header string
	}{{[]string{"first_name"}, "IMAGE"}, {[]string{"first_name", "unknown"}, "IMAGE"}, {[]string{"first_name", "code"}, "VIDEO"}} {
		if _, err := BuildBinding(version, tpl, input.names, input.header, "actor-1", now); !errors.Is(err, ErrTemplateBindingInvalid) {
			t.Fatalf("names=%v header=%s error=%v", input.names, input.header, err)
		}
	}
}

func approvedTextVersion() message.Version {
	return message.Version{ID: "message-approved", Status: message.StatusApproved, Type: message.TypeText,
		Variables: []message.Variable{{Name: "first_name"}, {Name: "order_id"}}}
}

func hashEmptyComponents(t *testing.T) string {
	t.Helper()
	value, err := CanonicalComponentHash([]byte(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCanonicalizeBindingAcceptsTypedComponentsAndDerivesLegacyProjection(t *testing.T) {
	now := time.Date(2026, 8, 13, 1, 45, 0, 0, time.UTC)
	version := message.Version{ID: "message-component", Status: message.StatusApproved, Type: message.TypeImageCaption,
		Variables: []message.Variable{{Name: "first_name"}, {Name: "code"}}}
	tpl := Template{Name: "promo_component", Language: "en_US", Status: "APPROVED",
		ComponentHash: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Components:    json.RawMessage(`[{"type":"HEADER","format":"IMAGE"},{"type":"BODY","text":"Hello {{1}}, code {{2}}"}]`)}
	binding := Binding{MessageVersionID: version.ID, TemplateName: tpl.Name, Language: tpl.Language,
		TemplateComponentHash: tpl.ComponentHash, CreatedBy: "checker", CreatedAt: now,
		ComponentBindings: []ComponentBinding{
			{Type: "BODY", SubType: "TEXT", ParameterNames: []string{"first_name", "code"}},
			{Type: "HEADER", SubType: "IMAGE"},
		}}
	normalized, err := CanonicalizeBinding(version, tpl, binding)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.MediaHeaderType != "IMAGE" || len(normalized.BodyVariableNames) != 2 || len(normalized.ComponentBindings) != 2 {
		t.Fatalf("unexpected canonical binding: %#v", normalized)
	}
}

func TestTemplateJSONExposesComponentsAsStructuredJSON(t *testing.T) {
	encoded, err := json.Marshal(Template{
		Name: "hello_name", Language: "en_US", Components: json.RawMessage(`[{"type":"BODY","text":"Hello {{1}}"}]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["components"].([]any); !ok {
		t.Fatalf("Meta template components must be structured JSON, got %T: %s", decoded["components"], encoded)
	}
}
