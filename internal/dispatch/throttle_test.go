package dispatch

import (
	"context"
	"testing"
	"time"
)

func TestEvaluateThrottleReducesRateForCombinedPressure(t *testing.T) {
	decision, err := EvaluateThrottle(ThrottleSignals{
		ConfiguredMessagesPerMinute: 120,
		SenderHealthScore:           70,
		GatewayHealthScore:          90,
		RecentFailureRate:           0.10,
		QueueUtilisation:            0.90,
		ProviderLatency:             2 * time.Second,
		ProviderLatencyTarget:       time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.MessagesPerMinute >= 120 || decision.MessagesPerMinute < 1 {
		t.Fatalf("rate=%d", decision.MessagesPerMinute)
	}
	if len(decision.Reasons) != 4 {
		t.Fatalf("reasons=%v", decision.Reasons)
	}
}

func TestEvaluateThrottleKeepsHealthyConfiguredRate(t *testing.T) {
	decision, err := EvaluateThrottle(ThrottleSignals{
		ConfiguredMessagesPerMinute: 60,
		SenderHealthScore:           100,
		GatewayHealthScore:          100,
		ProviderLatency:             100 * time.Millisecond,
		ProviderLatencyTarget:       time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.MessagesPerMinute != 60 || decision.ReductionPercent != 0 {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestMemoryThrottleHonoursCancellation(t *testing.T) {
	throttle := NewMemoryThrottle(ThrottleDecision{MinimumInterval: time.Hour})
	if err := throttle.Wait(context.Background(), "session-1"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := throttle.Wait(ctx, "session-1"); err == nil {
		t.Fatal("expected cancellation")
	}
}
