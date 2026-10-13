package commercial

import (
	"context"
	"errors"
	"math"
	"strings"
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
func TestValidateCampaignApprovalRequiresRecordedNonFuturePayment(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 11, 13, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		paymentRef string
		paymentAt  *time.Time
		wantErr    bool
	}{
		{name: "valid evidence", paymentRef: "PAY-VALID", paymentAt: func() *time.Time { v := now.Add(-time.Hour); return &v }()},
		{name: "missing reference", paymentRef: " ", paymentAt: func() *time.Time { v := now.Add(-time.Hour); return &v }(), wantErr: true},
		{name: "missing received time", paymentRef: "PAY-MISSING-TIME", wantErr: true},
		{name: "future received time", paymentRef: "PAY-FUTURE", paymentAt: func() *time.Time { v := now.Add(time.Minute); return &v }(), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemoryStore()
			record := Record{
				ID: "approval-" + tc.name, CampaignID: "campaign-" + tc.name, OrganisationID: "org",
				ApprovedRecipients: 10, Status: StatusApproved,
				PaymentReference: tc.paymentRef, PaymentReceivedAt: tc.paymentAt,
			}
			if _, err := store.Create(ctx, record); err != nil {
				t.Fatal(err)
			}
			service := &Service{Store: store, Clock: func() time.Time { return now }}
			id, err := service.ValidateCampaignApproval(ctx, record.CampaignID, record.OrganisationID, record.ApprovedRecipients)
			if tc.wantErr {
				if !errors.Is(err, ErrNotApproved) {
					t.Fatalf("expected payment evidence rejection, id=%q err=%v", id, err)
				}
				return
			}
			if err != nil || id != record.ID {
				t.Fatalf("valid payment evidence rejected: id=%q err=%v", id, err)
			}
		})
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

func TestCommercialDraftCorrectionAndEventHistory(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	service := &Service{Store: NewMemoryStore(), Clock: func() time.Time { return now }}
	paid := now.Add(-time.Hour)

	draft, err := service.CreateDraft(ctx, Record{
		CampaignID: "campaign-correction", OrganisationID: "org",
		QuotationReference: "Q-OLD", InvoiceReference: "I-OLD", Currency: "NGN",
		ApprovedRecipients: 100, UnitPriceMinor: 50, ManagementFeeMinor: 1000, TotalAmountMinor: 6000,
		PaymentReference: "PAY-OLD", PaymentReceivedAt: &paid,
	}, "maker", "initial commercial evidence")
	if err != nil {
		t.Fatal(err)
	}

	pending, err := service.Submit(ctx, draft.ID, draft.Version, "maker", "submit initial evidence")
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := service.Decide(ctx, pending.ID, pending.Version, false, "checker", "invoice reference needs correction")
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(time.Minute)
	corrected, err := service.UpdateDraft(ctx, rejected.ID, rejected.Version, Record{
		QuotationReference: "Q-NEW", InvoiceReference: "I-NEW", Currency: "NGN",
		ApprovedRecipients: 120, UnitPriceMinor: 60, ManagementFeeMinor: 1200, TotalAmountMinor: 8400,
		PaymentReference: "PAY-NEW", PaymentReceivedAt: &paid,
	}, "maker", "correct invoice and entitlement")
	if err != nil {
		t.Fatal(err)
	}
	if corrected.Status != StatusDraft || corrected.InvoiceReference != "I-NEW" || corrected.ApprovedRecipients != 120 || corrected.Version != rejected.Version+1 {
		t.Fatalf("correction not persisted as fresh draft: %+v", corrected)
	}
	if corrected.SubmittedBy != "" || corrected.ApprovedBy != "" {
		t.Fatalf("correction retained stale decision actors: %+v", corrected)
	}

	events, err := service.Events(ctx, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(events))
	for _, event := range events {
		got = append(got, event.EventType)
	}
	want := []string{"CREATED", "SUBMITTED", "REJECTED", "UPDATED"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events=%v want=%v", got, want)
	}
	if events[2].ActorID != "checker" || events[2].RecordVersion != rejected.Version {
		t.Fatalf("rejection evidence incomplete: %+v", events[2])
	}
}

func TestCommercialApprovedRecordCannotBeEditedAndEventsCoverApprovalRevocation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 11, 7, 0, 0, 0, time.UTC)
	service := &Service{Store: NewMemoryStore(), Clock: func() time.Time { return now }}
	paid := now.Add(-time.Hour)
	draft, err := service.CreateDraft(ctx, Record{
		CampaignID: "campaign-approved", OrganisationID: "org", QuotationReference: "Q1", InvoiceReference: "I1",
		Currency: "NGN", ApprovedRecipients: 10, UnitPriceMinor: 50, TotalAmountMinor: 500,
		PaymentReference: "PAY1", PaymentReceivedAt: &paid,
	}, "maker", "create entitlement")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.Submit(ctx, draft.ID, draft.Version, "maker", "submit entitlement")
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.Decide(ctx, pending.ID, pending.Version, true, "checker", "payment verified")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateDraft(ctx, approved.ID, approved.Version, approved, "maker", "try mutate approved"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("approved commercial evidence was editable: %v", err)
	}
	revoked, err := service.Revoke(ctx, approved.ID, approved.Version, "checker", "payment reversed")
	if err != nil {
		t.Fatal(err)
	}
	events, err := service.Events(ctx, revoked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := events[len(events)-2].EventType + "," + events[len(events)-1].EventType; got != "APPROVED,REVOKED" {
		t.Fatalf("final events=%s events=%+v", got, events)
	}
}

