package message

import (
	"testing"
	"time"
)

func TestNewDraftCanonicalisesVariableAndLinkIdentityBeforeHash(t *testing.T) {
	now := time.Date(2026, 8, 17, 2, 30, 0, 0, time.UTC)
	base := Input{
		CampaignID: "campaign-1", Version: 1, Type: TypeText, Body: "hello {{first_name}}",
		CreatedBy: "creator", IdempotencyKey: "canonical-message-0001", AllowedHosts: []string{"example.com"},
		Variables: []Variable{{Name: "first_name", DataType: "TEXT", Fallback: "friend"}},
		Links:     []Link{{URL: "https://example.com/path", Label: "Open"}},
	}
	canonical, err := NewDraft(base, now)
	if err != nil {
		t.Fatal(err)
	}
	variant := base
	variant.IdempotencyKey = "canonical-message-0002"
	variant.Variables = []Variable{{Name: "  first_name ", DataType: " text ", Fallback: "friend"}}
	variant.Links = []Link{{URL: " https://example.com/path ", Label: "Open"}}
	padded, err := NewDraft(variant, now)
	if err != nil {
		t.Fatal(err)
	}
	if padded.ContentHash != canonical.ContentHash {
		t.Fatalf("logical message identity hashed differently: canonical=%s padded=%s", canonical.ContentHash, padded.ContentHash)
	}
	if padded.Variables[0].Name != "first_name" || padded.Variables[0].DataType != "TEXT" || padded.Links[0].URL != "https://example.com/path" {
		t.Fatalf("draft persisted non-canonical message identity: %#v %#v", padded.Variables, padded.Links)
	}
}
