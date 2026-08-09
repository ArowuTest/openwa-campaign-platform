package message

import (
	"context"
	"testing"
	"time"
)

func TestMaterialMessageChangesCreateSuccessiveImmutableVersions(t *testing.T) {
	service := NewService(NewMemoryRepository())
	service.clock = func() time.Time { return time.Date(2026, 8, 8, 14, 30, 0, 0, time.UTC) }
	first, err := service.CreateDraft(context.Background(), Input{
		CampaignID: "campaign-versioning", Type: TypeText, Body: "Hello customer",
		CreatedBy: "maker", IdempotencyKey: "message-versioning-0001",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateDraft(context.Background(), Input{
		CampaignID: "campaign-versioning", Type: TypeText, Body: "Hello {{first_name}} — details",
		Variables: []Variable{{Name: "first_name", DataType: "TEXT", Fallback: "Customer"}},
		Links:     []Link{{URL: "https://example.com/details", Label: "Details"}}, AllowedHosts: []string{"example.com"},
		CreatedBy: "maker", IdempotencyKey: "message-versioning-0002",
	})
	if err != nil {
		t.Fatal(err)
	}
	third, err := service.CreateDraft(context.Background(), Input{
		CampaignID: "campaign-versioning", Type: TypeImageCaption, Body: "Updated creative",
		Media:     &Media{ObjectKey: "campaigns/creative.png", SHA256: "abc123", MediaType: "image/png", Size: 1234, ScanStatus: "CLEAN"},
		CreatedBy: "maker", IdempotencyKey: "message-versioning-0003",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || second.Version != 2 || third.Version != 3 {
		t.Fatalf("versions did not advance: first=%d second=%d third=%d", first.Version, second.Version, third.Version)
	}
	if first.ContentHash == second.ContentHash || second.ContentHash == third.ContentHash || first.ContentHash == third.ContentHash {
		t.Fatalf("materially different versions reused a content hash: %s %s %s", first.ContentHash, second.ContentHash, third.ContentHash)
	}
	if second.Variables[0].Fallback != "Customer" || len(second.Links) != 1 || third.Media == nil || third.Media.ObjectKey != "campaigns/creative.png" {
		t.Fatalf("versioned material was not retained: second=%+v third=%+v", second, third)
	}
	listed, err := service.ListByCampaign(context.Background(), "campaign-versioning")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 || listed[0].Version != 3 || listed[1].Version != 2 || listed[2].Version != 1 {
		t.Fatalf("immutable version history was not retained: %+v", listed)
	}
}
