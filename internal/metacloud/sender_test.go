package metacloud

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func validSender() Sender {
	return Sender{
		OrganisationID:       "00000000-0000-4000-8000-000000000011",
		SenderPoolID:         "00000000-0000-4000-8000-000000000012",
		WABAID:               "123456789012345",
		PhoneNumberID:        "987654321098765",
		DisplayName:          "Groove Support",
		BusinessPhoneDisplay: "+234 *** 0001",
		CredentialKey:        "meta-prod-ng-1",
		GraphAPIVersion:      "v23.0",
	}
}

func TestSenderLifecycleRequiresIndependentApprover(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 11, 18, 30, 0, 0, time.UTC)
	svc := &Service{Store: NewMemoryStore(), Clock: func() time.Time { return now }}
	draft, err := svc.CreateDraft(ctx, validSender(), "maker", "create Meta sender")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := svc.Submit(ctx, draft.ID, draft.Version, "submitter", "submit Meta sender")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Decide(ctx, pending.ID, pending.Version, true, "maker", "maker cannot approve", now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("creator approved own sender: %v", err)
	}
	if _, err = svc.Decide(ctx, pending.ID, pending.Version, true, "submitter", "submitter cannot approve", now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("submitter approved own sender: %v", err)
	}
	active, err := svc.Decide(ctx, pending.ID, pending.Version, true, "checker", "approve Meta sender", now)
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != StatusActive || active.ApprovedBy != "checker" || active.EffectiveFrom == nil {
		t.Fatalf("unexpected active sender: %#v", active)
	}
}

func TestSenderJSONExposesCredentialReferenceOnly(t *testing.T) {
	sender := validSender()
	raw, err := json.Marshal(sender)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, `"credentialKey":"meta-prod-ng-1"`) {
		t.Fatalf("credential key missing from sender JSON: %s", body)
	}
	for _, forbidden := range []string{"accessToken", "appSecret", "verifyToken"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("secret field %s leaked in sender JSON: %s", forbidden, body)
		}
	}
}

func TestSenderHealthRequiresActiveSenderAndMonotonicObservation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 11, 18, 45, 0, 0, time.UTC)
	svc := &Service{Store: NewMemoryStore(), Clock: func() time.Time { return now }}
	draft, err := svc.CreateDraft(ctx, validSender(), "maker", "create Meta sender")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ObserveHealth(ctx, draft.ID, draft.Version, HealthHealthy, now, "worker", "verified Meta sender"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("draft sender accepted health evidence: %v", err)
	}
	pending, err := svc.Submit(ctx, draft.ID, draft.Version, "submitter", "submit Meta sender")
	if err != nil {
		t.Fatal(err)
	}
	active, err := svc.Decide(ctx, pending.ID, pending.Version, true, "checker", "approve Meta sender", now)
	if err != nil {
		t.Fatal(err)
	}
	observedAt := now.Add(time.Minute)
	healthy, err := svc.ObserveHealth(ctx, active.ID, active.Version, HealthHealthy, observedAt, "worker", "Graph verification succeeded")
	if err != nil {
		t.Fatal(err)
	}
	if healthy.Health != HealthHealthy || healthy.HealthObservedAt == nil || !healthy.HealthObservedAt.Equal(observedAt) {
		t.Fatalf("health evidence missing: %#v", healthy)
	}
	if _, err = svc.ObserveHealth(ctx, healthy.ID, healthy.Version, HealthDegraded, now, "worker", "stale throttling observation"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("stale health evidence accepted: %v", err)
	}
}

func TestSenderHealthEventRetainsActor(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 11, 19, 0, 0, 0, time.UTC)
	svc := &Service{Store: NewMemoryStore(), Clock: func() time.Time { return now }}
	draft, err := svc.CreateDraft(ctx, validSender(), "maker", "create Meta sender")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := svc.Submit(ctx, draft.ID, draft.Version, "submitter", "submit Meta sender")
	if err != nil {
		t.Fatal(err)
	}
	active, err := svc.Decide(ctx, pending.ID, pending.Version, true, "checker", "approve Meta sender", now)
	if err != nil {
		t.Fatal(err)
	}
	healthy, err := svc.ObserveHealth(ctx, active.ID, active.Version, HealthHealthy, now.Add(time.Minute), "worker-7", "Graph verification succeeded")
	if err != nil {
		t.Fatal(err)
	}
	events, err := svc.ListEvents(ctx, healthy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := events[len(events)-1].ActorID; got != "worker-7" {
		t.Fatalf("health actor=%q want worker-7", got)
	}
}

func TestMemoryStoreResolvesExactMetaWebhookSenderBinding(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := &Service{Store: store, Clock: func() time.Time { return time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC) }}
	draft, err := svc.CreateDraft(ctx, validSender(), "maker", "create webhook sender")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := svc.Submit(ctx, draft.ID, draft.Version, "submitter", "submit webhook sender")
	if err != nil {
		t.Fatal(err)
	}
	active, err := svc.Decide(ctx, pending.ID, pending.Version, true, "checker", "approve webhook sender", time.Date(2026, 8, 13, 3, 59, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := store.ResolveMetaWebhookSender(ctx, active.CredentialKey, active.WABAID, active.PhoneNumberID)
	if err != nil || resolved.ID != active.ID {
		t.Fatalf("resolved=%#v err=%v", resolved, err)
	}
	if _, err := store.ResolveMetaWebhookSender(ctx, active.CredentialKey, active.WABAID, "different-phone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mismatched phone resolved: %v", err)
	}
}
