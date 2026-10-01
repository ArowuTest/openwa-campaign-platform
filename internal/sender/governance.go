package sender

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrSenderNotFound        = errors.New("sender record not found")
	ErrSenderConflict        = errors.New("sender record version conflict")
	ErrSenderMetadataInvalid = errors.New("sender operational metadata is invalid")
)

type Pool struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	OrganisationID       string    `json:"organisationId,omitempty"`
	Status               string    `json:"status"`
	MaxMessagesPerMinute int       `json:"maxMessagesPerMinute"`
	DailyCapacity        int64     `json:"dailyCapacity"`
	ReservedCapacity     int64     `json:"reservedCapacity"`
	Version              int64     `json:"version"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

type Node struct {
	ID                   string                `json:"id"`
	Name                 string                `json:"name"`
	PublicIP             string                `json:"publicIp,omitempty"`
	InternalURL          string                `json:"internalUrl,omitempty"`
	GatewayPoolID        string                `json:"gatewayPoolId,omitempty"`
	Provider             string                `json:"provider,omitempty"`
	Engine               string                `json:"engine,omitempty"`
	AdapterVersion       string                `json:"adapterVersion,omitempty"`
	BootID               string                `json:"bootId,omitempty"`
	Status               string                `json:"status"`
	BuildVersion         string                `json:"buildVersion,omitempty"`
	GatewayVersion       string                `json:"gatewayVersion,omitempty"`
	WorkerVersion        string                `json:"workerVersion,omitempty"`
	ConfigurationVersion string                `json:"configurationVersion,omitempty"`
	RuntimeCapabilities  []Capability          `json:"runtimeCapabilities,omitempty"`
	RuntimeState         RuntimeState          `json:"runtimeState,omitempty"`
	Capacity             int                   `json:"capacity"`
	SessionCount         int                   `json:"sessionCount"`
	QueueDepth           int64                 `json:"queueDepth"`
	CPUPercent           float64               `json:"cpuPercent"`
	MemoryBytes          int64                 `json:"memoryBytes"`
	ResourceHealth       RuntimeResourceHealth `json:"resourceHealth"`
	Draining             bool                  `json:"draining"`
	RegisteredAt         *time.Time            `json:"registeredAt,omitempty"`
	LastHeartbeatAt      *time.Time            `json:"lastHeartbeatAt,omitempty"`
	Version              int64                 `json:"version"`
}

type GovernedSession struct {
	Ownership                   *SessionOwnershipReceipt `json:"ownership,omitempty"`
	ID                          string                   `json:"id"`
	NodeID                      string                   `json:"nodeId,omitempty"`
	PoolID                      string                   `json:"poolId,omitempty"`
	GatewayPoolID               string                   `json:"gatewayPoolId,omitempty"`
	MaskedMSISDN                string                   `json:"maskedMsisdn"`
	OwnerReference              string                   `json:"ownerReference"`
	RegistrationCountryISO2     string                   `json:"registrationCountryIso2"`
	ProfileDisplayName          string                   `json:"profileDisplayName"`
	RecoveryReference           string                   `json:"-"`
	RecoveryReferenceConfigured bool                     `json:"recoveryReferenceConfigured"`
	EngineType                  string                   `json:"engineType"`
	EngineVersion               string                   `json:"engineVersion,omitempty"`
	StateVolumeReference        string                   `json:"stateVolumeReference,omitempty"`
	Status                      Status                   `json:"status"`
	SafeMessagesPerMinute       int                      `json:"safeMessagesPerMinute"`
	SafeDailyCapacity           int64                    `json:"safeDailyCapacity"`
	InFlightLimit               int                      `json:"inFlightLimit"`
	SentToday                   int64                    `json:"sentToday"`
	LastHeartbeatAt             *time.Time               `json:"lastHeartbeatAt,omitempty"`
	LastSuccessAt               *time.Time               `json:"lastSuccessAt,omitempty"`
	QuarantinedAt               *time.Time               `json:"quarantinedAt,omitempty"`
	QuarantineReason            string                   `json:"quarantineReason,omitempty"`
	ReinstatedAt                *time.Time               `json:"reinstatedAt,omitempty"`
	Version                     int64                    `json:"version"`
}

type SessionOperationalMetadata struct {
	OwnerReference          string
	RegistrationCountryISO2 string
	ProfileDisplayName      string
	RecoveryReference       string
}

type CapacitySummary struct {
	PoolID                      string    `json:"poolId"`
	ReadySessions               int       `json:"readySessions"`
	HealthyNodes                int       `json:"healthyNodes"`
	ConfiguredMessagesPerMinute int       `json:"configuredMessagesPerMinute"`
	AvailableMessagesPerMinute  int       `json:"availableMessagesPerMinute"`
	ConfiguredDailyCapacity     int64     `json:"configuredDailyCapacity"`
	RemainingDailyCapacity      int64     `json:"remainingDailyCapacity"`
	ReservedCapacity            int64     `json:"reservedCapacity"`
	AvailableDailyCapacity      int64     `json:"availableDailyCapacity"`
	AsAt                        time.Time `json:"asAt"`
}

type GovernanceStore interface {
	ListPools(context.Context) ([]Pool, error)
	CreatePool(context.Context, Pool, string, string) (Pool, error)
	UpdatePool(context.Context, string, int64, Pool, string, string) (Pool, error)
	ListNodes(context.Context) ([]Node, error)
	GetNode(context.Context, string) (Node, error)
	RegisterNode(context.Context, Node, string, string) (Node, error)
	HeartbeatNode(context.Context, string, int64, Node, time.Time) (Node, error)
	TransitionNode(context.Context, string, int64, string, string, string) (Node, error)
	ListSessions(context.Context) ([]GovernedSession, error)
	GetSession(context.Context, string) (GovernedSession, error)
	RegisterSession(context.Context, GovernedSession, []byte, string, string) (GovernedSession, error)
	UpdateSessionMetadata(context.Context, string, int64, SessionOperationalMetadata, string, string) (GovernedSession, error)
	TransitionSession(context.Context, string, int64, Status, string, string) (GovernedSession, error)
	HeartbeatSession(context.Context, string, int64, GovernedSession, time.Time) (GovernedSession, error)
	Capacity(context.Context, string, time.Time) (CapacitySummary, error)
}

func normaliseSessionOperationalMetadata(value SessionOperationalMetadata) (SessionOperationalMetadata, error) {
	value.OwnerReference = strings.TrimSpace(value.OwnerReference)
	value.RegistrationCountryISO2 = strings.ToUpper(strings.TrimSpace(value.RegistrationCountryISO2))
	value.ProfileDisplayName = strings.TrimSpace(value.ProfileDisplayName)
	value.RecoveryReference = strings.TrimSpace(value.RecoveryReference)
	if len(value.OwnerReference) < 2 || len(value.OwnerReference) > 200 ||
		len(value.ProfileDisplayName) < 1 || len(value.ProfileDisplayName) > 160 ||
		len(value.RecoveryReference) < 3 || len(value.RecoveryReference) > 500 {
		return SessionOperationalMetadata{}, ErrSenderMetadataInvalid
	}
	if len(value.RegistrationCountryISO2) != 2 || value.RegistrationCountryISO2[0] < 'A' || value.RegistrationCountryISO2[0] > 'Z' || value.RegistrationCountryISO2[1] < 'A' || value.RegistrationCountryISO2[1] > 'Z' {
		return SessionOperationalMetadata{}, ErrSenderMetadataInvalid
	}
	for _, field := range []string{value.OwnerReference, value.ProfileDisplayName, value.RecoveryReference} {
		if strings.ContainsAny(field, "\r\n\x00") {
			return SessionOperationalMetadata{}, ErrSenderMetadataInvalid
		}
	}
	return value, nil
}

type HealthPolicyEvidence struct {
	Source          string
	ConfigurationID string
	ScopeType       string
	ScopeID         string
	Version         int64
}

type HealthPolicyResolver interface {
	ResolveHealthPolicy(context.Context, GovernedSession, time.Time) (HealthPolicy, HealthPolicyEvidence, error)
}

type GovernanceService struct {
	Store                       GovernanceStore
	HealthPolicies              HealthPolicyResolver
	HealthSignals               HealthSignalSource
	RequireCanonicalRuntimeURL  bool
	RuntimeAllowedInternalHosts []string
	RuntimeForbiddenHosts       []string
}

func (s *GovernanceService) CreatePool(ctx context.Context, value Pool, actor, reason string) (Pool, error) {
	if s == nil || s.Store == nil {
		return Pool{}, errors.New("sender governance store is required")
	}
	value.Name = strings.TrimSpace(value.Name)
	value.Status = strings.ToUpper(strings.TrimSpace(value.Status))
	if value.Name == "" || actor == "" || strings.TrimSpace(reason) == "" {
		return Pool{}, errors.New("name, actor and reason are required")
	}
	if value.Status == "" {
		value.Status = "ACTIVE"
	}
	if value.Status != "ACTIVE" && value.Status != "PAUSED" && value.Status != "RETIRED" {
		return Pool{}, errors.New("invalid pool status")
	}
	if value.MaxMessagesPerMinute <= 0 || value.MaxMessagesPerMinute > 100000 {
		return Pool{}, errors.New("pool messages per minute must be between 1 and 100000")
	}
	if value.DailyCapacity <= 0 || value.ReservedCapacity < 0 || value.ReservedCapacity > value.DailyCapacity {
		return Pool{}, errors.New("invalid pool daily capacity or reservation")
	}
	return s.Store.CreatePool(ctx, value, actor, reason)
}

func (s *GovernanceService) UpdatePool(ctx context.Context, id string, expected int64, value Pool, actor, reason string) (Pool, error) {
	if strings.TrimSpace(id) == "" || expected <= 0 || strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return Pool{}, errors.New("id, expected version, actor and reason are required")
	}
	value.Name = strings.TrimSpace(value.Name)
	value.Status = strings.ToUpper(strings.TrimSpace(value.Status))
	if value.Name == "" || (value.Status != "ACTIVE" && value.Status != "PAUSED" && value.Status != "RETIRED") {
		return Pool{}, errors.New("invalid pool update")
	}
	if value.MaxMessagesPerMinute <= 0 || value.DailyCapacity <= 0 || value.ReservedCapacity < 0 || value.ReservedCapacity > value.DailyCapacity {
		return Pool{}, errors.New("invalid pool capacity")
	}
	return s.Store.UpdatePool(ctx, id, expected, value, actor, reason)
}

func (s *GovernanceService) RegisterNode(ctx context.Context, value Node, actor, reason string) (Node, error) {
	value.Name = strings.TrimSpace(value.Name)
	value.InternalURL = strings.TrimRight(strings.TrimSpace(value.InternalURL), "/")
	value.GatewayPoolID = strings.TrimSpace(value.GatewayPoolID)
	value.Provider = strings.ToUpper(strings.TrimSpace(value.Provider))
	value.Engine = strings.ToUpper(strings.TrimSpace(value.Engine))
	value.AdapterVersion = strings.TrimSpace(value.AdapterVersion)
	value.BootID = strings.TrimSpace(value.BootID)
	value.Status = strings.ToUpper(strings.TrimSpace(value.Status))
	if value.Name == "" || actor == "" || strings.TrimSpace(reason) == "" {
		return Node{}, errors.New("name, actor and reason are required")
	}
	if value.Status == "" {
		value.Status = "OFFLINE"
	}
	if value.Status != "READY" && value.Status != "DRAINING" && value.Status != "UNHEALTHY" && value.Status != "OFFLINE" {
		return Node{}, errors.New("invalid node status")
	}
	if value.InternalURL != "" {
		if s.RequireCanonicalRuntimeURL {
			canonical, valid := canonicalRuntimeInternalURL(value.InternalURL, s.RuntimeAllowedInternalHosts, s.RuntimeForbiddenHosts)
			if !valid {
				return Node{}, errors.New("node internal URL must be a governed canonical HTTPS authority")
			}
			value.InternalURL = canonical
		} else {
			canonical, valid := canonicalRuntimeInternalURLForMode(value.InternalURL, s.RuntimeAllowedInternalHosts, s.RuntimeForbiddenHosts, true)
			if !valid {
				return Node{}, errors.New("node internal URL must be a governed HTTP or HTTPS authority")
			}
			value.InternalURL = canonical
		}
	}
	if value.Provider != "" && value.Provider != "OPENWA" {
		return Node{}, errors.New("initial release supports OPENWA sender nodes")
	}
	if value.Engine != "" && value.Engine != "WHATSAPP_WEB_JS" && value.Engine != "BAILEYS" {
		return Node{}, errors.New("invalid OpenWA engine")
	}
	if value.Capacity < 0 || value.Capacity > 1000 {
		return Node{}, errors.New("node capacity is invalid")
	}
	return s.Store.RegisterNode(ctx, value, actor, reason)
}

func (s *GovernanceService) TransitionNode(ctx context.Context, id string, expected int64, status, actor, reason string) (Node, error) {
	if s == nil || s.Store == nil {
		return Node{}, errors.New("sender governance store is required")
	}
	id, status, actor, reason = strings.TrimSpace(id), strings.ToUpper(strings.TrimSpace(status)), strings.TrimSpace(actor), strings.TrimSpace(reason)
	if id == "" || expected <= 0 || actor == "" || reason == "" {
		return Node{}, errors.New("node, expected version, actor and reason are required")
	}
	switch status {
	case "DRAINING", "OFFLINE", "RETIRED":
	default:
		return Node{}, errors.New("administrative node transition is invalid")
	}
	return s.Store.TransitionNode(ctx, id, expected, status, actor, reason)
}

func (s *GovernanceService) RegisterSession(ctx context.Context, value GovernedSession, encryptedMSISDN []byte, actor, reason string) (GovernedSession, error) {
	value.MaskedMSISDN = strings.TrimSpace(value.MaskedMSISDN)
	value.EngineType = strings.TrimSpace(value.EngineType)
	metadata, err := normaliseSessionOperationalMetadata(SessionOperationalMetadata{
		OwnerReference: value.OwnerReference, RegistrationCountryISO2: value.RegistrationCountryISO2,
		ProfileDisplayName: value.ProfileDisplayName, RecoveryReference: value.RecoveryReference,
	})
	if err != nil {
		return GovernedSession{}, err
	}
	value.OwnerReference = metadata.OwnerReference
	value.RegistrationCountryISO2 = metadata.RegistrationCountryISO2
	value.ProfileDisplayName = metadata.ProfileDisplayName
	value.RecoveryReference = metadata.RecoveryReference
	value.RecoveryReferenceConfigured = true
	if value.MaskedMSISDN == "" || value.EngineType == "" || len(encryptedMSISDN) == 0 || actor == "" || strings.TrimSpace(reason) == "" {
		return GovernedSession{}, errors.New("sender identity, engine, actor and reason are required")
	}
	if value.Status == "" {
		value.Status = StatusNew
	}
	if value.Status != StatusNew {
		return GovernedSession{}, errors.New("new sender sessions must begin in NEW state")
	}
	if value.SafeMessagesPerMinute <= 0 || value.SafeDailyCapacity <= 0 || value.InFlightLimit <= 0 || value.InFlightLimit > 100 {
		return GovernedSession{}, errors.New("session capacity limits are invalid")
	}
	return s.Store.RegisterSession(ctx, value, encryptedMSISDN, actor, reason)
}

func (s *GovernanceService) UpdateSessionMetadata(ctx context.Context, id string, expected int64, value SessionOperationalMetadata, actor, reason string) (GovernedSession, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(id) == "" || expected <= 0 || strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 8 {
		return GovernedSession{}, ErrSenderMetadataInvalid
	}
	metadata, err := normaliseSessionOperationalMetadata(value)
	if err != nil {
		return GovernedSession{}, err
	}
	current, err := s.Store.GetSession(ctx, strings.TrimSpace(id))
	if err != nil {
		return GovernedSession{}, err
	}
	if current.Version != expected {
		return GovernedSession{}, ErrSenderConflict
	}
	return s.Store.UpdateSessionMetadata(ctx, current.ID, expected, metadata, strings.TrimSpace(actor), strings.TrimSpace(reason))
}

func (s *GovernanceService) TransitionSession(ctx context.Context, id string, expected int64, status Status, actor, reason string) (GovernedSession, error) {
	if id == "" || expected <= 0 || actor == "" || strings.TrimSpace(reason) == "" {
		return GovernedSession{}, errors.New("id, expected version, actor and reason are required")
	}
	if status == StatusQuarantined {
		return GovernedSession{}, errors.New("use the governed quarantine operation")
	}
	switch status {
	case StatusNew, StatusPairing, StatusConnecting, StatusReady, StatusBusy, StatusDraining, StatusPaused, StatusDisconnected, StatusRecovering, StatusRecoveryFail, StatusRestricted, StatusQuarantined, StatusRetired:
	default:
		return GovernedSession{}, fmt.Errorf("invalid sender status %q", status)
	}
	current, err := s.Store.GetSession(ctx, id)
	if err != nil {
		return GovernedSession{}, err
	}
	if current.Version != expected {
		return GovernedSession{}, ErrSenderConflict
	}
	if !AllowedSessionTransition(current.Status, status) {
		return GovernedSession{}, fmt.Errorf("sender transition %s -> %s is not permitted", current.Status, status)
	}
	return s.Store.TransitionSession(ctx, id, expected, status, actor, reason)
}

// AllowedSessionTransition is the canonical sender-session state machine. Both
// memory and PostgreSQL implementations call through the service, and the
// database migration mirrors these states so a newly registered or unpaired
// account can never be allocated as READY.
func AllowedSessionTransition(from, to Status) bool {
	if from == to {
		return true
	}
	allowed := map[Status]map[Status]bool{
		StatusNew:          {StatusPairing: true, StatusRetired: true},
		StatusPairing:      {StatusConnecting: true, StatusPaused: true, StatusDisconnected: true, StatusRestricted: true, StatusRetired: true},
		StatusConnecting:   {StatusReady: true, StatusDisconnected: true, StatusRestricted: true, StatusRecovering: true},
		StatusReady:        {StatusBusy: true, StatusDraining: true, StatusPaused: true, StatusDisconnected: true, StatusRestricted: true, StatusQuarantined: true, StatusRetired: true},
		StatusBusy:         {StatusReady: true, StatusDraining: true, StatusPaused: true, StatusDisconnected: true, StatusRestricted: true, StatusQuarantined: true},
		StatusDraining:     {StatusPaused: true, StatusReady: true, StatusDisconnected: true, StatusPairing: true, StatusRetired: true},
		StatusPaused:       {StatusConnecting: true, StatusReady: true, StatusRecovering: true, StatusDisconnected: true, StatusPairing: true, StatusRetired: true},
		StatusDisconnected: {StatusConnecting: true, StatusRecovering: true, StatusPairing: true, StatusRestricted: true, StatusRetired: true},
		StatusRecovering:   {StatusConnecting: true, StatusReady: true, StatusRecoveryFail: true, StatusRestricted: true},
		StatusRecoveryFail: {StatusRecovering: true, StatusPairing: true, StatusRetired: true},
		StatusRestricted:   {StatusRecovering: true, StatusPaused: true, StatusPairing: true, StatusRetired: true},
		StatusQuarantined:  {StatusReady: true, StatusRetired: true},
		StatusRetired:      {},
	}
	return allowed[from][to]
}

func (s *GovernanceService) QuarantineSession(ctx context.Context, id string, expected int64, actor, reason string) (GovernedSession, error) {
	if s == nil || s.Store == nil {
		return GovernedSession{}, errors.New("sender governance store is required")
	}
	if strings.TrimSpace(id) == "" || expected <= 0 || strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 8 {
		return GovernedSession{}, errors.New("id, expected version, actor and a meaningful quarantine reason are required")
	}
	return s.Store.TransitionSession(ctx, id, expected, StatusQuarantined, actor, reason)
}

func (s *GovernanceService) ReinstateSession(ctx context.Context, id string, expected int64, actor, reason string) (GovernedSession, error) {
	if s == nil || s.Store == nil {
		return GovernedSession{}, errors.New("sender governance store is required")
	}
	if strings.TrimSpace(id) == "" || expected <= 0 || strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 8 {
		return GovernedSession{}, errors.New("id, expected version, actor and a meaningful reinstatement reason are required")
	}
	return s.Store.TransitionSession(ctx, id, expected, StatusReady, actor, reason)
}

type HealthPolicy struct {
	HeartbeatStaleAfter      time.Duration
	HeartbeatCriticalAfter   time.Duration
	SuccessStaleAfter        time.Duration
	SuccessCriticalAfter     time.Duration
	FailureWindow            time.Duration
	FailureMinimumSamples    int
	FailureRateThresholdBPS  int
	DisconnectWindow         time.Duration
	DisconnectThreshold      int
	CapacityNearLimitPercent int
}

func DefaultHealthPolicy() HealthPolicy {
	return HealthPolicy{
		HeartbeatStaleAfter:      90 * time.Second,
		HeartbeatCriticalAfter:   180 * time.Second,
		SuccessStaleAfter:        6 * time.Hour,
		SuccessCriticalAfter:     24 * time.Hour,
		FailureWindow:            15 * time.Minute,
		FailureMinimumSamples:    20,
		FailureRateThresholdBPS:  1000,
		DisconnectWindow:         30 * time.Minute,
		DisconnectThreshold:      3,
		CapacityNearLimitPercent: 90,
	}
}

type HealthSignals struct {
	RecentOutcomeCount    int64
	RecentFailureCount    int64
	RecentDisconnectCount int
}

type HealthSignalSource interface {
	ReadHealthSignals(context.Context, GovernedSession, HealthPolicy, time.Time) (HealthSignals, error)
}

type HealthAssessment struct {
	SessionID             string    `json:"sessionId"`
	Score                 int       `json:"score"`
	State                 string    `json:"state"`
	Recommendation        string    `json:"recommendation"`
	Reasons               []string  `json:"reasons"`
	HeartbeatAgeSec       int64     `json:"heartbeatAgeSeconds"`
	CapacityUsedPct       int       `json:"capacityUsedPercent"`
	RecentOutcomeCount    int64     `json:"recentOutcomeCount"`
	RecentFailureCount    int64     `json:"recentFailureCount"`
	RecentFailureRateBPS  int       `json:"recentFailureRateBps"`
	RecentDisconnectCount int       `json:"recentDisconnectCount"`
	PolicySource          string    `json:"policySource"`
	PolicyConfigurationID string    `json:"policyConfigurationId,omitempty"`
	PolicyScopeType       string    `json:"policyScopeType,omitempty"`
	PolicyScopeID         string    `json:"policyScopeId,omitempty"`
	PolicyVersion         int64     `json:"policyVersion,omitempty"`
	AssessedAt            time.Time `json:"assessedAt"`
}

func (s *GovernanceService) AssessSession(ctx context.Context, id string, now time.Time) (HealthAssessment, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(id) == "" {
		return HealthAssessment{}, errors.New("sender governance store and session id are required")
	}
	v, err := s.Store.GetSession(ctx, id)
	if err != nil {
		return HealthAssessment{}, err
	}
	now = now.UTC()
	policy := DefaultHealthPolicy()
	evidence := HealthPolicyEvidence{Source: "SAFE_DEFAULT"}
	if s.HealthPolicies != nil {
		policy, evidence, err = s.HealthPolicies.ResolveHealthPolicy(ctx, v, now)
		if err != nil {
			return HealthAssessment{}, fmt.Errorf("resolve sender health policy: %w", err)
		}
	}
	out := HealthAssessment{
		SessionID: id, Score: 100, State: "HEALTHY", Recommendation: "KEEP_IN_ALLOCATION",
		PolicySource: evidence.Source, PolicyConfigurationID: evidence.ConfigurationID,
		PolicyScopeType: evidence.ScopeType, PolicyScopeID: evidence.ScopeID, PolicyVersion: evidence.Version,
		AssessedAt: now,
	}
	if s.HealthSignals != nil {
		signals, signalErr := s.HealthSignals.ReadHealthSignals(ctx, v, policy, now)
		if signalErr != nil {
			return HealthAssessment{}, fmt.Errorf("read sender health signals: %w", signalErr)
		}
		out.RecentOutcomeCount = signals.RecentOutcomeCount
		out.RecentFailureCount = signals.RecentFailureCount
		out.RecentDisconnectCount = signals.RecentDisconnectCount
		if signals.RecentOutcomeCount > 0 {
			out.RecentFailureRateBPS = int((signals.RecentFailureCount * 10000) / signals.RecentOutcomeCount)
		}
		if policy.FailureRateThresholdBPS > 0 && signals.RecentOutcomeCount >= int64(policy.FailureMinimumSamples) && out.RecentFailureRateBPS >= policy.FailureRateThresholdBPS {
			out.Score -= 40
			out.Reasons = append(out.Reasons, "FAILURE_RATE_THRESHOLD_BREACHED")
		}
		if policy.DisconnectThreshold > 0 && signals.RecentDisconnectCount >= policy.DisconnectThreshold {
			out.Score -= 40
			out.Reasons = append(out.Reasons, "DISCONNECT_THRESHOLD_BREACHED")
		}
	}
	if v.LastHeartbeatAt == nil {
		out.Score -= 60
		out.Reasons = append(out.Reasons, "NO_HEARTBEAT_EVIDENCE")
	} else {
		heartbeatAge := now.Sub(v.LastHeartbeatAt.UTC())
		out.HeartbeatAgeSec = int64(heartbeatAge.Seconds())
		if heartbeatAge > policy.HeartbeatCriticalAfter {
			out.Score -= 70
			out.Reasons = append(out.Reasons, "HEARTBEAT_CRITICALLY_STALE")
		} else if heartbeatAge > policy.HeartbeatStaleAfter {
			out.Score -= 35
			out.Reasons = append(out.Reasons, "HEARTBEAT_STALE")
		}
	}
	if v.LastSuccessAt == nil {
		if v.SentToday > 0 {
			out.Score -= 25
			out.Reasons = append(out.Reasons, "NO_SUCCESS_EVIDENCE")
		}
	} else {
		successAge := now.Sub(v.LastSuccessAt.UTC())
		if successAge > policy.SuccessCriticalAfter {
			out.Score -= 25
			out.Reasons = append(out.Reasons, "SUCCESS_CRITICALLY_STALE")
		} else if successAge > policy.SuccessStaleAfter {
			out.Score -= 10
			out.Reasons = append(out.Reasons, "SUCCESS_STALE")
		}
	}
	if v.SafeDailyCapacity > 0 {
		out.CapacityUsedPct = int((100 * v.SentToday) / v.SafeDailyCapacity)
		if out.CapacityUsedPct >= 100 {
			out.Score -= 25
			out.Reasons = append(out.Reasons, "DAILY_CAPACITY_EXHAUSTED")
		} else if out.CapacityUsedPct >= policy.CapacityNearLimitPercent {
			out.Score -= 10
			out.Reasons = append(out.Reasons, "DAILY_CAPACITY_NEAR_LIMIT")
		}
	}
	switch v.Status {
	case StatusQuarantined:
		out.Score = 0
		out.Reasons = append(out.Reasons, "SESSION_QUARANTINED")
	case StatusRestricted:
		out.Score = minInt(out.Score, 10)
		out.Reasons = append(out.Reasons, "PROVIDER_RESTRICTED")
	case StatusRetired:
		out.Score = 0
		out.Reasons = append(out.Reasons, "SESSION_RETIRED")
	case StatusDisconnected:
		out.Score -= 50
		out.Reasons = append(out.Reasons, "SESSION_DISCONNECTED")
	case StatusPaused:
		out.Score -= 20
		out.Reasons = append(out.Reasons, "SESSION_PAUSED")
	}
	if out.Score < 0 {
		out.Score = 0
	}
	if out.Score < 30 {
		out.State = "CRITICAL"
		out.Recommendation = "QUARANTINE_OR_KEEP_OUT_OF_ALLOCATION"
	} else if out.Score < 70 {
		out.State = "DEGRADED"
		out.Recommendation = "DRAIN_AND_REVIEW"
	}
	return out, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
