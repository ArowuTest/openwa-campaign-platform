package metacloud

import (
	"context"
	"testing"
	"time"
)

func TestCouncilMemorySenderCreateRejectsDuplicatePhoneIdentity(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	first := Sender{ID: "sender-phone-a", OrganisationID: "org-a", SenderPoolID: "pool-a", WABAID: "waba-a", PhoneNumberID: "phone-shared", DisplayName: "Sender A", BusinessPhoneDisplay: "+234 ***", CredentialKey: "meta-a", GraphAPIVersion: "v23.0", Status: StatusDraft, Health: HealthUnknown, Version: 1, CreatedBy: "actor-a", Reason: "create sender phone a", CreatedAt: now, UpdatedAt: now}
	if _, err := store.Create(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID, second.OrganisationID, second.SenderPoolID, second.WABAID, second.CredentialKey = "sender-phone-b", "org-b", "pool-b", "waba-b", "meta-b"
	if _, err := store.Create(context.Background(), second); err != ErrConflict {
		t.Fatalf("duplicate phone identity error=%v want conflict", err)
	}
}

func TestCouncilMemorySenderCASRejectsIdentityMutation(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	original := Sender{ID: "sender-immutable", OrganisationID: "org-a", SenderPoolID: "pool-a", WABAID: "waba-a", PhoneNumberID: "phone-a", DisplayName: "Sender A", BusinessPhoneDisplay: "+234 ***", CredentialKey: "meta-a", GraphAPIVersion: "v23.0", Status: StatusDraft, Health: HealthUnknown, Version: 1, CreatedBy: "actor-a", Reason: "create immutable sender", CreatedAt: now, UpdatedAt: now}
	if _, err := store.Create(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	mutated := original
	mutated.WABAID = "waba-attacker"
	mutated.PhoneNumberID = "phone-attacker"
	mutated.CredentialKey = "meta-attacker"
	mutated.Version = 2
	mutated.Status = StatusPending
	mutated.UpdatedAt = now.Add(time.Minute)
	if _, err := store.CompareAndSwap(context.Background(), mutated, 1, "actor-b", "PENDING_APPROVAL", nil); err != ErrConflict {
		t.Fatalf("identity mutation was not rejected: %v", err)
	}
	got, err := store.Get(context.Background(), original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.WABAID != original.WABAID || got.PhoneNumberID != original.PhoneNumberID || got.CredentialKey != original.CredentialKey || got.Version != 1 {
		t.Fatalf("immutable identity changed: %#v", got)
	}
}
