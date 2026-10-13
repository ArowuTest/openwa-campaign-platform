package commercial

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCommercialEventPagesContinueWithStableCursor(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	service := &Service{Store: NewMemoryStore(), Clock: func() time.Time { return now }}
	paid := now.Add(-time.Hour)

	draft, err := service.CreateDraft(ctx, Record{
		CampaignID: "campaign-event-pages", OrganisationID: "org-event-pages",
		QuotationReference: "Q-PAGE", InvoiceReference: "I-PAGE", Currency: "NGN",
		ApprovedRecipients: 10, UnitPriceMinor: 50, TotalAmountMinor: 500,
		PaymentReference: "PAY-PAGE", PaymentReceivedAt: &paid,
	}, "maker", "create event pagination fixture")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.Submit(ctx, draft.ID, draft.Version, "maker", "submit event pagination fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Decide(ctx, pending.ID, pending.Version, true, "checker", "approve event pagination fixture"); err != nil {
		t.Fatal(err)
	}

	first, err := service.EventsPage(ctx, draft.ID, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first page=%+v", first)
	}
	second, err := service.EventsPage(ctx, draft.ID, 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatalf("second page=%+v", second)
	}
	if second.Items[0].RecordVersion != 3 {
		t.Fatalf("second page event=%+v", second.Items[0])
	}
	if _, err := service.EventsPage(ctx, draft.ID, 2, "invalid"); !errors.Is(err, ErrInvalidEventCursor) {
		t.Fatalf("invalid event cursor error=%v", err)
	}
	malformed, err := encodeEventCursor(eventCursor{RecordVersion: 1, ID: "not-a-uuid"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.EventsPage(ctx, draft.ID, 2, malformed); !errors.Is(err, ErrInvalidEventCursor) {
		t.Fatalf("malformed event identity accepted: %v", err)
	}
}
