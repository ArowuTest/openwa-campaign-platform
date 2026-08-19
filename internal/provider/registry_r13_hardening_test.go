package provider

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestProviderMutationMethodsFailClosedWithoutStore(t *testing.T) {
	ctx := context.Background()
	calls := map[string]func() error{
		"submit": func() error { _, err := (&Service{}).Submit(ctx, "x", 1, "actor", "valid reason"); return err },
		"decide": func() error { _, err := (&Service{}).Decide(ctx, "x", 1, true, "actor", "valid reason"); return err },
		"retire": func() error { _, err := (&Service{}).Retire(ctx, "x", 1, "actor", "valid reason"); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("mutation panicked without store: %v", r)
				}
			}()
			err := call()
			if err == nil || !strings.Contains(err.Error(), "store is required") {
				t.Fatalf("got %v, want fail-closed store error", err)
			}
		})
	}
}

func TestProviderRejectRecordsCheckerAndClearsCallerAuditFields(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := &Service{Store: store, Clock: func() time.Time { return time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC) }}
	draft, err := service.CreateDraft(ctx, Definition{Provider: "OPENWA", Channel: ChannelWhatsApp, Engine: "BAILEYS", AdapterVersion: "1.0.0", Capabilities: []Capability{CapabilitySendText}, SubmittedBy: "forged-submitter", ApprovedBy: "forged-checker"}, "maker", "create governed definition")
	if err != nil {
		t.Fatal(err)
	}
	if draft.SubmittedBy != "" || draft.ApprovedBy != "" {
		t.Fatalf("caller injected audit fields: %+v", draft)
	}
	submitted, err := service.Submit(ctx, draft.ID, draft.Version, "maker", "submit for independent review")
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := service.Decide(ctx, draft.ID, submitted.Version, false, "checker", "reject after independent review")
	if err != nil {
		t.Fatal(err)
	}
	if rejected.ApprovedBy != "checker" {
		t.Fatalf("reject did not stamp checker: %+v", rejected)
	}
	events, err := service.ListEvents(ctx, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 || events[len(events)-1].Action != string(StatusRejected) || events[len(events)-1].ActorID != "checker" {
		t.Fatalf("reject audit actor incorrect: %#v", events)
	}
}

func TestProviderApprovalActorSurvivesSupersedeAndRetire(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	service := &Service{Store: store, Clock: func() time.Time { return now }}
	approve := func(adapter, maker, checker string, at time.Time) Definition {
		service.Clock = func() time.Time { return at }
		d, err := service.CreateDraft(ctx, Definition{Provider: "OPENWA", Channel: ChannelWhatsApp, Engine: "BAILEYS", AdapterVersion: adapter, Capabilities: []Capability{CapabilitySendText}, EffectiveFrom: at}, maker, "create provider definition")
		if err != nil {
			t.Fatal(err)
		}
		s, err := service.Submit(ctx, d.ID, d.Version, maker, "submit provider definition")
		if err != nil {
			t.Fatal(err)
		}
		a, err := service.Decide(ctx, s.ID, s.Version, true, checker, "approve provider definition")
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	first := approve("1.0.0", "maker-one", "checker-one", now)
	second := approve("2.0.0", "maker-two", "checker-two", now.Add(time.Minute))
	retiredFirst, err := service.Get(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retiredFirst.ApprovedBy != "checker-one" {
		t.Fatalf("supersede rewrote original approval actor: %+v", retiredFirst)
	}
	events, err := service.ListEvents(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if events[len(events)-1].Action != "SUPERSEDED" || events[len(events)-1].ActorID != "checker-two" {
		t.Fatalf("supersede event actor wrong: %#v", events)
	}
	service.Clock = func() time.Time { return now.Add(2 * time.Minute) }
	retiredSecond, err := service.Retire(ctx, second.ID, second.Version, "retirer", "retire provider definition")
	if err != nil {
		t.Fatal(err)
	}
	if retiredSecond.ApprovedBy != "checker-two" {
		t.Fatalf("retire rewrote original approval actor: %+v", retiredSecond)
	}
	events, err = service.ListEvents(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if events[len(events)-1].Action != string(StatusRetired) || events[len(events)-1].ActorID != "retirer" {
		t.Fatalf("retire event actor wrong: %#v", events)
	}
}
