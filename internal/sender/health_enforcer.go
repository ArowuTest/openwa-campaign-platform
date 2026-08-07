package sender

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

const HealthProtectionServiceActorID = "00000000-0000-4000-8000-000000000003"

type PostgreSQLHealthSignalSource struct{ DB *sql.DB }

func (s *PostgreSQLHealthSignalSource) ReadHealthSignals(ctx context.Context, session GovernedSession, policy HealthPolicy, now time.Time) (HealthSignals, error) {
	if s == nil || s.DB == nil || strings.TrimSpace(session.ID) == "" {
		return HealthSignals{}, errors.New("sender health signal database and session are required")
	}
	var signals HealthSignals
	err := s.DB.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE status IN ('FAILED_RETRYABLE','FAILED_PERMANENT','UNKNOWN'))
FROM campaign_recipients
WHERE assigned_session_id=$1::uuid AND last_event_at >= $2
  AND status IN ('SENT','DELIVERED','READ','FAILED_RETRYABLE','FAILED_PERMANENT','UNKNOWN')`, session.ID, now.UTC().Add(-policy.FailureWindow)).Scan(&signals.RecentOutcomeCount, &signals.RecentFailureCount)
	if err != nil {
		return HealthSignals{}, err
	}
	err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM sender_governance_events
WHERE object_type='SESSION' AND object_id=$1::uuid AND action='STATUS_DISCONNECTED' AND created_at >= $2`, session.ID, now.UTC().Add(-policy.DisconnectWindow)).Scan(&signals.RecentDisconnectCount)
	if err != nil {
		return HealthSignals{}, err
	}
	return signals, nil
}

type HealthEnforcer struct {
	Governance   *GovernanceService
	ActorID      string
	PollInterval time.Duration
	Clock        func() time.Time
	active       atomic.Int64
}

func (e *HealthEnforcer) now() time.Time {
	if e.Clock != nil {
		return e.Clock().UTC()
	}
	return time.Now().UTC()
}

func (e *HealthEnforcer) Active() int64 {
	if e == nil {
		return 0
	}
	return e.active.Load()
}
func (e *HealthEnforcer) Process(ctx context.Context) (int, error) {
	if e == nil || e.Governance == nil || e.Governance.Store == nil || strings.TrimSpace(e.ActorID) == "" {
		return 0, errors.New("sender health enforcer dependencies are required")
	}
	sessions, err := e.Governance.Store.ListSessions(ctx)
	if err != nil {
		return 0, err
	}
	now := e.now()
	drained := 0
	for _, session := range sessions {
		if session.Status != StatusReady && session.Status != StatusBusy {
			continue
		}
		assessment, assessErr := e.Governance.AssessSession(ctx, session.ID, now)
		if assessErr != nil {
			return drained, assessErr
		}
		if assessment.State == "HEALTHY" {
			continue
		}
		current, getErr := e.Governance.Store.GetSession(ctx, session.ID)
		if getErr != nil {
			return drained, getErr
		}
		if current.Status != StatusReady && current.Status != StatusBusy {
			continue
		}
		reason := fmt.Sprintf("automatic sender health protection: %s", strings.Join(assessment.Reasons, ","))
		_, transitionErr := e.Governance.TransitionSession(ctx, current.ID, current.Version, StatusDraining, e.ActorID, reason)
		if errors.Is(transitionErr, ErrSenderConflict) {
			continue
		}
		if transitionErr != nil {
			return drained, transitionErr
		}
		drained++
	}
	return drained, nil
}

func (e *HealthEnforcer) Run(ctx context.Context) error {
	if e == nil || e.PollInterval <= 0 {
		return errors.New("sender health enforcer poll interval must be positive")
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		e.active.Add(1)
		_, err := e.Process(ctx)
		e.active.Add(-1)
		if err != nil {
			return err
		}
		timer.Reset(e.PollInterval)
	}
}
