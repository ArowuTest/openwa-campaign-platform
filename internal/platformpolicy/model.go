package platformpolicy

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("platform policy record not found")
	ErrConflict = errors.New("platform policy version conflict")
	ErrInvalid  = errors.New("platform policy record is invalid")
	ErrBlocked  = errors.New("operation blocked by active maintenance policy")
)

type LifecycleStatus string

const (
	StatusDraft           LifecycleStatus = "DRAFT"
	StatusPendingApproval LifecycleStatus = "PENDING_APPROVAL"
	StatusActive          LifecycleStatus = "ACTIVE"
	StatusRejected        LifecycleStatus = "REJECTED"
	StatusSuperseded      LifecycleStatus = "SUPERSEDED"
	StatusRetired         LifecycleStatus = "RETIRED"
)

type ScopeType string

const (
	ScopePlatform     ScopeType = "PLATFORM"
	ScopeEnvironment  ScopeType = "ENVIRONMENT"
	ScopeOrganisation ScopeType = "ORGANISATION"
	ScopeCampaign     ScopeType = "CAMPAIGN"
	ScopeProvider     ScopeType = "PROVIDER"
	ScopeGatewayPool  ScopeType = "GATEWAY_POOL"
	ScopeSenderPool   ScopeType = "SENDER_POOL"
)

type Configuration struct {
	ID            string          `json:"id"`
	Key           string          `json:"key"`
	ScopeType     ScopeType       `json:"scopeType"`
	ScopeID       string          `json:"scopeId,omitempty"`
	Value         json.RawMessage `json:"value"`
	ValueChecksum string          `json:"valueChecksum"`
	Status        LifecycleStatus `json:"status"`
	EffectiveFrom time.Time       `json:"effectiveFrom"`
	EffectiveTo   *time.Time      `json:"effectiveTo,omitempty"`
	CreatedBy     string          `json:"createdBy"`
	SubmittedBy   string          `json:"submittedBy,omitempty"`
	ApprovedBy    string          `json:"approvedBy,omitempty"`
	SupersedesID  string          `json:"supersedesId,omitempty"`
	RollbackOfID  string          `json:"rollbackOfId,omitempty"`
	Reason        string          `json:"reason"`
	Version       int64           `json:"version"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

type ConfigurationQuery struct {
	Key       string
	ScopeType ScopeType
	ScopeID   string
	Status    LifecycleStatus
	Limit     int
}

type Event struct {
	ID         string         `json:"id"`
	ObjectID   string         `json:"objectId"`
	EventType  string         `json:"eventType"`
	Version    int64          `json:"version"`
	ActorID    string         `json:"actorId"`
	Reason     string         `json:"reason"`
	Evidence   map[string]any `json:"evidence,omitempty"`
	OccurredAt time.Time      `json:"occurredAt"`
}

type MaintenanceMode string

const (
	MaintenanceReadOnly        MaintenanceMode = "READ_ONLY"
	MaintenanceAdmissionFrozen MaintenanceMode = "ADMISSION_FROZEN"
	MaintenanceDraining        MaintenanceMode = "DRAINING"
	MaintenanceEmergencyStop   MaintenanceMode = "EMERGENCY_STOP"
)

type MaintenanceStatus string

const (
	MaintenanceDraft           MaintenanceStatus = "DRAFT"
	MaintenancePendingApproval MaintenanceStatus = "PENDING_APPROVAL"
	MaintenanceActive          MaintenanceStatus = "ACTIVE"
	MaintenanceRejected        MaintenanceStatus = "REJECTED"
	MaintenanceEnded           MaintenanceStatus = "ENDED"
	MaintenanceCancelled       MaintenanceStatus = "CANCELLED"
)

type MaintenanceWindow struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Mode                MaintenanceMode   `json:"mode"`
	ScopeType           ScopeType         `json:"scopeType"`
	ScopeID             string            `json:"scopeId,omitempty"`
	StartsAt            time.Time         `json:"startsAt"`
	EndsAt              *time.Time        `json:"endsAt,omitempty"`
	AllowActiveDispatch bool              `json:"allowActiveDispatch"`
	Status              MaintenanceStatus `json:"status"`
	CreatedBy           string            `json:"createdBy"`
	SubmittedBy         string            `json:"submittedBy,omitempty"`
	ApprovedBy          string            `json:"approvedBy,omitempty"`
	EndedBy             string            `json:"endedBy,omitempty"`
	Reason              string            `json:"reason"`
	Version             int64             `json:"version"`
	CreatedAt           time.Time         `json:"createdAt"`
	UpdatedAt           time.Time         `json:"updatedAt"`
}

type Operation string

const (
	OperationAPIWrite       Operation = "API_WRITE"
	OperationCampaignStart  Operation = "CAMPAIGN_START"
	OperationCampaignResume Operation = "CAMPAIGN_RESUME"
	OperationDispatchSubmit Operation = "DISPATCH_SUBMIT"
	OperationSessionChange  Operation = "SESSION_CHANGE"
)

type OperationalScope struct {
	Provider      string
	GatewayPoolID string
	SenderPoolID  string
}

type BlockedError struct {
	Window MaintenanceWindow
	Action Operation
}

func (e BlockedError) Error() string {
	return string(e.Action) + " blocked by maintenance window " + e.Window.ID
}
func (e BlockedError) Unwrap() error { return ErrBlocked }
