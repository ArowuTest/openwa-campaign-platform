package message

import (
	"strings"
	"testing"
	"time"
)

func TestMessageLinksFailClosedWithoutDestinationAllowList(t *testing.T) {
	_, err := NewDraft(Input{
		CampaignID: "campaign-link-policy", Version: 1, Type: TypeText, Body: "See details",
		CreatedBy: "maker", IdempotencyKey: "message-link-policy-r13-empty",
		Links: []Link{{URL: "https://unconfigured.example/details"}},
	}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "allow-list") {
		t.Fatalf("HTTPS destination was accepted without an allow-list: %v", err)
	}
}
