package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAdministrationProvisionUpdateResetAndRevoke(t *testing.T) {
	repo := NewMemoryAdministrationRepository("SUPER_ADMIN", "CAMPAIGN_OPERATOR")
	sessions := NewMemorySessionRepository()
	service := NewAdministrationService(repo, sessions)
	fixed := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return fixed }
	created, err := service.Create(context.Background(), CreateAccountInput{Email: "alice@example.test", DisplayName: "Alice Operator", TemporaryPassword: "a-temporary-password-long-enough", RoleCodes: []string{"CAMPAIGN_OPERATOR"}, ActorID: "admin-1", Reason: "onboard campaign operator"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Account.Status != StatusInvited || created.TOTPSecret == "" || created.Account.Version != 1 {
		t.Fatalf("unexpected provisioned account: %+v", created)
	}
	updated, err := service.Update(context.Background(), created.Account.ID, UpdateAccountInput{DisplayName: "Alice Operator", Status: StatusActive, RoleCodes: []string{"CAMPAIGN_OPERATOR"}, MFARequired: true, ExpectedVersion: 1, ActorID: "admin-1", Reason: "activate after verification"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusActive || updated.Version != 2 {
		t.Fatalf("unexpected updated account: %+v", updated)
	}
	reset, err := service.ResetCredentials(context.Background(), updated.ID, ResetCredentialInput{TemporaryPassword: "another-temporary-password-long", ExpectedVersion: 2, ActorID: "admin-1", Reason: "credential recovery request"})
	if err != nil {
		t.Fatal(err)
	}
	if reset.Account.Version != 3 || reset.TOTPSecret == "" {
		t.Fatalf("unexpected reset: %+v", reset)
	}
}

func TestAdministrationProtectsLastSuperAdmin(t *testing.T) {
	repo := NewMemoryAdministrationRepository("SUPER_ADMIN")
	sessions := NewMemorySessionRepository()
	service := NewAdministrationService(repo, sessions)
	created, err := service.Create(context.Background(), CreateAccountInput{Email: "owner@example.test", DisplayName: "Platform Owner", TemporaryPassword: "a-temporary-password-long-enough", RoleCodes: []string{"SUPER_ADMIN"}, ActorID: "bootstrap", Reason: "create platform owner"})
	if err != nil {
		t.Fatal(err)
	}
	// Activate directly through repository to establish the protected state.
	account := created.Account
	account.Status = StatusActive
	account.Version = 2
	if err := repo.CompareAndSwapAccount(context.Background(), account, 1, "bootstrap", "activate owner"); err != nil {
		t.Fatal(err)
	}
	_, err = service.Update(context.Background(), account.ID, UpdateAccountInput{DisplayName: account.DisplayName, Status: StatusDisabled, RoleCodes: []string{"SUPER_ADMIN"}, MFARequired: true, ExpectedVersion: 2, ActorID: "owner", Reason: "attempt to disable owner"})
	if !errors.Is(err, ErrLastSuperAdmin) {
		t.Fatalf("expected last-super-admin protection, got %v", err)
	}
}

func TestAdministrationRejectsGenericAndUnknownRole(t *testing.T) {
	repo := NewMemoryAdministrationRepository("ANALYST")
	service := NewAdministrationService(repo, NewMemorySessionRepository())
	_, err := service.Create(context.Background(), CreateAccountInput{Email: "support@example.test", DisplayName: "Support Team", TemporaryPassword: "a-temporary-password-long-enough", RoleCodes: []string{"ANALYST"}, ActorID: "admin", Reason: "create shared account"})
	if !errors.Is(err, ErrGenericSharedAccount) {
		t.Fatalf("expected generic-account rejection, got %v", err)
	}
	_, err = service.Create(context.Background(), CreateAccountInput{Email: "bob@example.test", DisplayName: "Bob Analyst", TemporaryPassword: "a-temporary-password-long-enough", RoleCodes: []string{"UNKNOWN"}, ActorID: "admin", Reason: "onboard analyst"})
	if !errors.Is(err, ErrUnknownRole) {
		t.Fatalf("expected unknown role rejection, got %v", err)
	}
}
