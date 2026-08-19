package campaign

import "testing"

func validTransport() TransportSelection {
	return TransportSelection{Channel: "WHATSAPP", Provider: ProviderOpenWA, Engine: EngineBaileys, RoutingMode: RoutingSenderPool, GatewayPoolID: "openwa-baileys-ng", SenderPoolID: "11111111-1111-1111-1111-111111111111", AdapterVersion: "0.13.0+platform.1", FallbackMode: FallbackNone, RoutingPolicyVersion: "route-v1", CapacityEvidenceVersion: "cap-v1", RequiredCapabilities: []string{"send_text", "delivery_events"}}
}
func TestTransportSelectionRejectsSilentFallbackAndMixedRouting(t *testing.T) {
	v := validTransport()
	v.FallbackMode = "AUTO"
	if v.Validate() == nil {
		t.Fatal("fallback accepted")
	}
	v = validTransport()
	v.SessionID = "22222222-2222-2222-2222-222222222222"
	if v.Validate() == nil {
		t.Fatal("mixed route accepted")
	}
}
func TestTransportSelectionAllowsExplicitOpenWAEnginePool(t *testing.T) {
	if err := validTransport().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTransportSelectionAllowsMetaCloudSenderPool(t *testing.T) {
	v := TransportSelection{
		Channel: "WHATSAPP", Provider: ProviderMeta, Engine: EngineMetaCloud,
		RoutingMode: RoutingSenderPool, SenderPoolID: "11111111-1111-1111-1111-111111111111",
		MetaSenderID:   "22222222-2222-2222-2222-222222222222",
		AdapterVersion: "1.0.0", FallbackMode: FallbackNone,
		RoutingPolicyVersion: "route-v2", CapacityEvidenceVersion: "cap-v2",
		RequiredCapabilities: []string{"SEND_TEMPLATE"},
	}
	if err := v.Validate(); err != nil {
		t.Fatalf("Meta Cloud transport rejected: %v", err)
	}
}

func TestTransportSelectionRejectsProviderEngineMismatch(t *testing.T) {
	v := validTransport()
	v.Provider = ProviderMeta
	if v.Validate() == nil {
		t.Fatal("META with BAILEYS engine accepted")
	}
	v = validTransport()
	v.Engine = EngineMetaCloud
	if v.Validate() == nil {
		t.Fatal("OPENWA with CLOUD_API engine accepted")
	}
}

func TestTransportSelectionRejectsNonCanonicalProviderEngineOrChannel(t *testing.T) {
	for name, mutate := range map[string]func(*TransportSelection){
		"channel":  func(v *TransportSelection) { v.Channel = "whatsapp" },
		"provider": func(v *TransportSelection) { v.Provider = Provider("openwa") },
		"engine":   func(v *TransportSelection) { v.Engine = Engine("baileys") },
	} {
		t.Run(name, func(t *testing.T) {
			v := validTransport()
			mutate(&v)
			if err := v.Validate(); err == nil {
				t.Fatalf("non-canonical %s was accepted: %+v", name, v)
			}
		})
	}
}
