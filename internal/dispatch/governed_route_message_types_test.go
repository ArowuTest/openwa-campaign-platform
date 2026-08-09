package dispatch

import (
	"strings"
	"testing"
	"time"
)

func TestGovernedRouteRequiresCapabilityForEverySupportedMessageType(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		messageType string
		capability  string
	}{
		{"text", "SEND_TEXT"},
		{"image", "SEND_IMAGE"},
		{"video", "SEND_VIDEO"},
		{"document", "SEND_DOCUMENT"},
	}
	for _, tc := range cases {
		t.Run(tc.messageType, func(t *testing.T) {
			value := validGovernedRouteEvidence()
			value.GatewayCapabilities = []string{tc.capability, "DELIVERY_EVENTS"}
			value.DefinitionCapabilities = []string{tc.capability, "DELIVERY_EVENTS"}
			if err := validateGovernedRouteEvidence(value, tc.messageType, now); err != nil {
				t.Fatalf("supported %s route rejected: %v", tc.messageType, err)
			}
			value.GatewayCapabilities = []string{"DELIVERY_EVENTS"}
			if err := validateGovernedRouteEvidence(value, tc.messageType, now); err == nil || !strings.Contains(err.Error(), tc.capability) {
				t.Fatalf("missing %s was not rejected: %v", tc.capability, err)
			}
		})
	}
}
