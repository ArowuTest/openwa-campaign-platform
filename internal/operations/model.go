package operations

import (
	"errors"
	"strings"
	"time"
)

type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityWarning  Severity = "WARNING"
	SeverityCritical Severity = "CRITICAL"
)

type IncidentStatus string

const (
	IncidentOpen         IncidentStatus = "OPEN"
	IncidentAcknowledged IncidentStatus = "ACKNOWLEDGED"
	IncidentResolved     IncidentStatus = "RESOLVED"
)

type Incident struct {
	ID              string         `json:"id"`
	CampaignID      string         `json:"campaignId,omitempty"`
	SenderSessionID string         `json:"senderSessionId,omitempty"`
	Category        string         `json:"category"`
	Severity        Severity       `json:"severity"`
	Status          IncidentStatus `json:"status"`
	Summary         string         `json:"summary"`
	Detail          string         `json:"detail,omitempty"`
	OwnerID         string         `json:"ownerId,omitempty"`
	Resolution      string         `json:"resolution,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
	ResolvedAt      *time.Time     `json:"resolvedAt,omitempty"`
	Version         int64          `json:"version"`
}
type Dashboard struct {
	GeneratedAt       time.Time        `json:"generatedAt"`
	Campaigns         map[string]int64 `json:"campaigns"`
	Recipients        map[string]int64 `json:"recipients"`
	Senders           map[string]int64 `json:"senders"`
	OpenIncidents     int64            `json:"openIncidents"`
	CriticalIncidents int64            `json:"criticalIncidents"`
	UnknownOutcomes   int64            `json:"unknownOutcomes"`
	QueueDepth        int64            `json:"queueDepth"`
	OldestQueuedAt    *time.Time       `json:"oldestQueuedAt,omitempty"`
	StaleWorkerNodes  int64            `json:"staleWorkerNodes"`
}

type DeliveryException struct {
	RecipientID       string    `json:"recipientId"`
	CampaignID        string    `json:"campaignId"`
	Status            string    `json:"status"`
	AssignedSessionID string    `json:"assignedSessionId,omitempty"`
	ProviderMessageID string    `json:"providerMessageId,omitempty"`
	AttemptCount      int       `json:"attemptCount"`
	ErrorCode         string    `json:"errorCode,omitempty"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type CampaignPoolReport struct {
	SenderPoolID              string           `json:"senderPoolId"`
	SenderPoolName            string           `json:"senderPoolName"`
	GatewayPoolID             string           `json:"gatewayPoolId"`
	Provider                  string           `json:"provider"`
	Engine                    string           `json:"engine"`
	MaximumRecipients         int64            `json:"maximumRecipients"`
	ReservedMessagesPerMinute int64            `json:"reservedMessagesPerMinute"`
	ReservedHourlyUnits       int64            `json:"reservedHourlyUnits"`
	ReservedDailyUnits        int64            `json:"reservedDailyUnits"`
	Recipients                map[string]int64 `json:"recipients"`
}

type CampaignCommercialReport struct {
	Status             string     `json:"status"`
	QuotationReference string     `json:"quotationReference,omitempty"`
	InvoiceReference   string     `json:"invoiceReference,omitempty"`
	Currency           string     `json:"currency,omitempty"`
	ApprovedRecipients int64      `json:"approvedRecipients"`
	UnitPriceMinor     int64      `json:"unitPriceMinor"`
	ManagementFeeMinor int64      `json:"managementFeeMinor"`
	TotalAmountMinor   int64      `json:"totalAmountMinor"`
	PaymentReference   string     `json:"paymentReference,omitempty"`
	PaymentReceivedAt  *time.Time `json:"paymentReceivedAt,omitempty"`
}

type CampaignReport struct {
	CampaignID     string                   `json:"campaignId"`
	OrganisationID string                   `json:"organisationId"`
	Name           string                   `json:"name"`
	Purpose        string                   `json:"purpose"`
	Status         string                   `json:"status"`
	Audience       map[string]int64         `json:"audience"`
	Delivery       map[string]int64         `json:"delivery"`
	Engagement     map[string]int64         `json:"engagement"`
	Exceptions     map[string]int64         `json:"exceptions"`
	Commercial     CampaignCommercialReport `json:"commercial"`
	Pools          []CampaignPoolReport     `json:"pools"`
	Warnings       []string                 `json:"warnings"`
	StartedAt      *time.Time               `json:"startedAt,omitempty"`
	CompletedAt    *time.Time               `json:"completedAt,omitempty"`
	GeneratedAt    time.Time                `json:"generatedAt"`
}
type ExportStatus string

const (
	ExportDraft      ExportStatus = "DRAFT"
	ExportPending    ExportStatus = "PENDING_APPROVAL"
	ExportApproved   ExportStatus = "APPROVED"
	ExportProcessing ExportStatus = "PROCESSING"
	ExportRejected   ExportStatus = "REJECTED"
	ExportReady      ExportStatus = "READY"
	ExportExpired    ExportStatus = "EXPIRED"
	ExportFailed     ExportStatus = "FAILED"
)

type ExportRequest struct {
	ID              string       `json:"id"`
	Kind            string       `json:"kind"`
	ObjectID        string       `json:"objectId"`
	Format          string       `json:"format"`
	Status          ExportStatus `json:"status"`
	RequestedBy     string       `json:"requestedBy"`
	ApprovedBy      string       `json:"approvedBy,omitempty"`
	Reason          string       `json:"reason"`
	RejectionReason string       `json:"rejectionReason,omitempty"`
	CreatedAt       time.Time    `json:"createdAt"`
	UpdatedAt       time.Time    `json:"updatedAt"`
	ExpiresAt       *time.Time   `json:"expiresAt,omitempty"`
	ObjectKey       string       `json:"objectKey,omitempty"`
	ContentType     string       `json:"contentType,omitempty"`
	SHA256          string       `json:"sha256,omitempty"`
	SizeBytes       int64        `json:"sizeBytes,omitempty"`
	FailureCode     string       `json:"failureCode,omitempty"`
	FailureDetail   string       `json:"failureDetail,omitempty"`
	GeneratedAt     *time.Time   `json:"generatedAt,omitempty"`
	LeaseOwner      string       `json:"-"`
	LeaseExpiresAt  *time.Time   `json:"-"`
	Version         int64        `json:"version"`
}

var (
	ErrNotFound = errors.New("operations record not found")
	ErrConflict = errors.New("operations version conflict")
	ErrInvalid  = errors.New("invalid operations request")
)

func ValidateIncident(in Incident) error {
	if strings.TrimSpace(in.Category) == "" || strings.TrimSpace(in.Summary) == "" {
		return ErrInvalid
	}
	switch in.Severity {
	case SeverityInfo, SeverityWarning, SeverityCritical:
	default:
		return ErrInvalid
	}
	return nil
}
