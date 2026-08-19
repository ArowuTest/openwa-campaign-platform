package provider

import (
	"context"
	"testing"
	"time"
)

type corruptActiveProviderStore struct{ *MemoryStore }

func (s corruptActiveProviderStore) Active(context.Context, string, Channel, string, time.Time) (Definition, error) {
	return Definition{
		Provider: "OPENWA", Channel: ChannelWhatsApp, Engine: "BAILEYS",
		AdapterVersion: "0.13.0", Capabilities: []Capability{CapabilitySendText},
		Status: StatusActive, EffectiveFrom: time.Now().Add(-time.Hour),
	}, nil
}

func TestRequireRejectsCorruptActiveProviderEvidence(t *testing.T) {
	svc := &Service{Store: corruptActiveProviderStore{MemoryStore: NewMemoryStore()}}
	if _, err := svc.Require(context.Background(), "OPENWA", ChannelWhatsApp, "BAILEYS", time.Now().UTC(), []Capability{CapabilitySendText}); err == nil {
		t.Fatal("active provider evidence with empty id/version was accepted")
	}
}
