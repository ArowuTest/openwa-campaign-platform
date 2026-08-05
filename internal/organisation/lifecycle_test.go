package organisation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLifecycleUpdateStatusAndEvents(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo)
	now := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	svc.clock = func() time.Time { return now }
	org, err := svc.Create(context.Background(), CreateInput{LegalName: " Example  Events ", CountryISO2: "ng", PrimaryContactEmail: "OPS@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.Update(context.Background(), org.ID, UpdateInput{ExpectedVersion: 1, LegalName: "Example Events Ltd", CountryISO2: "NG", PrimaryContactEmail: "ops@example.com", ActorID: "actor-1", Reason: "legal name correction"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 || updated.LegalName != "Example Events Ltd" {
		t.Fatalf("unexpected update: %+v", updated)
	}
	suspended, err := svc.SetStatus(context.Background(), org.ID, StatusInput{ExpectedVersion: 2, Status: StatusSuspended, ActorID: "actor-2", Reason: "consent review expired"})
	if err != nil {
		t.Fatal(err)
	}
	if suspended.Version != 3 || suspended.Status != StatusSuspended {
		t.Fatalf("unexpected status: %+v", suspended)
	}
	events, err := svc.ListEvents(context.Background(), org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events=%d", len(events))
	}
}
func TestLifecycleConflictAndClosedProtection(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	org, err := svc.Create(context.Background(), CreateInput{LegalName: "Closed Org"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.SetStatus(context.Background(), org.ID, StatusInput{ExpectedVersion: 1, Status: StatusClosed, ActorID: "actor", Reason: "relationship ended"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Update(context.Background(), org.ID, UpdateInput{ExpectedVersion: 2, LegalName: "Changed", ActorID: "actor"})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected closed, got %v", err)
	}
	_, err = svc.SetStatus(context.Background(), org.ID, StatusInput{ExpectedVersion: 1, Status: StatusSuspended, ActorID: "actor", Reason: "stale"})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected closed, got %v", err)
	}
}
func TestValidation(t *testing.T) {
	if _, err := New(CreateInput{LegalName: "X", CountryISO2: "NGA"}, time.Now()); err == nil {
		t.Fatal("expected country validation")
	}
	if _, err := New(CreateInput{LegalName: "X", PrimaryContactEmail: "not-email"}, time.Now()); err == nil {
		t.Fatal("expected email validation")
	}
}

func TestNewOrganisationStartsUnderReview(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	org, err := svc.Create(context.Background(), CreateInput{LegalName: "New Organisation"})
	if err != nil {
		t.Fatal(err)
	}
	if org.Status != StatusUnderReview {
		t.Fatalf("new organisation status=%s, want %s", org.Status, StatusUnderReview)
	}
}
