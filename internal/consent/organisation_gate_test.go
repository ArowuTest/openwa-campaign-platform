package consent

import (
	"context"
	"errors"
	"testing"

	"campaign-platform/internal/organisation"
)

func TestReviewCreationRequiresActiveOrganisation(t *testing.T) {
	orgs := organisation.NewService(organisation.NewMemoryRepository())
	org, err := orgs.Create(context.Background(), organisation.CreateInput{LegalName: "Governed Org"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewMemoryRepository()).WithOrganisationReader(orgs)
	input := CreateInput{OrganisationID: org.ID, Name: "Review", Channel: "WHATSAPP"}
	if _, err := svc.Create(context.Background(), input); !errors.Is(err, organisation.ErrNotActive) {
		t.Fatalf("expected inactive organisation rejection, got %v", err)
	}
}
