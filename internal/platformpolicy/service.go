package platformpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"campaign-platform/internal/shared/id"
)

var configurationKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_.-]{2,119}$`)

type ConfigurationStore interface {
	ListConfigurations(context.Context, ConfigurationQuery) ([]Configuration, error)
	GetConfiguration(context.Context, string) (Configuration, error)
	CreateConfiguration(context.Context, Configuration, Event) (Configuration, error)
	UpdateConfiguration(context.Context, Configuration, int64, Event) (Configuration, error)
	ActivateConfiguration(context.Context, Configuration, int64, Event) (Configuration, error)
	ResolveConfiguration(context.Context, string, ScopeType, string, time.Time) (Configuration, error)
	ListConfigurationEvents(context.Context, string, int) ([]Event, error)
}

type ConfigurationAdministration struct {
	Store ConfigurationStore
	Clock func() time.Time
}

func (a *ConfigurationAdministration) now() time.Time {
	if a != nil && a.Clock != nil {
		return a.Clock().UTC()
	}
	return time.Now().UTC()
}

func canonicalJSON(value json.RawMessage) (json.RawMessage, string, error) {
	if len(value) == 0 || len(value) > 64<<10 {
		return nil, "", ErrInvalid
	}
	var decoded any
	if err := json.Unmarshal(value, &decoded); err != nil {
		return nil, "", ErrInvalid
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(sum[:]), nil
}

func validateScope(scope ScopeType, scopeID string) error {
	scopeID = strings.TrimSpace(scopeID)
	switch scope {
	case ScopePlatform:
		if scopeID != "" {
			return ErrInvalid
		}
	case ScopeEnvironment, ScopeOrganisation, ScopeCampaign, ScopeProvider, ScopeGatewayPool, ScopeSenderPool, ScopeSenderSession:
		if scopeID == "" || len(scopeID) > 200 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func (a *ConfigurationAdministration) Create(ctx context.Context, input Configuration, actor, reason string) (Configuration, error) {
	if a == nil || a.Store == nil {
		return Configuration{}, errors.New("configuration store is required")
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	input.Key = strings.ToUpper(strings.TrimSpace(input.Key))
	input.ScopeID = strings.TrimSpace(input.ScopeID)
	if actor == "" || reason == "" || !configurationKeyPattern.MatchString(input.Key) || validateScope(input.ScopeType, input.ScopeID) != nil {
		return Configuration{}, ErrInvalid
	}
	value, checksum, err := canonicalJSON(input.Value)
	if err != nil {
		return Configuration{}, err
	}
	now := a.now()
	if input.EffectiveFrom.IsZero() {
		input.EffectiveFrom = now
	} else {
		input.EffectiveFrom = input.EffectiveFrom.UTC()
	}
	if input.EffectiveTo != nil {
		v := input.EffectiveTo.UTC()
		input.EffectiveTo = &v
		if !v.After(input.EffectiveFrom) {
			return Configuration{}, ErrInvalid
		}
	}
	identifier, err := id.New()
	if err != nil {
		return Configuration{}, err
	}
	input.ID = identifier
	input.Value = value
	input.ValueChecksum = checksum
	input.Status = StatusDraft
	input.CreatedBy = actor
	input.SubmittedBy = ""
	input.ApprovedBy = ""
	input.Reason = reason
	input.Version = 1
	input.CreatedAt = now
	input.UpdatedAt = now
	event := Event{ObjectID: input.ID, EventType: "CREATED", Version: 1, ActorID: actor, Reason: reason, Evidence: map[string]any{"key": input.Key, "scopeType": input.ScopeType, "scopeId": input.ScopeID, "checksum": checksum}, OccurredAt: now}
	return a.Store.CreateConfiguration(ctx, input, event)
}

func (a *ConfigurationAdministration) Submit(ctx context.Context, configurationID string, expected int64, actor, reason string) (Configuration, error) {
	current, err := a.Store.GetConfiguration(ctx, strings.TrimSpace(configurationID))
	if err != nil {
		return Configuration{}, err
	}
	if current.Version != expected || current.Status != StatusDraft || strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return Configuration{}, ErrConflict
	}
	current.Status = StatusPendingApproval
	current.SubmittedBy = strings.TrimSpace(actor)
	current.Reason = strings.TrimSpace(reason)
	current.Version++
	current.UpdatedAt = a.now()
	event := Event{ObjectID: current.ID, EventType: "SUBMITTED", Version: current.Version, ActorID: actor, Reason: reason, Evidence: map[string]any{"checksum": current.ValueChecksum}, OccurredAt: current.UpdatedAt}
	return a.Store.UpdateConfiguration(ctx, current, expected, event)
}

func (a *ConfigurationAdministration) Decide(ctx context.Context, configurationID string, expected int64, approve bool, actor, reason string) (Configuration, error) {
	current, err := a.Store.GetConfiguration(ctx, strings.TrimSpace(configurationID))
	if err != nil {
		return Configuration{}, err
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if current.Version != expected || current.Status != StatusPendingApproval || actor == "" || reason == "" {
		return Configuration{}, ErrConflict
	}
	if actor == current.CreatedBy || actor == current.SubmittedBy {
		return Configuration{}, errors.New("configuration approver must be independent from maker and submitter")
	}
	previous := current.Version
	current.ApprovedBy = actor
	current.Reason = reason
	current.Version++
	current.UpdatedAt = a.now()
	if !approve {
		current.Status = StatusRejected
		event := Event{ObjectID: current.ID, EventType: "REJECTED", Version: current.Version, ActorID: actor, Reason: reason, Evidence: map[string]any{"checksum": current.ValueChecksum}, OccurredAt: current.UpdatedAt}
		return a.Store.UpdateConfiguration(ctx, current, previous, event)
	}
	current.Status = StatusActive
	event := Event{ObjectID: current.ID, EventType: "ACTIVATED", Version: current.Version, ActorID: actor, Reason: reason, Evidence: map[string]any{"effectiveFrom": current.EffectiveFrom, "effectiveTo": current.EffectiveTo, "checksum": current.ValueChecksum}, OccurredAt: current.UpdatedAt}
	return a.Store.ActivateConfiguration(ctx, current, previous, event)
}

func (a *ConfigurationAdministration) Retire(ctx context.Context, configurationID string, expected int64, actor, reason string) (Configuration, error) {
	current, err := a.Store.GetConfiguration(ctx, strings.TrimSpace(configurationID))
	if err != nil {
		return Configuration{}, err
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if current.Version != expected || current.Status != StatusActive || actor == "" || reason == "" {
		return Configuration{}, ErrConflict
	}
	now := a.now()
	current.Status = StatusRetired
	if current.EffectiveTo == nil || current.EffectiveTo.After(now) {
		current.EffectiveTo = &now
	}
	current.Reason = reason
	current.Version++
	current.UpdatedAt = now
	event := Event{ObjectID: current.ID, EventType: "RETIRED", Version: current.Version, ActorID: actor, Reason: reason, Evidence: map[string]any{"checksum": current.ValueChecksum}, OccurredAt: now}
	return a.Store.UpdateConfiguration(ctx, current, expected, event)
}

func (a *ConfigurationAdministration) Rollback(ctx context.Context, sourceID string, actor, reason string, effectiveFrom time.Time) (Configuration, error) {
	source, err := a.Store.GetConfiguration(ctx, strings.TrimSpace(sourceID))
	if err != nil {
		return Configuration{}, err
	}
	copy := source
	copy.ID = ""
	copy.Status = ""
	copy.SubmittedBy = ""
	copy.ApprovedBy = ""
	copy.SupersedesID = source.ID
	copy.RollbackOfID = source.ID
	copy.Version = 0
	copy.CreatedAt = time.Time{}
	copy.UpdatedAt = time.Time{}
	copy.EffectiveFrom = effectiveFrom.UTC()
	if copy.EffectiveFrom.IsZero() {
		copy.EffectiveFrom = a.now()
	}
	copy.EffectiveTo = nil
	created, err := a.Create(ctx, copy, actor, reason)
	if err != nil {
		return Configuration{}, err
	}
	created.RollbackOfID = source.ID
	created.SupersedesID = source.ID
	return created, nil
}

func (a *ConfigurationAdministration) Resolve(ctx context.Context, key string, scopes []OperationalScopeRef, at time.Time) (Configuration, error) {
	if a == nil || a.Store == nil {
		return Configuration{}, errors.New("configuration store is required")
	}
	key = strings.ToUpper(strings.TrimSpace(key))
	if !configurationKeyPattern.MatchString(key) {
		return Configuration{}, ErrInvalid
	}
	if at.IsZero() {
		at = a.now()
	}
	// The caller passes most-specific to least-specific scopes. PLATFORM is
	// always checked last so a safe default remains available.
	for _, scope := range scopes {
		value, err := a.Store.ResolveConfiguration(ctx, key, scope.Type, strings.TrimSpace(scope.ID), at.UTC())
		if err == nil {
			return value, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Configuration{}, err
		}
	}
	return a.Store.ResolveConfiguration(ctx, key, ScopePlatform, "", at.UTC())
}

type OperationalScopeRef struct {
	Type ScopeType
	ID   string
}

func (a *ConfigurationAdministration) List(ctx context.Context, query ConfigurationQuery) ([]Configuration, error) {
	if query.Limit <= 0 || query.Limit > 500 {
		query.Limit = 100
	}
	return a.Store.ListConfigurations(ctx, query)
}

func (a *ConfigurationAdministration) Events(ctx context.Context, id string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	return a.Store.ListConfigurationEvents(ctx, strings.TrimSpace(id), limit)
}

// Maintenance storage and service.
type MaintenanceStore interface {
	ListMaintenance(context.Context, MaintenanceStatus, int) ([]MaintenanceWindow, error)
	GetMaintenance(context.Context, string) (MaintenanceWindow, error)
	CreateMaintenance(context.Context, MaintenanceWindow, Event) (MaintenanceWindow, error)
	UpdateMaintenance(context.Context, MaintenanceWindow, int64, Event) (MaintenanceWindow, error)
	ListActiveMaintenance(context.Context, time.Time) ([]MaintenanceWindow, error)
	ListMaintenanceEvents(context.Context, string, int) ([]Event, error)
}

type MaintenanceAdministration struct {
	Store MaintenanceStore
	Clock func() time.Time
}

func (a *MaintenanceAdministration) now() time.Time {
	if a != nil && a.Clock != nil {
		return a.Clock().UTC()
	}
	return time.Now().UTC()
}

func validateMaintenanceScope(scope ScopeType, id string) error {
	switch scope {
	case ScopePlatform:
		if strings.TrimSpace(id) != "" {
			return ErrInvalid
		}
	case ScopeProvider, ScopeGatewayPool, ScopeSenderPool:
		if strings.TrimSpace(id) == "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func (a *MaintenanceAdministration) Create(ctx context.Context, input MaintenanceWindow, actor, reason string) (MaintenanceWindow, error) {
	if a == nil || a.Store == nil {
		return MaintenanceWindow{}, errors.New("maintenance store is required")
	}
	input.Name = strings.TrimSpace(input.Name)
	input.ScopeID = strings.TrimSpace(input.ScopeID)
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if input.Name == "" || actor == "" || reason == "" || validateMaintenanceScope(input.ScopeType, input.ScopeID) != nil {
		return MaintenanceWindow{}, ErrInvalid
	}
	switch input.Mode {
	case MaintenanceReadOnly, MaintenanceAdmissionFrozen, MaintenanceEmergencyStop:
		input.AllowActiveDispatch = false
	case MaintenanceDraining:
	default:
		return MaintenanceWindow{}, ErrInvalid
	}
	now := a.now()
	if input.StartsAt.IsZero() {
		input.StartsAt = now
	} else {
		input.StartsAt = input.StartsAt.UTC()
	}
	if input.EndsAt != nil {
		end := input.EndsAt.UTC()
		input.EndsAt = &end
		if !end.After(input.StartsAt) {
			return MaintenanceWindow{}, ErrInvalid
		}
	}
	identifier, err := id.New()
	if err != nil {
		return MaintenanceWindow{}, err
	}
	input.ID = identifier
	input.Status = MaintenanceDraft
	input.CreatedBy = actor
	input.Reason = reason
	input.Version = 1
	input.CreatedAt, input.UpdatedAt = now, now
	event := Event{ObjectID: input.ID, EventType: "CREATED", Version: 1, ActorID: actor, Reason: reason, Evidence: map[string]any{"mode": input.Mode, "scopeType": input.ScopeType, "scopeId": input.ScopeID}, OccurredAt: now}
	return a.Store.CreateMaintenance(ctx, input, event)
}

func (a *MaintenanceAdministration) Submit(ctx context.Context, id string, expected int64, actor, reason string) (MaintenanceWindow, error) {
	current, err := a.Store.GetMaintenance(ctx, strings.TrimSpace(id))
	if err != nil {
		return MaintenanceWindow{}, err
	}
	if current.Version != expected || current.Status != MaintenanceDraft || strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return MaintenanceWindow{}, ErrConflict
	}
	current.Status = MaintenancePendingApproval
	current.SubmittedBy = strings.TrimSpace(actor)
	current.Reason = strings.TrimSpace(reason)
	current.Version++
	current.UpdatedAt = a.now()
	event := Event{ObjectID: current.ID, EventType: "SUBMITTED", Version: current.Version, ActorID: actor, Reason: reason, OccurredAt: current.UpdatedAt}
	return a.Store.UpdateMaintenance(ctx, current, expected, event)
}

func (a *MaintenanceAdministration) Decide(ctx context.Context, id string, expected int64, approve bool, actor, reason string) (MaintenanceWindow, error) {
	current, err := a.Store.GetMaintenance(ctx, strings.TrimSpace(id))
	if err != nil {
		return MaintenanceWindow{}, err
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if current.Version != expected || current.Status != MaintenancePendingApproval || actor == "" || reason == "" {
		return MaintenanceWindow{}, ErrConflict
	}
	if actor == current.CreatedBy || actor == current.SubmittedBy {
		return MaintenanceWindow{}, errors.New("maintenance approver must be independent from maker and submitter")
	}
	now := a.now()
	if approve && current.EndsAt != nil && !current.EndsAt.After(now) {
		return MaintenanceWindow{}, errors.New("expired maintenance window cannot be activated")
	}
	current.ApprovedBy = actor
	current.Reason = reason
	current.Version++
	current.UpdatedAt = now
	if approve {
		current.Status = MaintenanceActive
	} else {
		current.Status = MaintenanceRejected
	}
	eventType := "REJECTED"
	if approve {
		eventType = "ACTIVATED"
	}
	event := Event{ObjectID: current.ID, EventType: eventType, Version: current.Version, ActorID: actor, Reason: reason, Evidence: map[string]any{"startsAt": current.StartsAt, "endsAt": current.EndsAt, "mode": current.Mode}, OccurredAt: current.UpdatedAt}
	return a.Store.UpdateMaintenance(ctx, current, expected, event)
}

func (a *MaintenanceAdministration) End(ctx context.Context, id string, expected int64, actor, reason string) (MaintenanceWindow, error) {
	current, err := a.Store.GetMaintenance(ctx, strings.TrimSpace(id))
	if err != nil {
		return MaintenanceWindow{}, err
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if current.Version != expected || current.Status != MaintenanceActive || actor == "" || reason == "" {
		return MaintenanceWindow{}, ErrConflict
	}
	now := a.now()
	eventType := "ENDED"
	if current.StartsAt.After(now) {
		current.Status = MaintenanceCancelled
		eventType = "CANCELLED"
	} else {
		current.Status = MaintenanceEnded
		if current.EndsAt == nil || current.EndsAt.After(now) {
			current.EndsAt = &now
		}
	}
	current.EndedBy = actor
	current.Reason = reason
	current.Version++
	current.UpdatedAt = now
	event := Event{ObjectID: current.ID, EventType: eventType, Version: current.Version, ActorID: actor, Reason: reason, OccurredAt: now}
	return a.Store.UpdateMaintenance(ctx, current, expected, event)
}

func (a *MaintenanceAdministration) Active(ctx context.Context, at time.Time) ([]MaintenanceWindow, error) {
	if at.IsZero() {
		at = a.now()
	}
	items, err := a.Store.ListActiveMaintenance(ctx, at.UTC())
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Mode == items[j].Mode {
			return items[i].StartsAt.Before(items[j].StartsAt)
		}
		return maintenanceSeverity(items[i].Mode) > maintenanceSeverity(items[j].Mode)
	})
	return items, nil
}

func maintenanceSeverity(mode MaintenanceMode) int {
	switch mode {
	case MaintenanceEmergencyStop:
		return 4
	case MaintenanceDraining:
		return 3
	case MaintenanceAdmissionFrozen:
		return 2
	case MaintenanceReadOnly:
		return 1
	default:
		return 0
	}
}

func appliesTo(window MaintenanceWindow, scope OperationalScope) bool {
	switch window.ScopeType {
	case ScopePlatform:
		return true
	case ScopeProvider:
		return strings.EqualFold(window.ScopeID, scope.Provider)
	case ScopeGatewayPool:
		return window.ScopeID == scope.GatewayPoolID
	case ScopeSenderPool:
		return window.ScopeID == scope.SenderPoolID
	default:
		return false
	}
}

func blocks(window MaintenanceWindow, action Operation) bool {
	switch window.Mode {
	case MaintenanceEmergencyStop:
		return true
	case MaintenanceDraining:
		if action == OperationDispatchSubmit {
			return !window.AllowActiveDispatch
		}
		return action == OperationCampaignStart || action == OperationCampaignResume || action == OperationSessionChange
	case MaintenanceAdmissionFrozen:
		return action == OperationCampaignStart || action == OperationCampaignResume
	case MaintenanceReadOnly:
		return action == OperationAPIWrite || action == OperationSessionChange
	default:
		return false
	}
}

func (a *MaintenanceAdministration) Check(ctx context.Context, action Operation, scope OperationalScope, at time.Time) error {
	items, err := a.Active(ctx, at)
	if err != nil {
		return err
	}
	for _, item := range items {
		if appliesTo(item, scope) && blocks(item, action) {
			return BlockedError{Window: item, Action: action}
		}
	}
	return nil
}

func (a *MaintenanceAdministration) List(ctx context.Context, status MaintenanceStatus, limit int) ([]MaintenanceWindow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return a.Store.ListMaintenance(ctx, status, limit)
}
func (a *MaintenanceAdministration) Events(ctx context.Context, id string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	return a.Store.ListMaintenanceEvents(ctx, strings.TrimSpace(id), limit)
}

func (a *ConfigurationAdministration) ValidateResolvedChecksum(value Configuration) error {
	canonical, checksum, err := canonicalJSON(value.Value)
	if err != nil {
		return err
	}
	if checksum != value.ValueChecksum || string(canonical) != string(value.Value) {
		return fmt.Errorf("configuration checksum mismatch: %w", ErrInvalid)
	}
	return nil
}
