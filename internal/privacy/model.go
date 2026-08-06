package privacy

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type CaseType string

const (
	CaseAccess        CaseType = "ACCESS"
	CaseRectification CaseType = "RECTIFICATION"
	CaseErasure       CaseType = "ERASURE"
	CasePortability   CaseType = "PORTABILITY"
	CaseObjection     CaseType = "OBJECTION"
	CaseRestriction   CaseType = "RESTRICTION"
)

type CaseStatus string

const (
	StatusOpen            CaseStatus = "OPEN"
	StatusAssigned        CaseStatus = "ASSIGNED"
	StatusPendingApproval CaseStatus = "PENDING_APPROVAL"
	StatusApproved        CaseStatus = "APPROVED"
	StatusRejected        CaseStatus = "REJECTED"
	StatusInProgress      CaseStatus = "IN_PROGRESS"
	StatusCompleted       CaseStatus = "COMPLETED"
	StatusCancelled       CaseStatus = "CANCELLED"
)

type Case struct {
	ID                string          `json:"id"`
	Type              CaseType        `json:"type"`
	Status            CaseStatus      `json:"status"`
	SubjectLookupHMAC []byte          `json:"-"`
	SubjectMasked     string          `json:"subjectMasked"`
	ContactID         string          `json:"contactId,omitempty"`
	OrganisationID    string          `json:"organisationId,omitempty"`
	RequestedAt       time.Time       `json:"requestedAt"`
	DueAt             time.Time       `json:"dueAt"`
	AssignedTo        string          `json:"assignedTo,omitempty"`
	CreatedBy         string          `json:"createdBy"`
	SubmittedBy       string          `json:"submittedBy,omitempty"`
	DecidedBy         string          `json:"decidedBy,omitempty"`
	ExecutedBy        string          `json:"executedBy,omitempty"`
	RequestReason     string          `json:"requestReason"`
	DecisionReason    string          `json:"decisionReason,omitempty"`
	ExecutionReason   string          `json:"executionReason,omitempty"`
	RequestedChanges  json.RawMessage `json:"requestedChanges,omitempty"`
	ResultCiphertext  []byte          `json:"-"`
	ResultKeyVersion  string          `json:"-"`
	ResultSHA256      string          `json:"resultSha256,omitempty"`
	CompletedAt       *time.Time      `json:"completedAt,omitempty"`
	RejectedAt        *time.Time      `json:"rejectedAt,omitempty"`
	CancelledAt       *time.Time      `json:"cancelledAt,omitempty"`
	Version           int64           `json:"version"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

type Event struct {
	ID          string          `json:"id"`
	CaseID      string          `json:"caseId"`
	Type        string          `json:"type"`
	ActorID     string          `json:"actorId"`
	Reason      string          `json:"reason,omitempty"`
	CaseVersion int64           `json:"caseVersion"`
	Evidence    json.RawMessage `json:"evidence,omitempty"`
	OccurredAt  time.Time       `json:"occurredAt"`
}

type HoldStatus string

const (
	HoldDraft           HoldStatus = "DRAFT"
	HoldPendingApproval HoldStatus = "PENDING_APPROVAL"
	HoldActive          HoldStatus = "ACTIVE"
	HoldRejected        HoldStatus = "REJECTED"
	HoldReleased        HoldStatus = "RELEASED"
)

type LegalHold struct {
	ID                string     `json:"id"`
	SubjectLookupHMAC []byte     `json:"-"`
	ContactID         string     `json:"contactId,omitempty"`
	OrganisationID    string     `json:"organisationId,omitempty"`
	Scope             string     `json:"scope"`
	Status            HoldStatus `json:"status"`
	Reason            string     `json:"reason"`
	CreatedBy         string     `json:"createdBy"`
	SubmittedBy       string     `json:"submittedBy,omitempty"`
	DecidedBy         string     `json:"decidedBy,omitempty"`
	DecisionReason    string     `json:"decisionReason,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	ActivatedAt       *time.Time `json:"activatedAt,omitempty"`
	RejectedAt        *time.Time `json:"rejectedAt,omitempty"`
	ExpiresAt         *time.Time `json:"expiresAt,omitempty"`
	ReleasedAt        *time.Time `json:"releasedAt,omitempty"`
	ReleasedBy        string     `json:"releasedBy,omitempty"`
	ReleaseReason     string     `json:"releaseReason,omitempty"`
	Version           int64      `json:"version"`
}

type LegalHoldEvent struct {
	ID          string          `json:"id"`
	LegalHoldID string          `json:"legalHoldId"`
	Type        string          `json:"type"`
	ActorID     string          `json:"actorId"`
	Reason      string          `json:"reason,omitempty"`
	HoldVersion int64           `json:"holdVersion"`
	Evidence    json.RawMessage `json:"evidence,omitempty"`
	OccurredAt  time.Time       `json:"occurredAt"`
}

type Query struct {
	AfterCreatedAt *time.Time
	AfterID        string
	Status         CaseStatus
	Type           CaseType
	AssignedTo     string
	Limit          int
}

type Page struct {
	Items     []Case `json:"items"`
	NextAfter string `json:"nextAfter,omitempty"`
}

type SubjectPackage struct {
	ContactID            string           `json:"contactId"`
	EncryptedMSISDN      []byte           `json:"-"`
	MaskedMSISDN         string           `json:"maskedMsisdn"`
	Status               string           `json:"status"`
	ProcessingRestricted bool             `json:"processingRestricted"`
	Profile              map[string]any   `json:"profile"`
	Attributes           []map[string]any `json:"attributes"`
	Consents             []map[string]any `json:"consents"`
	Suppressions         []map[string]any `json:"suppressions"`
	CampaignHistory      []map[string]any `json:"campaignHistory"`
	GeneratedAt          time.Time        `json:"generatedAt"`
}

type EncryptedPackageEnvelope struct {
	CaseID          string `json:"caseId"`
	Ciphertext      []byte `json:"ciphertext"`
	KeyVersion      string `json:"keyVersion"`
	PlaintextSHA256 string `json:"plaintextSha256"`
}

type CreateInput struct {
	Type             CaseType
	MSISDN           string
	OrganisationID   string
	Reason           string
	RequestedChanges any
	DueAt            *time.Time
	CreatedBy        string
}

var (
	ErrNotFound  = errors.New("privacy record not found")
	ErrConflict  = errors.New("privacy record version conflict")
	ErrInvalid   = errors.New("invalid privacy request")
	ErrLegalHold = errors.New("privacy action is blocked by an active legal hold")
)

func validType(value CaseType) bool {
	switch value {
	case CaseAccess, CaseRectification, CaseErasure, CasePortability, CaseObjection, CaseRestriction:
		return true
	default:
		return false
	}
}

func validHoldScope(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "ALL", "CONTACT", "CONSENT", "CAMPAIGN", "AUDIT", "COMMERCIAL":
		return true
	default:
		return false
	}
}
