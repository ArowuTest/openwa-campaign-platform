package execution

import (
	"errors"
	"math"
	"strings"
	"time"
)

type AdmissionDecision string

const (
	DecisionAdmit  AdmissionDecision = "ADMIT"
	DecisionHold   AdmissionDecision = "HOLD"
	DecisionReject AdmissionDecision = "REJECT"
)

type CapacityEvidence struct {
	CampaignID                 string            `json:"campaignId"`
	PoolID                     string            `json:"poolId"`
	EvidenceVersion            string            `json:"evidenceVersion"`
	RemainingRecipients        int64             `json:"remainingRecipients"`
	AvailableMessagesPerMinute int               `json:"availableMessagesPerMinute"`
	AvailableDailyCapacity     int64             `json:"availableDailyCapacity"`
	SafetyMarginPercent        int               `json:"safetyMarginPercent"`
	RequiredMessagesPerMinute  float64           `json:"requiredMessagesPerMinute"`
	EffectiveMessagesPerMinute float64           `json:"effectiveMessagesPerMinute"`
	ForecastCompletionAt       *time.Time        `json:"forecastCompletionAt,omitempty"`
	DeadlineAt                 time.Time         `json:"deadlineAt"`
	Decision                   AdmissionDecision `json:"decision"`
	Reasons                    []string          `json:"reasons"`
	EvaluatedAt                time.Time         `json:"evaluatedAt"`
}

type AdmissionInput struct {
	CampaignID                 string
	PoolID                     string
	EvidenceVersion            string
	RemainingRecipients        int64
	AvailableMessagesPerMinute int
	AvailableDailyCapacity     int64
	SafetyMarginPercent        int
	StartAt                    time.Time
	DeadlineAt                 time.Time
	Now                        time.Time
}

func EvaluateAdmission(in AdmissionInput) (CapacityEvidence, error) {
	if strings.TrimSpace(in.CampaignID) == "" || strings.TrimSpace(in.PoolID) == "" || strings.TrimSpace(in.EvidenceVersion) == "" {
		return CapacityEvidence{}, errors.New("campaign, pool and evidence version are required")
	}
	if in.RemainingRecipients < 0 || in.AvailableMessagesPerMinute < 0 || in.AvailableDailyCapacity < 0 {
		return CapacityEvidence{}, errors.New("capacity values cannot be negative")
	}
	if in.SafetyMarginPercent < 0 || in.SafetyMarginPercent > 90 {
		return CapacityEvidence{}, errors.New("safety margin must be between 0 and 90 percent")
	}
	if in.Now.IsZero() {
		in.Now = time.Now().UTC()
	}
	in.Now = in.Now.UTC()
	if in.StartAt.IsZero() || in.StartAt.Before(in.Now) {
		in.StartAt = in.Now
	}
	if in.DeadlineAt.IsZero() || !in.DeadlineAt.After(in.StartAt) {
		return CapacityEvidence{}, errors.New("deadline must be after effective start")
	}
	ev := CapacityEvidence{CampaignID: strings.TrimSpace(in.CampaignID), PoolID: strings.TrimSpace(in.PoolID), EvidenceVersion: strings.TrimSpace(in.EvidenceVersion), RemainingRecipients: in.RemainingRecipients, AvailableMessagesPerMinute: in.AvailableMessagesPerMinute, AvailableDailyCapacity: in.AvailableDailyCapacity, SafetyMarginPercent: in.SafetyMarginPercent, DeadlineAt: in.DeadlineAt.UTC(), EvaluatedAt: in.Now, Reasons: []string{}}
	minutes := in.DeadlineAt.Sub(in.StartAt).Minutes()
	ev.RequiredMessagesPerMinute = float64(in.RemainingRecipients) / minutes
	ev.EffectiveMessagesPerMinute = float64(in.AvailableMessagesPerMinute) * (1 - float64(in.SafetyMarginPercent)/100)
	if in.RemainingRecipients == 0 {
		t := in.StartAt.UTC()
		ev.ForecastCompletionAt = &t
		ev.Decision = DecisionAdmit
		return ev, nil
	}
	if in.AvailableDailyCapacity < in.RemainingRecipients {
		ev.Reasons = append(ev.Reasons, "INSUFFICIENT_DAILY_CAPACITY")
	}
	if ev.EffectiveMessagesPerMinute <= 0 {
		ev.Reasons = append(ev.Reasons, "NO_EFFECTIVE_THROUGHPUT")
	} else {
		d := time.Duration(math.Ceil(float64(in.RemainingRecipients)/ev.EffectiveMessagesPerMinute)) * time.Minute
		t := in.StartAt.Add(d).UTC()
		ev.ForecastCompletionAt = &t
		if t.After(in.DeadlineAt) {
			ev.Reasons = append(ev.Reasons, "DEADLINE_CAPACITY_SHORTFALL")
		}
	}
	if len(ev.Reasons) == 0 {
		ev.Decision = DecisionAdmit
	} else if in.AvailableMessagesPerMinute == 0 || in.AvailableDailyCapacity == 0 {
		ev.Decision = DecisionHold
	} else {
		ev.Decision = DecisionReject
	}
	return ev, nil
}

type CompletionState string

const (
	CompletionInProgress          CompletionState = "IN_PROGRESS"
	CompletionReady               CompletionState = "READY"
	CompletionReadyWithExceptions CompletionState = "READY_WITH_EXCEPTIONS"
)

type Metrics struct {
	Authorised, Queued, Submitted, Sent, Delivered, Read, Failed, Unknown, Suppressed int64
	Pending                                                                           int64
}
type CompletionAssessment struct {
	CampaignID  string          `json:"campaignId"`
	State       CompletionState `json:"state"`
	Terminal    int64           `json:"terminal"`
	Outstanding int64           `json:"outstanding"`
	Failed      int64           `json:"failed"`
	Unknown     int64           `json:"unknown"`
	AssessedAt  time.Time       `json:"assessedAt"`
}

func AssessCompletion(campaignID string, m Metrics, now time.Time) (CompletionAssessment, error) {
	if strings.TrimSpace(campaignID) == "" {
		return CompletionAssessment{}, errors.New("campaign ID is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	terminal := m.Sent + m.Delivered + m.Read + m.Failed + m.Unknown + m.Suppressed
	outstanding := m.Pending
	if outstanding == 0 && (m.Authorised > 0 || m.Queued > 0 || m.Submitted > 0) {
		outstanding = m.Authorised + m.Queued + m.Submitted
	}
	out := CompletionAssessment{CampaignID: campaignID, Terminal: terminal, Outstanding: outstanding, Failed: m.Failed, Unknown: m.Unknown, AssessedAt: now.UTC(), State: CompletionInProgress}
	if outstanding == 0 {
		if m.Failed > 0 || m.Unknown > 0 {
			out.State = CompletionReadyWithExceptions
		} else {
			out.State = CompletionReady
		}
	}
	return out, nil
}
