package execution

import (
	"testing"
	"time"
)

func TestAdmissionHonoursSafetyMarginAndDeadline(t *testing.T) {
	now := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	ev, err := EvaluateAdmission(AdmissionInput{CampaignID: "c", PoolID: "p", EvidenceVersion: "v1", RemainingRecipients: 5400, AvailableMessagesPerMinute: 100, AvailableDailyCapacity: 10000, SafetyMarginPercent: 10, StartAt: now, DeadlineAt: now.Add(time.Hour), Now: now})
	if err != nil || ev.Decision != DecisionAdmit || ev.EffectiveMessagesPerMinute != 90 {
		t.Fatalf("%+v %v", ev, err)
	}
	ev, err = EvaluateAdmission(AdmissionInput{CampaignID: "c", PoolID: "p", EvidenceVersion: "v1", RemainingRecipients: 6000, AvailableMessagesPerMinute: 100, AvailableDailyCapacity: 10000, SafetyMarginPercent: 10, StartAt: now, DeadlineAt: now.Add(time.Hour), Now: now})
	if err != nil || ev.Decision != DecisionReject {
		t.Fatalf("%+v %v", ev, err)
	}
}
func TestCompletionUsesMutuallyExclusiveTerminalCounts(t *testing.T) {
	a, err := AssessCompletion("c", Metrics{Sent: 10, Delivered: 10, Read: 70, Failed: 5, Unknown: 3, Suppressed: 2, Pending: 0}, time.Now())
	if err != nil || a.Outstanding != 0 || a.State != CompletionReadyWithExceptions || a.Terminal != 100 {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestExecutionForecastClassifiesDeadlineAndUnknownRisk(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	completion := now.Add(70 * time.Minute)
	forecast := BuildExecutionForecast(CapacityEvidence{
		CampaignID: "campaign-1", RemainingRecipients: 6000,
		EffectiveMessagesPerMinute: 90, AvailableDailyCapacity: 10000,
		ForecastCompletionAt: &completion, DeadlineAt: now.Add(time.Hour),
		Decision: DecisionReject, Reasons: []string{"DEADLINE_CAPACITY_SHORTFALL"}, EvaluatedAt: now,
	}, Metrics{Unknown: 2, Failed: 1})
	if forecast.Risk != ForecastRiskHigh {
		t.Fatalf("risk=%s", forecast.Risk)
	}
	if forecast.DeadlineSlackMinutes != -10 {
		t.Fatalf("slack=%d", forecast.DeadlineSlackMinutes)
	}
	if len(forecast.Bottlenecks) != 3 {
		t.Fatalf("bottlenecks=%v", forecast.Bottlenecks)
	}
}

func TestExecutionForecastBlocksWithoutThroughput(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	forecast := BuildExecutionForecast(CapacityEvidence{
		CampaignID: "campaign-1", RemainingRecipients: 100,
		AvailableDailyCapacity: 1000, DeadlineAt: now.Add(time.Hour),
		Decision: DecisionHold, Reasons: []string{"NO_EFFECTIVE_THROUGHPUT"}, EvaluatedAt: now,
	}, Metrics{})
	if forecast.Risk != ForecastRiskBlocked {
		t.Fatalf("risk=%s", forecast.Risk)
	}
}