func TestCommercialRejectsOverflowingAmountArithmetic(t *testing.T) {
	service := &Service{Store: NewMemoryStore()}
	_, err := service.CreateDraft(context.Background(), Record{
		CampaignID: "campaign-overflow", OrganisationID: "org",
		QuotationReference: "Q-OVERFLOW", InvoiceReference: "I-OVERFLOW", Currency: "NGN",
		ApprovedRecipients: 2, UnitPriceMinor: math.MaxInt64, ManagementFeeMinor: 0,
		TotalAmountMinor: -2,
	}, "maker", "reject overflowing amount")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("overflowing commercial arithmetic was accepted: %v", err)
	}
}

func TestCommercialEventsFreezeExactGovernedEvidencePerVersion(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 11, 8, 0, 0, 123456000, time.UTC)
	service := &Service{Store: NewMemoryStore(), Clock: func() time.Time { return now }}
	paid := now.Add(-30 * time.Minute)

	draft, err := service.CreateDraft(ctx, Record{
		CampaignID: "campaign-evidence", OrganisationID: "org-evidence",
		QuotationReference: "Q-EVIDENCE", InvoiceReference: "I-EVIDENCE", Currency: "NGN",
		ApprovedRecipients: 125, UnitPriceMinor: 75, ManagementFeeMinor: 625, TotalAmountMinor: 10000,
		PaymentReference: "PAY-EVIDENCE", PaymentReceivedAt: &paid,
	}, "maker", "freeze commercial evidence")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.Submit(ctx, draft.ID, draft.Version, "maker", "submit commercial evidence")
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.Decide(ctx, pending.ID, pending.Version, true, "checker", "independent payment verification")
	if err != nil {
		t.Fatal(err)
	}

	events, err := service.Events(ctx, approved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("event count=%d want 3", len(events))
	}
	if events[0].RecordVersion != 1 || events[1].RecordVersion != 2 || events[2].RecordVersion != 3 {
		t.Fatalf("event versions are not monotonic: %+v", events)
	}
	evidence := events[2].Evidence
	if evidence["campaignId"] != approved.CampaignID ||
		evidence["organisationId"] != approved.OrganisationID ||
		evidence["quotationReference"] != approved.QuotationReference ||
		evidence["invoiceReference"] != approved.InvoiceReference ||
		evidence["currency"] != approved.Currency ||
		evidence["paymentReference"] != approved.PaymentReference ||
		evidence["status"] != StatusApproved ||
		evidence["approvedBy"] != "checker" {
		t.Fatalf("approval evidence snapshot incomplete: %#v", evidence)
	}
	if evidence["approvedRecipients"] != approved.ApprovedRecipients ||
		evidence["unitPriceMinor"] != approved.UnitPriceMinor ||
		evidence["managementFeeMinor"] != approved.ManagementFeeMinor ||
		evidence["totalAmountMinor"] != approved.TotalAmountMinor {
		t.Fatalf("approval monetary evidence drifted: %#v", evidence)
	}
	if evidence["paymentReceivedAt"] != paid.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("payment timestamp evidence=%v want %s", evidence["paymentReceivedAt"], paid.UTC().Format(time.RFC3339Nano))
	}
}

