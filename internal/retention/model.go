package retention

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("retention record not found")
	ErrConflict = errors.New("retention version conflict")
	ErrInvalid  = errors.New("retention record is invalid")
)

type ObjectType string

const (
	ObjectInboundContent       ObjectType = "INBOUND_CONTENT"
	ObjectAudienceImportSource ObjectType = "AUDIENCE_IMPORT_SOURCE"
	ObjectExportObject         ObjectType = "EXPORT_OBJECT"
	ObjectProviderEvent        ObjectType = "PROVIDER_EVENT"
	ObjectDeliveryEvent        ObjectType = "DELIVERY_EVENT"
	ObjectAuditEvent           ObjectType = "AUDIT_EVENT"
	ObjectIncident             ObjectType = "INCIDENT"
	ObjectPrivacyCase          ObjectType = "PRIVACY_CASE"
)

type Action string

const (
	ActionArchive            Action = "ARCHIVE"
	ActionDelete             Action = "DELETE"
	ActionAnonymise          Action = "ANONYMISE"
	ActionReviewRequired     Action = "REVIEW_REQUIRED"
	ActionRetainIndefinitely Action = "RETAIN_INDEFINITELY"
)

type ScopeType string

const (
	ScopePlatform     ScopeType = "PLATFORM"
	ScopeOrganisation ScopeType = "ORGANISATION"
)

type Status string

const (
	StatusDraft           Status = "DRAFT"
	StatusPendingApproval Status = "PENDING_APPROVAL"
	StatusActive          Status = "ACTIVE"
	StatusRejected        Status = "REJECTED"
	StatusRetired         Status = "RETIRED"
)

type Policy struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	ObjectType        ObjectType `json:"objectType"`
	Action            Action     `json:"action"`
	ScopeType         ScopeType  `json:"scopeType"`
	ScopeID           string     `json:"scopeId,omitempty"`
	RetentionDays     int        `json:"retentionDays,omitempty"`
	RespectLegalHolds bool       `json:"respectLegalHolds"`
	Status            Status     `json:"status"`
	EffectiveFrom     time.Time  `json:"effectiveFrom"`
	EffectiveTo       *time.Time `json:"effectiveTo,omitempty"`
	CreatedBy         string     `json:"createdBy"`
	SubmittedBy       string     `json:"submittedBy,omitempty"`
	ApprovedBy        string     `json:"approvedBy,omitempty"`
	Reason            string     `json:"reason"`
	Version           int64      `json:"version"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type Event struct {
	ID         string         `json:"id"`
	PolicyID   string         `json:"policyId"`
	EventType  string         `json:"eventType"`
	Version    int64          `json:"version"`
	ActorID    string         `json:"actorId"`
	Reason     string         `json:"reason"`
	Evidence   map[string]any `json:"evidence,omitempty"`
	OccurredAt time.Time      `json:"occurredAt"`
}

type JobStatus string

const (
	JobPending    JobStatus = "PENDING"
	JobClaimed    JobStatus = "CLAIMED"
	JobCompleted  JobStatus = "COMPLETED"
	JobFailed     JobStatus = "FAILED"
	JobHeldReview JobStatus = "HELD_REVIEW"
)

type Job struct {
	ID                 string         `json:"id"`
	PolicyID           string         `json:"policyId"`
	ObjectType         ObjectType     `json:"objectType"`
	ObjectID           string         `json:"objectId"`
	ObjectKey          string         `json:"objectKey,omitempty"`
	Action             Action         `json:"action"`
	Status             JobStatus      `json:"status"`
	AvailableAt        time.Time      `json:"availableAt"`
	LeaseOwner         string         `json:"leaseOwner,omitempty"`
	LeaseVersion       int64          `json:"leaseVersion"`
	LeaseExpiresAt     *time.Time     `json:"leaseExpiresAt,omitempty"`
	AttemptCount       int            `json:"attemptCount"`
	LastErrorCode      string         `json:"lastErrorCode,omitempty"`
	LastErrorReference string         `json:"lastErrorReference,omitempty"`
	Evidence           map[string]any `json:"evidence,omitempty"`
	CreatedAt          time.Time      `json:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt"`
	CompletedAt        *time.Time     `json:"completedAt,omitempty"`
}
