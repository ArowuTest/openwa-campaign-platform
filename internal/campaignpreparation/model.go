package campaignpreparation

import (
	"campaign-platform/internal/campaign"
	"errors"
	"time"
)

var (
	ErrUnavailable              = errors.New("campaign readiness is unavailable")
	ErrStageUnsupported         = errors.New("campaign readiness stage is unsupported")
	ErrInvalidEvidence          = errors.New("campaign readiness evidence is invalid")
	ErrRouteIdentityUnavailable = errors.New("capacity route identity is unavailable")
)

const MaxSafeInteger int64 = 9007199254740991
const AdvisoryLimitation = "Readiness is advisory; final approval revalidates current evidence."
const ReservationLimitation = "These live capacity figures do not assess competing campaign reservations over the saved window. They do not establish reserved-window or deadline feasibility."
const ReservationReason = "RESERVATION_WINDOW_FEASIBILITY_NOT_ASSESSED"

type EvidenceReference struct {
	Kind       string     `json:"kind"`
	ID         string     `json:"id"`
	Version    int64      `json:"version,omitempty"`
	Hash       string     `json:"hash,omitempty"`
	Status     string     `json:"status,omitempty"`
	ObservedAt *time.Time `json:"observedAt,omitempty"`
}
type ReadinessCheck struct {
	Key         string              `json:"key"`
	Status      string              `json:"status"`
	Code        string              `json:"code"`
	Message     string              `json:"message"`
	Remediation string              `json:"remediation"`
	Evidence    []EvidenceReference `json:"evidence"`
}
type CapacityPreview struct {
	SenderPoolID               string     `json:"senderPoolId"`
	GatewayPoolID              string     `json:"gatewayPoolId"`
	HealthySessions            int        `json:"healthySessions"`
	HealthyNodes               int        `json:"healthyNodes"`
	MinimumHealthyNodes        int        `json:"minimumHealthyNodes"`
	AvailableMessagesPerMinute int        `json:"availableMessagesPerMinute"`
	AvailableHourlyUnits       int64      `json:"availableHourlyUnits"`
	AvailableDailyUnits        int64      `json:"availableDailyUnits"`
	SafetyMarginPercent        int        `json:"safetyMarginPercent"`
	BasisRecipientCount        int64      `json:"basisRecipientCount"`
	Basis                      string     `json:"basis"`
	RequiredMessagesPerMinute  *float64   `json:"requiredMessagesPerMinute,omitempty"`
	ProjectedCompletionAt      *time.Time `json:"projectedCompletionAt,omitempty"`
	MeasurementAsOf            time.Time  `json:"measurementAsOf"`
	Classification             string     `json:"classification"`
	Reasons                    []string   `json:"reasons"`
}
type Readiness struct {
	CampaignID          string           `json:"campaignId"`
	CampaignVersion     int64            `json:"campaignVersion"`
	CampaignStatus      campaign.Status  `json:"campaignStatus"`
	AssessedAt          time.Time        `json:"assessedAt"`
	EffectiveStartAt    *time.Time       `json:"effectiveStartAt,omitempty"`
	State               string           `json:"state"`
	ReadyForFinalReview bool             `json:"readyForFinalReview"`
	Checks              []ReadinessCheck `json:"checks"`
	Capacity            *CapacityPreview `json:"capacity,omitempty"`
	Limitations         []string         `json:"limitations"`
}
