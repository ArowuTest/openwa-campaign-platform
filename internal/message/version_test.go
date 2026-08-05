package message

import (
	"testing"
	"time"
)

func TestApprovedMessageIsMakerCheckerAndHasStableHash(t *testing.T) {
	input := Input{CampaignID: "c", Version: 1, Type: TypeText, Body: "Hello", CreatedBy: "maker", IdempotencyKey: "message-request-0001", Links: []Link{{URL: "https://example.com/a"}}, AllowedHosts: []string{"example.com"}}
	a, err := NewDraft(input, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewDraft(input, time.Now())
	if err != nil || a.ContentHash != b.ContentHash {
		t.Fatal("hash not stable")
	}
	if _, err := a.Approve("maker", time.Now()); err == nil {
		t.Fatal("self approval accepted")
	}
	if _, err := a.Approve("checker", time.Now()); err != nil {
		t.Fatal(err)
	}
}
func TestMessageRejectsUnsafeLink(t *testing.T) {
	_, err := NewDraft(Input{CampaignID: "c", Version: 1, Type: TypeText, Body: "x", CreatedBy: "u", IdempotencyKey: "message-request-0002", Links: []Link{{URL: "http://evil.test"}}}, time.Now())
	if err == nil {
		t.Fatal("unsafe link accepted")
	}
}

func TestNewDraftRejectsInvalidVariableDefinitions(t *testing.T) {
	base := Input{CampaignID: "campaign", Version: 1, Type: TypeText, Body: "Hello {{first_name}}", CreatedBy: "maker", IdempotencyKey: "message-variable-validation"}
	base.Variables = []Variable{{Name: "first_name", DataType: "TEXT"}, {Name: "first_name", DataType: "TEXT"}}
	if _, err := NewDraft(base, time.Now()); err == nil {
		t.Fatal("duplicate variable accepted")
	}
	base.Variables = []Variable{{Name: "first_name", DataType: "EXECUTABLE"}}
	if _, err := NewDraft(base, time.Now()); err == nil {
		t.Fatal("unsupported variable type accepted")
	}
	base.Variables = []Variable{{Name: "first name", DataType: "TEXT"}}
	if _, err := NewDraft(base, time.Now()); err == nil {
		t.Fatal("invalid variable name accepted")
	}
}

func TestNewDraftRejectsUndeclaredPlaceholders(t *testing.T) {
	_, err := NewDraft(Input{CampaignID: "campaign", Version: 1, Type: TypeText, Body: "Hello {{first_name}}", CreatedBy: "maker", IdempotencyKey: "message-undeclared-variable"}, time.Now())
	if err == nil {
		t.Fatal("undeclared placeholder accepted")
	}
}
