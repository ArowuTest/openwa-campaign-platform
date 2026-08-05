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
