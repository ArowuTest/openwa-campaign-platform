package message

import (
	"testing"
	"time"
)

func TestMessageMakerCheckerCanonicalizesActorIdentity(t *testing.T) {
	now := time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC)
	v, err := NewDraft(Input{CampaignID: "campaign", Version: 1, Type: TypeText, Body: "hello", CreatedBy: " maker ", IdempotencyKey: "message-maker-checker-r13"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if v.CreatedBy != "maker" {
		t.Fatalf("creator identity not canonical: %q", v.CreatedBy)
	}
	for _, actor := range []string{"maker", " maker", "maker ", "\tmaker"} {
		if _, err := v.Approve(actor, now.Add(time.Minute)); err == nil {
			t.Fatalf("self approval accepted for %q", actor)
		}
	}
	approved, err := v.Approve(" checker ", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if approved.ApprovedBy != "checker" {
		t.Fatalf("approver identity not canonical: %q", approved.ApprovedBy)
	}
}

func TestMessageContentHashUsesTotalLinkOrder(t *testing.T) {
	base := Input{CampaignID: "campaign", Version: 1, Type: TypeText, Body: "hello", CreatedBy: "maker", AllowedHosts: []string{"example.com"}}
	a := base
	a.IdempotencyKey = "message-link-order-r13-a"
	a.Links = []Link{{URL: "https://example.com/a", Label: "A"}, {URL: "https://example.com/a", Label: "B"}}
	b := base
	b.IdempotencyKey = "message-link-order-r13-b"
	b.Links = []Link{{URL: "https://example.com/a", Label: "B"}, {URL: "https://example.com/a", Label: "A"}}
	va, err := NewDraft(a, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	vb, err := NewDraft(b, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if va.ContentHash != vb.ContentHash {
		t.Fatalf("same logical link set hashed differently: %s != %s", va.ContentHash, vb.ContentHash)
	}
}
