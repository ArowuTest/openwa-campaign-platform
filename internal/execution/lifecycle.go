package execution

import (
	"context"
	"errors"
	"strings"
	"time"

	"campaign-platform/internal/campaign"
)

var ErrLifecycleCommitterRequired = errors.New("atomic campaign lifecycle committer is required")
var ErrExecutionLeaseConflict = errors.New("execution lease ownership or fence conflict")

type ReservationOperation string

const (
	ReservationNone     ReservationOperation = "NONE"
	ReservationActivate ReservationOperation = "ACTIVATE"
	ReservationRelease  ReservationOperation = "RELEASE"
)

type ExecutionLeaseFence struct {
	Owner      string
	FenceToken int64
}

type LifecycleCommit struct {
	Campaign             campaign.Campaign
	ExpectedVersion      int64
	Action               campaign.Action
	EventType            string
	ActorID              string
	Reason               string
	Details              map[string]any
	RoutingPlanID        string
	ReservationOperation ReservationOperation
	OccurredAt           time.Time
	ExecutionLease       *ExecutionLeaseFence
}

func (v LifecycleCommit) Validate() error {
	if strings.TrimSpace(v.Campaign.ID) == "" {
		return errors.New("campaign identity is required")
	}
	if v.ExpectedVersion <= 0 {
		return errors.New("expected campaign version is required")
	}
	if v.Campaign.Version != v.ExpectedVersion+1 {
		return errors.New("candidate campaign version must follow expected version")
	}
	if strings.TrimSpace(string(v.Action)) == "" {
		return errors.New("campaign lifecycle action is required")
	}
	if strings.TrimSpace(v.EventType) == "" {
		return errors.New("campaign lifecycle event type is required")
	}
	if strings.TrimSpace(v.ActorID) == "" {
		return errors.New("campaign lifecycle actor is required")
	}
	if v.OccurredAt.IsZero() {
		return errors.New("campaign lifecycle occurrence time is required")
	}
	if v.ExecutionLease != nil {
		if strings.TrimSpace(v.ExecutionLease.Owner) == "" {
			return errors.New("execution lease owner is required")
		}
		if v.ExecutionLease.FenceToken <= 0 {
			return errors.New("execution lease fence token is required")
		}
	}
	switch v.ReservationOperation {
	case ReservationNone:
	case ReservationActivate, ReservationRelease:
		if strings.TrimSpace(v.RoutingPlanID) == "" {
			return errors.New("routing plan is required for reservation mutation")
		}
	default:
		return errors.New("campaign reservation operation is invalid")
	}
	return nil
}

type LifecycleCommitter interface {
	Commit(context.Context, LifecycleCommit) (campaign.Campaign, error)
}
