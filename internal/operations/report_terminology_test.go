package operations

import (
	"strings"
	"testing"
)

func TestCampaignReportRendererKeepsSubmittedSeparateFromDelivered(t *testing.T) {
	report := CampaignReport{
		CampaignID: "campaign-terminology",
		Delivery: map[string]int64{
			"submitted": 7,
			"delivered": 3,
		},
	}
	payload, err := renderCSV(report)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if !strings.Contains(text, "delivery,submitted,7") || !strings.Contains(text, "delivery,delivered,3") {
		t.Fatalf("delivery terminology was collapsed: %s", text)
	}
	if strings.Contains(text, "delivery,delivered,7") {
		t.Fatalf("submitted messages were mislabeled as delivered: %s", text)
	}
}
