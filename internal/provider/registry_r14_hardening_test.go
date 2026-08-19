package provider

import (
	"context"
	"testing"
	"time"
)

func TestRetireFutureEffectiveDefinitionDoesNotInvertWindow(t *testing.T) {
	now := time.Date(2026, 8, 17, 2, 30, 0, 0, time.UTC)
	service := &Service{Store: NewMemoryStore(), Clock: func() time.Time { return now }}
	future := now.Add(24 * time.Hour)
	draft, err := service.CreateDraft(context.Background(), Definition{
		Provider: "OPENWA", Channel: ChannelWhatsApp, Engine: "BAILEYS", AdapterVersion: "1.0.0",
		Capabilities: []Capability{CapabilitySendText}, EffectiveFrom: future,
	}, "creator", "future provider draft")
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := service.Submit(context.Background(), draft.ID, draft.Version, "submitter", "submit future provider")
	if err != nil {
		t.Fatal(err)
	}
	active, err := service.Decide(context.Background(), submitted.ID, submitted.Version, true, "checker", "approve future provider")
	if err != nil {
		t.Fatal(err)
	}
	retired, err := service.Retire(context.Background(), active.ID, active.Version, "retirer", "retire before effective start")
	if err != nil {
		t.Fatal(err)
	}
	if retired.EffectiveTo != nil && !retired.EffectiveTo.After(retired.EffectiveFrom) {
		t.Fatalf("retire inverted effective window: from=%s to=%s", retired.EffectiveFrom, retired.EffectiveTo)
	}
	if _, err := service.Store.Active(context.Background(), "OPENWA", ChannelWhatsApp, "BAILEYS", future.Add(time.Hour)); err != ErrNotFound {
		t.Fatalf("retired future definition remained active: %v", err)
	}
}
