package message

import (
	"testing"
	"time"
)

func TestNewDraftAcceptsEverySupportedMessageType(t *testing.T) {
	cases := []struct {
		name      string
		typeValue Type
		media     *Media
	}{
		{"text", TypeText, nil},
		{"image-caption", TypeImageCaption, &Media{ObjectKey: "images/one.png", SHA256: "sha-image", MediaType: "image/png", Size: 100, ScanStatus: "CLEAN"}},
		{"video", TypeVideo, &Media{ObjectKey: "video/one.mp4", SHA256: "sha-video", MediaType: "video/mp4", Size: 200, ScanStatus: "CLEAN"}},
		{"document", TypeDocument, &Media{ObjectKey: "docs/one.pdf", SHA256: "sha-doc", MediaType: "application/pdf", Size: 300, ScanStatus: "CLEAN"}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := "message body"
			version, err := NewDraft(Input{
				CampaignID: "campaign-types", Version: i + 1, Type: tc.typeValue, Body: body, Media: tc.media,
				CreatedBy: "maker", IdempotencyKey: "supported-message-type-000" + string(rune('1'+i)),
			}, time.Now())
			if err != nil {
				t.Fatalf("supported type %s rejected: %v", tc.typeValue, err)
			}
			if version.Type != tc.typeValue {
				t.Fatalf("type=%s want=%s", version.Type, tc.typeValue)
			}
		})
	}
}