func TestCommercialRejectsNonAlphabeticCurrency(t *testing.T) {
	service := &Service{Store: NewMemoryStore()}
	_, err := service.CreateDraft(context.Background(), Record{
		CampaignID: "campaign-currency", OrganisationID: "org",
		QuotationReference: "Q-CURRENCY", InvoiceReference: "I-CURRENCY", Currency: "12$",
		ApprovedRecipients: 1, UnitPriceMinor: 100, TotalAmountMinor: 100,
	}, "maker", "reject invalid currency")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-alphabetic currency was accepted: %v", err)
	}
}

func TestCommercialApprovalRejectsFuturePaymentEvidence(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	future := now.Add(time.Minute)
	service := &Service{Store: NewMemoryStore(), Clock: func() time.Time { return now }}
	draft, err := service.CreateDraft(ctx, Record{
		CampaignID: "campaign-future-payment", OrganisationID: "org",
		QuotationReference: "Q-FUTURE", InvoiceReference: "I-FUTURE", Currency: "NGN",
		ApprovedRecipients: 10, UnitPriceMinor: 50, TotalAmountMinor: 500,
		PaymentReference: "PAY-FUTURE", PaymentReceivedAt: &future,
	}, "maker", "record future payment evidence")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.Submit(ctx, draft.ID, draft.Version, "maker", "submit future payment evidence")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Decide(ctx, pending.ID, pending.Version, true, "checker", "verify payment evidence"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("future payment evidence was approved: %v", err)
	}
	stored, err := service.Store.Get(ctx, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusPending || stored.Version != pending.Version {
		t.Fatalf("rejected future-payment approval mutated record: %+v", stored)
	}
}

func TestCommercialReasonLengthAndReferenceNormalizationMatchPersistenceContract(t *testing.T) {
	ctx := context.Background()
	service := &Service{Store: NewMemoryStore()}
	validReason := strings.Repeat("r", 1000)
	draft, err := service.CreateDraft(ctx, Record{
		CampaignID: " campaign-normalize ", OrganisationID: " org-normalize ",
		QuotationReference: "  Q-NORMALIZE  ", InvoiceReference: "  I-NORMALIZE  ", Currency: " ngn ",
		ApprovedRecipients: 10, UnitPriceMinor: 50, ManagementFeeMinor: 0, TotalAmountMinor: 500,
		PaymentReference: "  PAY-NORMALIZE  ",
	}, " maker ", validReason)
	if err != nil {
		t.Fatal(err)
	}
	if draft.CampaignID != "campaign-normalize" || draft.OrganisationID != "org-normalize" ||
		draft.QuotationReference != "Q-NORMALIZE" || draft.InvoiceReference != "I-NORMALIZE" ||
		draft.Currency != "NGN" || draft.PaymentReference != "PAY-NORMALIZE" || draft.CreatedBy != "maker" {
		t.Fatalf("commercial service did not normalize governed references/actor: %+v", draft)
	}
	if _, err := service.Submit(ctx, draft.ID, draft.Version, " maker ", strings.Repeat("x", 1001)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized submission reason was not rejected consistently: %v", err)
	}
	pending, err := service.Submit(ctx, draft.ID, draft.Version, " maker ", " submit governed evidence ")
	if err != nil {
		t.Fatal(err)
	}
	if pending.SubmittedBy != "maker" || pending.Reason != "submit governed evidence" {
		t.Fatalf("submission identity/reason were not normalized: %+v", pending)
	}
	if _, err := service.Decide(ctx, pending.ID, pending.Version, false, " checker ", strings.Repeat("x", 1001)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized decision reason was not rejected consistently: %v", err)
	}
	if _, err := service.CreateDraft(ctx, Record{
		CampaignID: "c-too-long", OrganisationID: "o-too-long", QuotationReference: "Q", InvoiceReference: "I",
		Currency: "NGN", ApprovedRecipients: 1, UnitPriceMinor: 1, TotalAmountMinor: 1,
	}, "maker", strings.Repeat("x", 1001)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized creation reason was not rejected consistently: %v", err)
	}
}

func TestCommercialRevocationCanBeCorrectedAndReapprovedWithoutCreatingSecondCampaignRecord(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	paid := now.Add(-time.Hour)
	service := &Service{Store: NewMemoryStore(), Clock: func() time.Time { return now }}

	draft, err := service.CreateDraft(ctx, Record{
		CampaignID: "campaign-revoked-recovery", OrganisationID: "org",
		QuotationReference: "Q-OLD", InvoiceReference: "I-OLD", Currency: "NGN",
		ApprovedRecipients: 100, UnitPriceMinor: 50, TotalAmountMinor: 5000,
		PaymentReference: "PAY-OLD", PaymentReceivedAt: &paid,
	}, "maker", "record initial commercial evidence")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.Submit(ctx, draft.ID, draft.Version, "maker", "submit initial commercial evidence")
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.Decide(ctx, pending.ID, pending.Version, true, "checker-a", "approve initial payment evidence")
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := service.Revoke(ctx, approved.ID, approved.Version, "checker-b", "revoke superseded commercial authority")
	if err != nil {
		t.Fatal(err)
	}

	replacementPaid := now.Add(-30 * time.Minute)
	corrected, err := service.UpdateDraft(ctx, revoked.ID, revoked.Version, Record{
		QuotationReference: "Q-REPLACEMENT", InvoiceReference: "I-REPLACEMENT", Currency: "NGN",
		ApprovedRecipients: 120, UnitPriceMinor: 50, TotalAmountMinor: 6000,
		PaymentReference: "PAY-REPLACEMENT", PaymentReceivedAt: &replacementPaid,
	}, "maker", "replace revoked commercial evidence")
	if err != nil {
		t.Fatal(err)
	}
	if corrected.ID != revoked.ID || corrected.Status != StatusDraft || corrected.Version != revoked.Version+1 {
		t.Fatalf("revoked commercial record did not reopen as the same governed draft: revoked=%+v corrected=%+v", revoked, corrected)
	}
	if corrected.SubmittedBy != "" || corrected.ApprovedBy != "" {
		t.Fatalf("reopened draft retained stale approval identity: %+v", corrected)
	}

	resubmitted, err := service.Submit(ctx, corrected.ID, corrected.Version, "maker", "submit corrected commercial evidence")
	if err != nil {
		t.Fatal(err)
	}
	reapproved, err := service.Decide(ctx, resubmitted.ID, resubmitted.Version, true, "checker-c", "approve corrected payment evidence")
	if err != nil {
		t.Fatal(err)
	}
	if reapproved.Status != StatusApproved || reapproved.ApprovedRecipients != 120 {
		t.Fatalf("corrected commercial evidence did not regain approval: %+v", reapproved)
	}
	events, err := service.Events(ctx, reapproved.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"CREATED", "SUBMITTED", "APPROVED", "REVOKED", "UPDATED", "SUBMITTED", "APPROVED"}
	if len(events) != len(want) {
		t.Fatalf("event count=%d want=%d: %+v", len(events), len(want), events)
	}
	for i, event := range events {
		if event.EventType != want[i] || event.RecordVersion != int64(i+1) {
			t.Fatalf("event[%d]=%+v want type=%s version=%d", i, event, want[i], i+1)
		}
	}
}
