package operations

import (
	"encoding/json"
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
	IncidentOpen          IncidentStatus = "OPEN"
	IncidentAcknowledged  IncidentStatus = "ACKNOWLEDGED"
	IncidentInvestigating IncidentStatus = "INVESTIGATING"
	IncidentMitigated     IncidentStatus = "MITIGATED"
	IncidentResolved      IncidentStatus = "RESOLVED"
	IncidentClosed        IncidentStatus = "CLOSED"
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
	GeneratedAt                              time.Time        `json:"generatedAt"`
	Campaigns                                map[string]int64 `json:"campaigns"`
	Recipients                               map[string]int64 `json:"recipients"`
	Senders                                  map[string]int64 `json:"senders"`
	OpenIncidents                            int64            `json:"openIncidents"`
	CriticalIncidents                        int64            `json:"criticalIncidents"`
	UnknownOutcomes                          int64            `json:"unknownOutcomes"`
	QueueDepth                               int64            `json:"queueDepth"`
	OldestQueuedAt                           *time.Time       `json:"oldestQueuedAt,omitempty"`
	StaleWorkerNodes                         int64            `json:"staleWorkerNodes"`
	UnavailableGatewayNodes                  int64            `json:"unavailableGatewayNodes"`
	GatewayStaleAfterSeconds                 int64            `json:"gatewayStaleAfterSeconds"`
	GatewayRuntimeHealthSource               string           `json:"gatewayRuntimeHealthSource"`
	GatewayRuntimeHealthConfigurationID      string           `json:"gatewayRuntimeHealthConfigurationId,omitempty"`
	GatewayRuntimeHealthConfigurationVersion int64            `json:"gatewayRuntimeHealthConfigurationVersion,omitempty"`
	UnhealthySenderSessions                  int64            `json:"unhealthySenderSessions"`
	CampaignsAtRisk                          int64            `json:"campaignsAtRisk"`
	CapacityShortfallPools                   int64            `json:"capacityShortfallPools"`
	ReconciliationBacklog                    int64            `json:"reconciliationBacklog"`
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

type ReportBreakdownCell struct {
	Label      string `json:"label"`
	Count      *int64 `json:"count,omitempty"`
	Suppressed bool   `json:"suppressed"`
}

type ReportPrivacyEvidence struct {
	PolicyID            string    `json:"policyId"`
	PolicyVersion       int64     `json:"policyVersion"`
	MinimumCohortSize   int       `json:"minimumCohortSize"`
	SuppressionLabel    string    `json:"suppressionLabel"`
	SuppressedCellCount int       `json:"suppressedCellCount"`
	AppliedAt           time.Time `json:"appliedAt"`
}

type CampaignReport struct {
	CampaignID     string                           `json:"campaignId"`
	OrganisationID string                           `json:"organisationId"`
	Name           string                           `json:"name"`
	Purpose        string                           `json:"purpose"`
	Status         string                           `json:"status"`
	Audience       map[string]int64                 `json:"audience"`
	Delivery       map[string]int64                 `json:"delivery"`
	Engagement     map[string]int64                 `json:"engagement"`
	Exceptions     map[string]int64                 `json:"exceptions"`
	Commercial     CampaignCommercialReport         `json:"commercial"`
	Pools          []CampaignPoolReport             `json:"pools"`
	Warnings       []string                         `json:"warnings"`
	Breakdowns     map[string][]ReportBreakdownCell `json:"breakdowns,omitempty"`
	RawBreakdowns  map[string]map[string]int64      `json:"-"`
	Privacy        ReportPrivacyEvidence            `json:"privacy"`
	StartedAt      *time.Time                       `json:"startedAt,omitempty"`
	CompletedAt    *time.Time                       `json:"completedAt,omitempty"`
	GeneratedAt    time.Time                        `json:"generatedAt"`
}

type CurrencyCommercialSummary struct {
	Currency            string `json:"currency"`
	Campaigns           int64  `json:"campaigns"`
	ApprovedRecipients  int64  `json:"approvedRecipients"`
	ApprovedAmountMinor int64  `json:"approvedAmountMinor"`
}

type OrganisationPerformanceReport struct {
	OrganisationID   string                           `json:"organisationId"`
	OrganisationName string                           `json:"organisationName"`
	Campaigns        map[string]int64                 `json:"campaigns"`
	Recipients       map[string]int64                 `json:"recipients"`
	Delivery         map[string]int64                 `json:"delivery"`
	Commercial       []CurrencyCommercialSummary      `json:"commercial"`
	Warnings         []string                         `json:"warnings"`
	Breakdowns       map[string][]ReportBreakdownCell `json:"breakdowns,omitempty"`
	RawBreakdowns    map[string]map[string]int64      `json:"-"`
	Privacy          ReportPrivacyEvidence            `json:"privacy"`
	GeneratedAt      time.Time                        `json:"generatedAt"`
}

type FinancialReconciliationStatus string

const (
	FinancialReconciliationBalanced          FinancialReconciliationStatus = "BALANCED"
	FinancialReconciliationUnderAllocated    FinancialReconciliationStatus = "UNDER_ALLOCATED"
	FinancialReconciliationOverAllocated     FinancialReconciliationStatus = "OVER_ALLOCATED"
	FinancialReconciliationPaymentMissing    FinancialReconciliationStatus = "PAYMENT_MISSING"
	FinancialReconciliationCommercialMissing FinancialReconciliationStatus = "COMMERCIAL_EVIDENCE_MISSING"
)

type CampaignFinancialReconciliation struct {
	CampaignID           string                        `json:"campaignId"`
	OrganisationID       string                        `json:"organisationId"`
	CampaignName         string                        `json:"campaignName"`
	CampaignStatus       string                        `json:"campaignStatus"`
	CommercialStatus     string                        `json:"commercialStatus,omitempty"`
	Currency             string                        `json:"currency,omitempty"`
	QuotationReference   string                        `json:"quotationReference,omitempty"`
	InvoiceReference     string                        `json:"invoiceReference,omitempty"`
	PaymentReference     string                        `json:"paymentReference,omitempty"`
	PaymentReceivedAt    *time.Time                    `json:"paymentReceivedAt,omitempty"`
	ApprovedRecipients   int64                         `json:"approvedRecipients"`
	RecipientObligations int64                         `json:"recipientObligations"`
	ProviderAccepted     int64                         `json:"providerAccepted"`
	Sent                 int64                         `json:"sent"`
	Delivered            int64                         `json:"delivered"`
	Read                 int64                         `json:"read"`
	Failed               int64                         `json:"failed"`
	Unknown              int64                         `json:"unknown"`
	ApprovedAmountMinor  int64                         `json:"approvedAmountMinor"`
	RecipientVariance    int64                         `json:"recipientVariance"`
	ReconciliationStatus FinancialReconciliationStatus `json:"reconciliationStatus"`
	Warnings             []string                      `json:"warnings"`
	GeneratedAt          time.Time                     `json:"generatedAt"`
}

type ExportStatus string

const (
	ExportDraft      ExportStatus = "DRAFT"
	ExportPending    ExportStatus = "PENDING_APPROVAL"
	ExportApproved   ExportStatus = "APPROVED"
	ExportProcessing ExportStatus = "PROCESSING"
	ExportRejected   ExportStatus = "REJECTED"
	ExportReady      ExportStatus = "READY"
	ExportExpiring   ExportStatus = "EXPIRING"
	ExportExpired    ExportStatus = "EXPIRED"
	ExportFailed     ExportStatus = "FAILED"
	ExportRevoked    ExportStatus = "REVOKED"
)

type ExportRequest struct {
	ID                string          `json:"id"`
	Kind              string          `json:"kind"`
	ObjectID          string          `json:"objectId"`
	Format            string          `json:"format"`
	Status            ExportStatus    `json:"status"`
	RequestedBy       string          `json:"requestedBy"`
	ApprovedBy        string          `json:"approvedBy,omitempty"`
	Reason            string          `json:"reason"`
	RejectionReason   string          `json:"rejectionReason,omitempty"`
	Criteria          json.RawMessage `json:"criteria,omitempty"`
	TemplateVersion   string          `json:"templateVersion"`
	AsOf              *time.Time      `json:"asOf,omitempty"`
	FrozenPayload     json.RawMessage `json:"-"`
	AuditHeadSequence uint64          `json:"auditHeadSequence,omitempty"`
	AuditHeadHash     string          `json:"auditHeadHash,omitempty"`
	WatermarkText     string          `json:"watermarkText,omitempty"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
	ExpiresAt         *time.Time      `json:"expiresAt,omitempty"`
	ObjectKey         string          `json:"objectKey,omitempty"`
	ContentType       string          `json:"contentType,omitempty"`
	SHA256            string          `json:"sha256,omitempty"`
	SizeBytes         int64           `json:"sizeBytes,omitempty"`
	FailureCode       string          `json:"failureCode,omitempty"`
	FailureDetail     string          `json:"failureDetail,omitempty"`
	GeneratedAt       *time.Time      `json:"generatedAt,omitempty"`
	DownloadCount     int64           `json:"downloadCount"`
	LastDownloadedAt  *time.Time      `json:"lastDownloadedAt,omitempty"`
	RevokedAt         *time.Time      `json:"revokedAt,omitempty"`
	RevokedBy         string          `json:"revokedBy,omitempty"`
	RevocationReason  string          `json:"revocationReason,omitempty"`
	LeaseOwner        string          `json:"-"`
	LeaseExpiresAt    *time.Time      `json:"-"`
	Version           int64           `json:"version"`
}

type ExportQuery struct {
	AfterCreatedAt *time.Time
	AfterID        string
	Status         ExportStatus
	Kind           string
	RequestedBy    string
	Limit          int
}

type ExportPage struct {
	Items     []ExportRequest `json:"items"`
	NextAfter string          `json:"nextAfter,omitempty"`
}

type DownloadGrant struct {
	ID        string     `json:"id"`
	ExportID  string     `json:"exportId"`
	ActorID   string     `json:"actorId"`
	TokenHash string     `json:"-"`
	RequestID string     `json:"requestId"`
	ExpiresAt time.Time  `json:"expiresAt"`
	UsedAt    *time.Time `json:"usedAt,omitempty"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

type DownloadAuthorization struct {
	Grant DownloadGrant `json:"grant"`
	Token string        `json:"token"`
}

var (
	ErrNotFound         = errors.New("operations record not found")
	ErrConflict         = errors.New("operations version conflict")
	ErrInvalid          = errors.New("invalid operations request")
	ErrApprovalRequired = errors.New("explicit duplicate-risk approval is required")
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
