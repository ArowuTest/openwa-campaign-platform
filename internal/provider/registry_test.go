package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProviderDefinitionMakerCheckerAndCapabilityResolution(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	svc := &Service{Store: NewMemoryStore(), Clock: func() time.Time { return now }}
	d, err := svc.CreateDraft(context.Background(), Definition{Provider: "openwa", Channel: ChannelWhatsApp, Engine: "baileys", AdapterVersion: "0.13.0", Capabilities: []Capability{CapabilitySendText, CapabilityPairingCode, CapabilitySendText}}, "maker", "initial OpenWA definition")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Capabilities) != 2 {
		t.Fatalf("expected deduplicated capabilities, got %v", d.Capabilities)
	}
	d, err = svc.Submit(context.Background(), d.ID, d.Version, "maker", "submit definition")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Decide(context.Background(), d.ID, d.Version, true, "maker", "self approval"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected maker checker error, got %v", err)
	}
	d, err = svc.Decide(context.Background(), d.ID, d.Version, true, "checker", "approve provider route")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.Require(context.Background(), "OPENWA", ChannelWhatsApp, "BAILEYS", now, []Capability{CapabilitySendText})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != d.ID {
		t.Fatalf("resolved wrong definition")
	}
	if _, err = svc.Require(context.Background(), "OPENWA", ChannelWhatsApp, "BAILEYS", now, []Capability{CapabilityReadEvents}); err == nil {
		t.Fatal("missing capability accepted")
	}
}

func TestActivatingReplacementRetiresOverlappingDefinition(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	svc := &Service{Store: store, Clock: func() time.Time { return now }}
	createActivate := func(version string) Definition {
		d, err := svc.CreateDraft(context.Background(), Definition{Provider: "OPENWA", Channel: ChannelWhatsApp, Engine: "WHATSAPP_WEB_JS", AdapterVersion: version, Capabilities: []Capability{CapabilitySendText}}, "maker", "create definition")
		if err != nil {
			t.Fatal(err)
		}
		d, err = svc.Submit(context.Background(), d.ID, d.Version, "maker", "submit definition")
		if err != nil {
			t.Fatal(err)
		}
		d, err = svc.Decide(context.Background(), d.ID, d.Version, true, "checker", "approve definition")
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	first := createActivate("1.0.0")
	second := createActivate("1.1.0")
	old, err := store.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.Status != StatusRetired {
		t.Fatalf("expected retired, got %s", old.Status)
	}
	active, err := store.Active(context.Background(), "OPENWA", ChannelWhatsApp, "WHATSAPP_WEB_JS", now)
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != second.ID {
		t.Fatalf("expected replacement active")
	}
}

func TestRetireActiveDefinitionPreservesEventHistory(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	service := &Service{Store: store, Clock: func() time.Time { return now }}
	draft, err := service.CreateDraft(ctx, Definition{Provider: "OPENWA", Channel: ChannelWhatsApp, Engine: "BAILEYS", AdapterVersion: "0.13.0", Capabilities: []Capability{CapabilitySendText}}, "maker", "create provider definition")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.Submit(ctx, draft.ID, draft.Version, "maker", "submit provider definition")
	if err != nil {
		t.Fatal(err)
	}
	active, err := service.Decide(ctx, pending.ID, pending.Version, true, "checker", "approve provider definition")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	retired, err := service.Retire(ctx, active.ID, active.Version, "checker", "retire obsolete provider route")
	if err != nil {
		t.Fatal(err)
	}
	if retired.Status != StatusRetired || retired.EffectiveTo == nil {
		t.Fatalf("unexpected retired definition: %+v", retired)
	}
	events, err := service.ListEvents(ctx, retired.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 lifecycle events, got %d", len(events))
	}
	if events[len(events)-1].Action != string(StatusRetired) {
		t.Fatalf("unexpected final event %+v", events[len(events)-1])
	}
}
