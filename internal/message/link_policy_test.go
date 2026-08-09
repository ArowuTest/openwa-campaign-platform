package message

import (
	"strings"
	"testing"
	"time"
)

func TestMessageRejectsHTTPSDestinationOutsideConfiguredAllowList(t *testing.T) {
	_, err := NewDraft(Input{
		CampaignID: "campaign-link-policy", Version: 1, Type: TypeText, Body: "See details",
		CreatedBy: "maker", IdempotencyKey: "message-link-policy-0001",
		Links:        []Link{{URL: "https://unapproved.example/details"}},
		AllowedHosts: []string{"approved.example"},
	}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "allow-listed") {
		t.Fatalf("disallowed HTTPS destination was accepted: %v", err)
	}

	if _, err := NewDraft(Input{
		CampaignID: "campaign-link-policy", Version: 1, Type: TypeText, Body: "See details",
		CreatedBy: "maker", IdempotencyKey: "message-link-policy-0002",
		Links:        []Link{{URL: "https://approved.example/details"}},
		AllowedHosts: []string{"approved.example"},
	}, time.Now()); err != nil {
		t.Fatalf("approved HTTPS destination was rejected: %v", err)
	}
}
