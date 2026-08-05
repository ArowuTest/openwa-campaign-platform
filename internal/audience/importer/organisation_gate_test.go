package importer

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/organisation"
)

func TestImportCreationRequiresActiveOrganisation(t *testing.T) {
	orgs := organisation.NewService(organisation.NewMemoryRepository())
	org, err := orgs.Create(context.Background(), organisation.CreateInput{LegalName: "Governed Org"})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewMemoryImportRepository()
	svc := ImportService{Repository: repo, Organisations: orgs}
	input := validCreateImportInput()
	input.OrganisationID = org.ID
	if _, _, err := svc.Create(context.Background(), input); !errors.Is(err, organisation.ErrNotActive) {
		t.Fatalf("expected inactive organisation rejection, got %v", err)
	}
}

func TestImportApprovalRechecksOrganisationStatus(t *testing.T) {
	ctx := context.Background()
	orgs := organisation.NewService(organisation.NewMemoryRepository())
	org, err := orgs.Create(ctx, organisation.CreateInput{LegalName: "Governed Org"})
	if err != nil {
		t.Fatal(err)
	}
	org, err = orgs.SetStatus(ctx, org.ID, organisation.StatusInput{ExpectedVersion: org.Version, Status: organisation.StatusActive, ActorID: "approver", Reason: "approved onboarding"})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewMemoryImportRepository()
	svc := ImportService{Repository: repo, Organisations: orgs}
	input := validCreateImportInput()
	input.OrganisationID = org.ID
	batch, _, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	batch, err = svc.RecordScan(ctx, batch.ID, MalwareClean, true, "", batch.Version)
	if err != nil {
		t.Fatal(err)
	}
	batch, err = repo.SetPreviewReady(batch.ID, PreviewResult{UploadedRows: 1, ValidRows: 1}, batch.Version, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	repo.SetConsentEligible(batch.ConsentReviewID, true)
	org, err = orgs.SetStatus(ctx, org.ID, organisation.StatusInput{ExpectedVersion: org.Version, Status: organisation.StatusSuspended, ActorID: "compliance", Reason: "consent concern"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(ctx, batch.ID, "checker", batch.Version); !errors.Is(err, organisation.ErrNotActive) {
		t.Fatalf("expected suspended organisation to block import approval, got %v", err)
	}
}
