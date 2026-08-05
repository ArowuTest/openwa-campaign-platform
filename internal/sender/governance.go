package sender

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrSenderNotFound = errors.New("sender record not found")
	ErrSenderConflict = errors.New("sender record version conflict")
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
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	PublicIP        string     `json:"publicIp,omitempty"`
	Status          string     `json:"status"`
	BuildVersion    string     `json:"buildVersion,omitempty"`
	Capacity        int        `json:"capacity"`
	QueueDepth      int64      `json:"queueDepth"`
	Draining        bool       `json:"draining"`
	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`
	Version         int64      `json:"version"`
}

type GovernedSession struct {
	ID                    string     `json:"id"`
	NodeID                string     `json:"nodeId,omitempty"`
	PoolID                string     `json:"poolId,omitempty"`
	MaskedMSISDN          string     `json:"maskedMsisdn"`
	EngineType            string     `json:"engineType"`
	EngineVersion         string     `json:"engineVersion,omitempty"`
	Status                Status     `json:"status"`
	SafeMessagesPerMinute int        `json:"safeMessagesPerMinute"`
	SafeDailyCapacity     int64      `json:"safeDailyCapacity"`
	InFlightLimit         int        `json:"inFlightLimit"`
	SentToday             int64      `json:"sentToday"`
	LastHeartbeatAt       *time.Time `json:"lastHeartbeatAt,omitempty"`
	LastSuccessAt         *time.Time `json:"lastSuccessAt,omitempty"`
	Version               int64      `json:"version"`
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
	RegisterNode(context.Context, Node, string, string) (Node, error)
	HeartbeatNode(context.Context, string, int64, Node, time.Time) (Node, error)
	ListSessions(context.Context) ([]GovernedSession, error)
	RegisterSession(context.Context, GovernedSession, []byte, string, string) (GovernedSession, error)
	TransitionSession(context.Context, string, int64, Status, string, string) (GovernedSession, error)
	HeartbeatSession(context.Context, string, int64, GovernedSession, time.Time) (GovernedSession, error)
	Capacity(context.Context, string, time.Time) (CapacitySummary, error)
}

type GovernanceService struct{ Store GovernanceStore }

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
	value.Status = strings.ToUpper(strings.TrimSpace(value.Status))
	if value.Name == "" || actor == "" || strings.TrimSpace(reason) == "" {
		return Node{}, errors.New("name, actor and reason are required")
	}
	if value.Status == "" {
		value.Status = "READY"
	}
	if value.Capacity < 0 || value.Capacity > 1000 {
		return Node{}, errors.New("node capacity is invalid")
	}
	return s.Store.RegisterNode(ctx, value, actor, reason)
}

func (s *GovernanceService) RegisterSession(ctx context.Context, value GovernedSession, encryptedMSISDN []byte, actor, reason string) (GovernedSession, error) {
	value.MaskedMSISDN = strings.TrimSpace(value.MaskedMSISDN)
	value.EngineType = strings.TrimSpace(value.EngineType)
	if value.MaskedMSISDN == "" || value.EngineType == "" || len(encryptedMSISDN) == 0 || actor == "" || strings.TrimSpace(reason) == "" {
		return GovernedSession{}, errors.New("sender identity, engine, actor and reason are required")
	}
	if value.Status == "" {
		value.Status = StatusReady
	}
	if value.SafeMessagesPerMinute <= 0 || value.SafeDailyCapacity <= 0 || value.InFlightLimit <= 0 || value.InFlightLimit > 100 {
		return GovernedSession{}, errors.New("session capacity limits are invalid")
	}
	return s.Store.RegisterSession(ctx, value, encryptedMSISDN, actor, reason)
}

func (s *GovernanceService) TransitionSession(ctx context.Context, id string, expected int64, status Status, actor, reason string) (GovernedSession, error) {
	if id == "" || expected <= 0 || actor == "" || strings.TrimSpace(reason) == "" {
		return GovernedSession{}, errors.New("id, expected version, actor and reason are required")
	}
	switch status {
	case StatusReady, StatusBusy, StatusPaused, StatusDisconnected, StatusRestricted, StatusRetired:
	default:
		return GovernedSession{}, fmt.Errorf("invalid sender status %q", status)
	}
	return s.Store.TransitionSession(ctx, id, expected, status, actor, reason)
}
