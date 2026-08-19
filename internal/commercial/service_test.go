package commercial

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCommercialMakerCheckerAndEntitlement(t *testing.T) {
	s := &Service{Store: NewMemoryStore(), Clock: func() time.Time { return time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC) }}
	paid := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	r, err := s.CreateDraft(context.Background(), Record{CampaignID: "cmp", OrganisationID: "org", QuotationReference: "Q1", InvoiceReference: "I1", Currency: "NGN", ApprovedRecipients: 100, UnitPriceMinor: 50, ManagementFeeMinor: 1000, TotalAmountMinor: 6000, PaymentReference: "PAY1", PaymentReceivedAt: &paid}, "maker", "initial quote")
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Submit(context.Background(), r.ID, r.Version, "maker", "submit approval")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(context.Background(), r.ID, r.Version, true, "maker", "self approval"); err == nil {
		t.Fatal("expected maker checker rejection")
	}
	r, err = s.Decide(context.Background(), r.ID, r.Version, true, "checker", "payment verified")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ValidateCampaignApproval(context.Background(), "cmp", "org", 101); err == nil {
		t.Fatal("expected entitlement rejection")
	}
	if id, err := s.ValidateCampaignApproval(context.Background(), "cmp", "org", 100); err != nil || id != r.ID {
		t.Fatalf("validation failed: %s %v", id, err)
	}
}
func TestCommercialTotalMustMatch(t *testing.T) {
	s := &Service{Store: NewMemoryStore()}
	_, err := s.CreateDraft(context.Background(), Record{CampaignID: "c", OrganisationID: "o", QuotationReference: "q", InvoiceReference: "i", Currency: "NGN", ApprovedRecipients: 2, UnitPriceMinor: 10, TotalAmountMinor: 99}, "a", "valid reason")
	if err == nil {
		t.Fatal("expected invalid total")
	}
}

func TestCommercialEntitlementCannotBeReusedByAnotherCampaign(t *testing.T) {
	s := &Service{Store: NewMemoryStore(), Clock: func() time.Time { return time.Date(2026, 8, 8, 15, 0, 0, 0, time.UTC) }}
	paid := time.Date(2026, 8, 8, 14, 0, 0, 0, time.UTC)
	r, err := s.CreateDraft(context.Background(), Record{CampaignID: "campaign-a", OrganisationID: "org", QuotationReference: "Q-A", InvoiceReference: "I-A", Currency: "NGN", ApprovedRecipients: 100, UnitPriceMinor: 50, TotalAmountMinor: 5000, PaymentReference: "PAY-A", PaymentReceivedAt: &paid}, "maker", "campaign a entitlement")
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Submit(context.Background(), r.ID, r.Version, "maker", "submit entitlement")
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Decide(context.Background(), r.ID, r.Version, true, "checker", "approve entitlement")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidateCampaignApproval(context.Background(), "campaign-b", "org", 100); err == nil {
		t.Fatal("campaign B reused campaign A entitlement")
	}
	if id, err := s.ValidateCampaignApproval(context.Background(), "campaign-a", "org", 100); err != nil || id != r.ID {
		t.Fatalf("campaign A entitlement invalid: id=%q err=%v", id, err)
	}
}

func TestCommercialCreatorCannotApproveAfterIndependentSubmission(t *testing.T) {
	ctx := context.Background()
	paid := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	service := &Service{Store: NewMemoryStore()}
	draft, err := service.CreateDraft(ctx, Record{
		CampaignID: "campaign-three-actor", OrganisationID: "org",
		QuotationReference: "Q-3", InvoiceReference: "I-3", Currency: "NGN",
		ApprovedRecipients: 10, UnitPriceMinor: 50, TotalAmountMinor: 500,
		PaymentReference: "PAY-3", PaymentReceivedAt: &paid,
	}, "maker", "maker creates entitlement")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.Submit(ctx, draft.ID, draft.Version, "submitter", "submitter submits entitlement")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Decide(ctx, pending.ID, pending.Version, true, "maker", "maker self approves"); !errors.Is(err, ErrMakerChecker) {
		t.Fatalf("creator approved own commercial record after another actor submitted it: %v", err)
	}
	stored, err := service.Store.Get(ctx, pending.ID)
	if err != nil || stored.Status != StatusPending {
		t.Fatalf("rejected decision mutated record: status=%s err=%v", stored.Status, err)
	}
}
