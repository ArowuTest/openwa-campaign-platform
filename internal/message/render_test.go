package message

import (
	"strings"
	"testing"
)

func TestRenderUsesValuesFallbacksAndMasksSensitiveData(t *testing.T) {
	v := Version{Status: StatusApproved, Body: "Hello {{first_name}}, ref {{reference}}, call {{msisdn}}", Variables: []Variable{{Name: "first_name", DataType: "TEXT", Fallback: "Customer"}, {Name: "reference", DataType: "TEXT"}, {Name: "msisdn", DataType: "MSISDN"}}}
	result, err := Render(v, RenderInput{Mode: RenderPreview, MaskSensitive: true, Values: map[string]string{"reference": "ABC-123", "msisdn": "+2348012345678"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Body, "Customer") || !strings.Contains(result.Body, "ABC-123") {
		t.Fatalf("unexpected body %q", result.Body)
	}
	if strings.Contains(result.Body, "+2348012345678") {
		t.Fatal("sensitive value was not masked")
	}
	if len(result.UsedFallbacks) != 1 || result.UsedFallbacks[0] != "first_name" {
		t.Fatalf("unexpected fallbacks %#v", result.UsedFallbacks)
	}
}

func TestRenderDispatchFailsMissingRequiredValue(t *testing.T) {
	v := Version{Status: StatusApproved, Body: "Hello {{first_name}}", Variables: []Variable{{Name: "first_name", DataType: "TEXT"}}}
	result, err := Render(v, RenderInput{Mode: RenderDispatch})
	if err == nil || len(result.Missing) != 1 {
		t.Fatalf("expected missing variable error, got %#v %v", result, err)
	}
}

func TestRenderRejectsUndeclaredPlaceholder(t *testing.T) {
	v := Version{Status: StatusDraft, Body: "Hello {{unknown}}"}
	result, err := Render(v, RenderInput{Mode: RenderPreview})
	if err == nil || len(result.Undeclared) != 1 {
		t.Fatalf("expected undeclared error, got %#v %v", result, err)
	}
}
