package dispatch

import (
	"context"
	"errors"
	"hash/fnv"
	"math"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/sender"
)

// ThrottleSignals are bounded operational inputs used to reduce send rate
// before a sender or gateway becomes unhealthy. Values outside the documented
// ranges are clamped rather than trusted.
type ThrottleSignals struct {
	ConfiguredMessagesPerMinute int
	SenderHealthScore           int     // 0..100
	GatewayHealthScore          int     // 0..100
	RecentFailureRate           float64 // 0..1
	QueueUtilisation            float64 // 0..1
	ProviderLatency             time.Duration
	ProviderLatencyTarget       time.Duration
}

type ThrottleDecision struct {
	MessagesPerMinute int               `json:"messagesPerMinute"`
	MinimumInterval   time.Duration     `json:"minimumInterval"`
	MaximumInterval   time.Duration     `json:"maximumInterval"`
	JitterMode        sender.JitterMode `json:"jitterMode"`
	BurstSize         int               `json:"burstSize"`
	ReductionPercent  int               `json:"reductionPercent"`
	Reasons           []string          `json:"reasons"`
}

func EvaluateThrottle(in ThrottleSignals) (ThrottleDecision, error) {
	if in.ConfiguredMessagesPerMinute <= 0 {
		return ThrottleDecision{}, errors.New("configured messages per minute must be positive")
	}
	health := minInt(clampInt(in.SenderHealthScore, 0, 100), clampInt(in.GatewayHealthScore, 0, 100))
	failure := clampFloat(in.RecentFailureRate, 0, 1)
	queue := clampFloat(in.QueueUtilisation, 0, 1)
	factor := 1.0
	reasons := make([]string, 0, 4)

	if health < 80 {
		factor *= math.Max(0.1, float64(health)/100)
		reasons = append(reasons, "HEALTH_DEGRADED")
	}
	if failure > 0.02 {
		factor *= math.Max(0.1, 1-(failure*2))
		reasons = append(reasons, "FAILURE_RATE_ELEVATED")
	}
	if queue > 0.8 {
		// High queue utilisation indicates downstream congestion. Reduce producer
		// pressure while preserving at least ten percent of configured capacity.
		factor *= math.Max(0.1, 1-((queue-0.8)*3))
		reasons = append(reasons, "QUEUE_PRESSURE_HIGH")
	}
	if in.ProviderLatencyTarget > 0 && in.ProviderLatency > in.ProviderLatencyTarget {
		ratio := float64(in.ProviderLatencyTarget) / float64(in.ProviderLatency)
		factor *= math.Max(0.1, ratio)
		reasons = append(reasons, "PROVIDER_LATENCY_HIGH")
	}
	if factor > 1 {
		factor = 1
	}
	rate := int(math.Floor(float64(in.ConfiguredMessagesPerMinute) * factor))
	if rate < 1 {
		rate = 1
	}
	interval := time.Minute / time.Duration(rate)
	return ThrottleDecision{
		MessagesPerMinute: rate,
		MinimumInterval:   interval,
		MaximumInterval:   interval,
		JitterMode:        sender.JitterNone,
		BurstSize:         1,
		ReductionPercent:  100 - int(math.Round(float64(rate)*100/float64(in.ConfiguredMessagesPerMinute))),
		Reasons:           reasons,
	}, nil
}

type Throttle interface {
	Wait(context.Context, string) error
}

// MemoryThrottle is a process-local pacing implementation suitable for one
// ordered session pipeline. Durable cross-process limits remain enforced by
// sender ownership and queue admission in PostgreSQL.
type MemoryThrottle struct {
	mu           sync.Mutex
	decision     ThrottleDecision
	nextByID     map[string]time.Time
	sequenceByID map[string]uint64
	clock        func() time.Time
}

func NewMemoryThrottle(decision ThrottleDecision) *MemoryThrottle {
	return &MemoryThrottle{decision: decision, nextByID: map[string]time.Time{}, sequenceByID: map[string]uint64{}}
}

func (t *MemoryThrottle) Wait(ctx context.Context, key string) error {
	if t == nil || t.decision.MinimumInterval <= 0 {
		return nil
	}
	now := time.Now().UTC()
	if t.clock != nil {
		now = t.clock().UTC()
	}
	t.mu.Lock()
	next := t.nextByID[key]
	if next.Before(now) {
		next = now
	}
	interval := t.intervalFor(key)
	t.nextByID[key] = next.Add(interval)
	t.mu.Unlock()
	if !next.After(now) {
		return nil
	}
	timer := time.NewTimer(next.Sub(now))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// DecisionFromPacingPolicy binds the active governed policy to the runtime throttle.
// Adaptive signals may reduce the configured rate, but never make the configured
// minimum wait shorter. Message-type overrides are applied before adaptive reduction.
func DecisionFromPacingPolicy(policy sender.PacingPolicy, messageType string, signals ThrottleSignals) (ThrottleDecision, error) {
	configuredMin := time.Duration(policy.MinimumDelayMS) * time.Millisecond
	configuredMax := time.Duration(policy.MaximumDelayMS) * time.Millisecond
	messageType = strings.ToUpper(strings.TrimSpace(messageType))
	for _, override := range policy.Overrides {
		if override.MessageType == messageType {
			configuredMin = time.Duration(override.MinimumDelayMS) * time.Millisecond
			configuredMax = time.Duration(override.MaximumDelayMS) * time.Millisecond
			break
		}
	}
	signals.ConfiguredMessagesPerMinute = policy.MessagesPerMinute
	decision, err := EvaluateThrottle(signals)
	if err != nil {
		return ThrottleDecision{}, err
	}
	if configuredMin > decision.MinimumInterval {
		decision.MinimumInterval = configuredMin
	}
	if configuredMax < decision.MinimumInterval {
		configuredMax = decision.MinimumInterval
	}
	decision.MaximumInterval = configuredMax
	decision.JitterMode = policy.JitterMode
	decision.BurstSize = policy.BurstSize
	return decision, nil
}

func (t *MemoryThrottle) intervalFor(key string) time.Duration {
	min := t.decision.MinimumInterval
	max := t.decision.MaximumInterval
	if max <= min || t.decision.JitterMode != sender.JitterUniform {
		return min
	}
	seq := t.sequenceByID[key]
	t.sequenceByID[key] = seq + 1
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	var b [8]byte
	for i := 0; i < 8; i++ {
		b[i] = byte(seq >> (8 * i))
	}
	_, _ = h.Write(b[:])
	span := uint64(max - min)
	if span == 0 {
		return min
	}
	return min + time.Duration(h.Sum64()%(span+1))
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func clampFloat(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
