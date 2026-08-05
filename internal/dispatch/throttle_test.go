package dispatch

import (
	"context"
	"testing"
	"time"

	"campaign-platform/internal/sender"
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

func TestDecisionFromPacingPolicyHonoursAdminWaitAndMediaOverride(t *testing.T) {
	policy := sender.PacingPolicy{MinimumDelayMS: 3000, MaximumDelayMS: 8000, JitterMode: sender.JitterUniform, MessagesPerMinute: 100, MaxInFlight: 1, HourlyAllowance: 500, DailyAllowance: 4000, MaxActiveCampaigns: 2, BurstSize: 1, Overrides: []sender.MessageTypeOverride{{MessageType: "VIDEO", MinimumDelayMS: 10000, MaximumDelayMS: 20000}}}
	decision, err := DecisionFromPacingPolicy(policy, "video", ThrottleSignals{SenderHealthScore: 100, GatewayHealthScore: 100})
	if err != nil {
		t.Fatal(err)
	}
	if decision.MinimumInterval != 10*time.Second || decision.MaximumInterval != 20*time.Second {
		t.Fatalf("unexpected governed wait %#v", decision)
	}
}

func TestMemoryThrottleUniformJitterStaysWithinPolicy(t *testing.T) {
	throttle := NewMemoryThrottle(ThrottleDecision{MinimumInterval: 3 * time.Second, MaximumInterval: 8 * time.Second, JitterMode: sender.JitterUniform})
	for i := 0; i < 20; i++ {
		d := throttle.intervalFor("session-1")
		if d < 3*time.Second || d > 8*time.Second {
			t.Fatalf("interval outside bounds: %s", d)
		}
	}
}

func TestPacingIntervalIsDeterministicAndBounded(t *testing.T) {
	first := pacingInterval("session-1", 4, 3000, 8000, sender.JitterUniform)
	second := pacingInterval("session-1", 4, 3000, 8000, sender.JitterUniform)
	if first != second {
		t.Fatalf("expected deterministic interval: %s != %s", first, second)
	}
	if first < 3*time.Second || first > 8*time.Second {
		t.Fatalf("interval outside configured bounds: %s", first)
	}
	if fixed := pacingInterval("session-1", 4, 5000, 5000, sender.JitterUniform); fixed != 5*time.Second {
		t.Fatalf("fixed interval=%s", fixed)
	}
}
