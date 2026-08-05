package organisation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOrganisationPolicyMakerCheckerAndPurposeEnforcement(t *testing.T) {
	ctx := context.Background()
	orgRepo := NewMemoryRepository()
	orgSvc := NewService(orgRepo)
	org, err := orgSvc.Create(ctx, CreateInput{LegalName: "Policy Test Ltd", CountryISO2: "NG"})
	if err != nil {
		t.Fatal(err)
	}
	org, err = orgSvc.SetStatus(ctx, org.ID, StatusInput{ExpectedVersion: org.Version, Status: StatusActive, ActorID: "checker", Reason: "approved for operations"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	admin := &PolicyAdministration{Store: NewMemoryPolicyStore(), Organisations: orgSvc, Clock: func() time.Time { return now }}
	draft, err := admin.CreateDraft(ctx, Policy{OrganisationID: org.ID, AllowedPurposeIDs: []string{"events"}, ProhibitedPurposeIDs: []string{"finance"}, ContactRetentionDays: 365, CampaignRetentionDays: 730, ReportBrandName: "Policy Test"}, "maker", "initial controlled policy")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := admin.Submit(ctx, draft.ID, draft.Version, "maker", "ready for independent review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Decide(ctx, pending.ID, pending.Version, true, "maker", "self approve"); !errors.Is(err, ErrPolicyInvalid) {
		t.Fatalf("expected maker-checker rejection, got %v", err)
	}
	active, err := admin.Decide(ctx, pending.ID, pending.Version, true, "checker", "approved after evidence review")
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != PolicyActive {
		t.Fatalf("status=%s", active.Status)
	}
	if err = admin.ValidatePurpose(ctx, org.ID, "events"); err != nil {
		t.Fatalf("permitted purpose rejected: %v", err)
	}
	if err = admin.ValidatePurpose(ctx, org.ID, "finance"); !errors.Is(err, ErrPurposeProhibited) {
		t.Fatalf("expected prohibited, got %v", err)
	}
	if err = admin.ValidatePurpose(ctx, org.ID, "telecom"); !errors.Is(err, ErrPurposeNotPermitted) {
		t.Fatalf("expected not permitted, got %v", err)
	}
}

func TestOrganisationPolicyRejectsOverlappingPurposes(t *testing.T) {
	admin := &PolicyAdministration{Store: NewMemoryPolicyStore()}
	_, err := admin.CreateDraft(context.Background(), Policy{OrganisationID: "org", AllowedPurposeIDs: []string{"x"}, ProhibitedPurposeIDs: []string{"x"}, ContactRetentionDays: 10, CampaignRetentionDays: 30}, "maker", "invalid overlap policy")
	if !errors.Is(err, ErrPolicyInvalid) {
		t.Fatalf("expected invalid, got %v", err)
	}
}
