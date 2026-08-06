package operations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"campaign-platform/internal/shared/id"
)

type PostgreSQLAlertStore struct{ DB *sql.DB }
type alertScanner interface{ Scan(...any) error }

const alertPolicyColumns = `id::text,name,metric,comparison,threshold,severity,consecutive_evaluations,cooldown_seconds,auto_incident,escalation_steps,status,effective_from,effective_to,created_by::text,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,version,current_breach_count,last_evaluated_at,cooldown_until,created_at,updated_at`

func scanAlertPolicy(s alertScanner) (AlertPolicy, error) {
	var v AlertPolicy
	var raw []byte
	var effectiveTo, lastEval, cool sql.NullTime
	err := s.Scan(&v.ID, &v.Name, &v.Metric, &v.Comparison, &v.Threshold, &v.Severity, &v.ConsecutiveEvaluations, &v.CooldownSeconds, &v.AutoIncident, &raw, &v.Status, &v.EffectiveFrom, &effectiveTo, &v.CreatedBy, &v.SubmittedBy, &v.ApprovedBy, &v.Reason, &v.Version, &v.CurrentBreachCount, &lastEval, &cool, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(raw, &v.EscalationSteps); err != nil {
		return v, err
	}
	if effectiveTo.Valid {
		x := effectiveTo.Time.UTC()
		v.EffectiveTo = &x
	}
	if lastEval.Valid {
		x := lastEval.Time.UTC()
		v.LastEvaluatedAt = &x
	}
	if cool.Valid {
		x := cool.Time.UTC()
		v.CooldownUntil = &x
	}
	return v, nil
}
func alertPolicyEventInsert(ctx context.Context, q interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, v AlertPolicyEvent) error {
	if v.ID == "" {
		var err error
		v.ID, err = id.New()
		if err != nil {
			return err
		}
	}
	raw, err := json.Marshal(v.Evidence)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO operational_alert_policy_events(id,alert_policy_id,event_type,version,actor_id,reason,evidence,occurred_at) VALUES($1::uuid,$2::uuid,$3,$4,$5::uuid,$6,$7::jsonb,$8)`, v.ID, v.PolicyID, v.EventType, v.Version, v.ActorID, v.Reason, string(raw), v.OccurredAt)
	return err
}
func (p *PostgreSQLAlertStore) ListAlertPolicies(ctx context.Context, status AlertPolicyStatus, limit int) ([]AlertPolicy, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+alertPolicyColumns+` FROM operational_alert_policies WHERE ($1='' OR status=$1) ORDER BY created_at DESC,id DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertPolicy{}
	for rows.Next() {
		v, e := scanAlertPolicy(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (p *PostgreSQLAlertStore) GetAlertPolicy(ctx context.Context, key string) (AlertPolicy, error) {
	v, err := scanAlertPolicy(p.DB.QueryRowContext(ctx, `SELECT `+alertPolicyColumns+` FROM operational_alert_policies WHERE id=$1::uuid`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return AlertPolicy{}, ErrNotFound
	}
	return v, err
}
func (p *PostgreSQLAlertStore) CreateAlertPolicy(ctx context.Context, v AlertPolicy, e AlertPolicyEvent) (AlertPolicy, error) {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	steps, err := json.Marshal(v.EscalationSteps)
	if err != nil {
		return v, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO operational_alert_policies(id,name,metric,comparison,threshold,severity,consecutive_evaluations,cooldown_seconds,auto_incident,escalation_steps,status,effective_from,effective_to,created_by,reason,version,created_at,updated_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13,$14::uuid,$15,$16,$17,$18)`, v.ID, v.Name, v.Metric, v.Comparison, v.Threshold, v.Severity, v.ConsecutiveEvaluations, v.CooldownSeconds, v.AutoIncident, string(steps), v.Status, v.EffectiveFrom, v.EffectiveTo, v.CreatedBy, v.Reason, v.Version, v.CreatedAt, v.UpdatedAt)
	if err != nil {
		return v, err
	}
	if err = alertPolicyEventInsert(ctx, tx, e); err != nil {
		return v, err
	}
	return v, tx.Commit()
}
func (p *PostgreSQLAlertStore) updatePolicy(ctx context.Context, v AlertPolicy, expected int64, e AlertPolicyEvent, activate bool) (AlertPolicy, error) {
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT version FROM operational_alert_policies WHERE id=$1::uuid FOR UPDATE`, v.ID).Scan(&version); errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	} else if err != nil {
		return v, err
	}
	if version != expected {
		return v, ErrConflict
	}
	if activate {
		rows, queryErr := tx.QueryContext(ctx, `SELECT id::text,effective_from,effective_to,version FROM operational_alert_policies WHERE id<>$1::uuid AND metric=$2 AND status='ACTIVE' AND (effective_to IS NULL OR effective_to>$3) AND ($4::timestamptz IS NULL OR effective_from<$4) FOR UPDATE`, v.ID, v.Metric, v.EffectiveFrom, v.EffectiveTo)
		if queryErr != nil {
			return v, queryErr
		}
		type overlap struct {
			id      string
			start   time.Time
			end     sql.NullTime
			version int64
		}
		var overlaps []overlap
		for rows.Next() {
			var o overlap
			if scanErr := rows.Scan(&o.id, &o.start, &o.end, &o.version); scanErr != nil {
				_ = rows.Close()
				return v, scanErr
			}
			overlaps = append(overlaps, o)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			_ = rows.Close()
			return v, rowsErr
		}
		if closeErr := rows.Close(); closeErr != nil {
			return v, closeErr
		}
		for _, o := range overlaps {
			if !v.EffectiveFrom.After(o.start) {
				return v, ErrConflict
			}
			res, updateErr := tx.ExecContext(ctx, `UPDATE operational_alert_policies SET effective_to=$2,version=version+1,updated_at=$3 WHERE id=$1::uuid AND version=$4`, o.id, v.EffectiveFrom, v.UpdatedAt, o.version)
			if updateErr != nil {
				return v, updateErr
			}
			changed, rowsErr := res.RowsAffected()
			if rowsErr != nil {
				return v, rowsErr
			}
			if changed != 1 {
				return v, ErrConflict
			}
			superseded := AlertPolicyEvent{PolicyID: o.id, EventType: "SUPERSEDED", Version: o.version + 1, ActorID: e.ActorID, Reason: e.Reason, Evidence: map[string]any{"supersededById": v.ID, "effectiveTo": v.EffectiveFrom}, OccurredAt: e.OccurredAt}
			if eventErr := alertPolicyEventInsert(ctx, tx, superseded); eventErr != nil {
				return v, eventErr
			}
		}
	}
	steps, err := json.Marshal(v.EscalationSteps)
	if err != nil {
		return v, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE operational_alert_policies SET name=$2,comparison=$3,threshold=$4,severity=$5,consecutive_evaluations=$6,cooldown_seconds=$7,auto_incident=$8,escalation_steps=$9::jsonb,status=$10,effective_from=$11,effective_to=$12,submitted_by=NULLIF($13,'')::uuid,approved_by=NULLIF($14,'')::uuid,reason=$15,version=$16,updated_at=$17 WHERE id=$1::uuid AND version=$18`, v.ID, v.Name, v.Comparison, v.Threshold, v.Severity, v.ConsecutiveEvaluations, v.CooldownSeconds, v.AutoIncident, string(steps), v.Status, v.EffectiveFrom, v.EffectiveTo, v.SubmittedBy, v.ApprovedBy, v.Reason, v.Version, v.UpdatedAt, expected)
	if err != nil {
		return v, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return v, err
	}
	if n != 1 {
		return v, ErrConflict
	}
	if err = alertPolicyEventInsert(ctx, tx, e); err != nil {
		return v, err
	}
	return v, tx.Commit()
}
func (p *PostgreSQLAlertStore) UpdateAlertPolicy(ctx context.Context, v AlertPolicy, e int64, ev AlertPolicyEvent) (AlertPolicy, error) {
	return p.updatePolicy(ctx, v, e, ev, false)
}
func (p *PostgreSQLAlertStore) ActivateAlertPolicy(ctx context.Context, v AlertPolicy, e int64, ev AlertPolicyEvent) (AlertPolicy, error) {
	return p.updatePolicy(ctx, v, e, ev, true)
}
func (p *PostgreSQLAlertStore) ListAlertPolicyEvents(ctx context.Context, key string, limit int) ([]AlertPolicyEvent, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,alert_policy_id::text,event_type,version,actor_id::text,reason,evidence,occurred_at FROM operational_alert_policy_events WHERE alert_policy_id=$1::uuid ORDER BY occurred_at DESC,id DESC LIMIT $2`, key, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertPolicyEvent{}
	for rows.Next() {
		var v AlertPolicyEvent
		var raw []byte
		if err = rows.Scan(&v.ID, &v.PolicyID, &v.EventType, &v.Version, &v.ActorID, &v.Reason, &raw, &v.OccurredAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &v.Evidence); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (p *PostgreSQLAlertStore) ListActiveAlertPolicies(ctx context.Context, now time.Time) ([]AlertPolicy, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+alertPolicyColumns+` FROM operational_alert_policies WHERE status='ACTIVE' AND effective_from<=$1 AND (effective_to IS NULL OR effective_to>$1) ORDER BY metric,effective_from DESC`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertPolicy{}
	for rows.Next() {
		v, e := scanAlertPolicy(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

const alertColumns = `id::text,policy_id::text,status,severity,observed_value,threshold,occurrence_count,first_triggered_at,last_observed_at,coalesce(acknowledged_by::text,''),acknowledged_at,resolved_at,coalesce(linked_incident_id::text,''),escalation_index,next_escalation_at,coalesce(escalation_lease_owner,''),escalation_lease_version,escalation_lease_expires_at,version,updated_at`

func scanAlert(s alertScanner) (Alert, error) {
	var v Alert
	var ack, res, next, leaseExpiry sql.NullTime
	err := s.Scan(&v.ID, &v.PolicyID, &v.Status, &v.Severity, &v.ObservedValue, &v.Threshold, &v.OccurrenceCount, &v.FirstTriggeredAt, &v.LastObservedAt, &v.AcknowledgedBy, &ack, &res, &v.LinkedIncidentID, &v.EscalationIndex, &next, &v.LeaseOwner, &v.LeaseVersion, &leaseExpiry, &v.Version, &v.UpdatedAt)
	if ack.Valid {
		x := ack.Time.UTC()
		v.AcknowledgedAt = &x
	}
	if res.Valid {
		x := res.Time.UTC()
		v.ResolvedAt = &x
	}
	if next.Valid {
		x := next.Time.UTC()
		v.NextEscalationAt = &x
	}
	if leaseExpiry.Valid {
		x := leaseExpiry.Time.UTC()
		v.LeaseExpiresAt = &x
	}
	return v, err
}
func insertAlertEvent(ctx context.Context, q interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, v AlertEvent) error {
	if v.ID == "" {
		var err error
		v.ID, err = id.New()
		if err != nil {
			return err
		}
	}
	raw, err := json.Marshal(v.Evidence)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO operational_alert_events(id,alert_id,event_type,actor_id,observed_value,detail,evidence,occurred_at) VALUES($1::uuid,$2::uuid,$3,NULLIF($4,'')::uuid,$5,$6,$7::jsonb,$8)`, v.ID, v.AlertID, v.EventType, v.ActorID, v.ObservedValue, v.Detail, string(raw), v.OccurredAt)
	return err
}
func (p *PostgreSQLAlertStore) ObserveAlertPolicy(ctx context.Context, policy AlertPolicy, value float64, isBreach bool, now time.Time) (*Alert, error) {
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	current, err := scanAlertPolicy(tx.QueryRowContext(ctx, `SELECT `+alertPolicyColumns+` FROM operational_alert_policies WHERE id=$1::uuid FOR UPDATE`, policy.ID))
	if err != nil {
		return nil, err
	}
	if current.Status != AlertPolicyActive {
		return nil, nil
	}
	if !isBreach {
		_, err = tx.ExecContext(ctx, `UPDATE operational_alert_policies SET current_breach_count=0,last_evaluated_at=$2,updated_at=$2 WHERE id=$1::uuid`, policy.ID, now)
		if err != nil {
			return nil, err
		}
		a, aerr := scanAlert(tx.QueryRowContext(ctx, `SELECT `+alertColumns+` FROM operational_alerts WHERE policy_id=$1::uuid AND status IN ('ACTIVE','ACKNOWLEDGED') FOR UPDATE`, policy.ID))
		if errors.Is(aerr, sql.ErrNoRows) {
			return nil, tx.Commit()
		} else if aerr != nil {
			return nil, aerr
		}
		a.Status = AlertResolved
		a.ResolvedAt = &now
		a.NextEscalationAt = nil
		a.Version++
		a.UpdatedAt = now
		_, err = tx.ExecContext(ctx, `UPDATE operational_alerts SET status='RESOLVED',resolved_at=$2,next_escalation_at=NULL,escalation_lease_owner=NULL,escalation_lease_expires_at=NULL,version=$3,updated_at=$2 WHERE id=$1::uuid`, a.ID, now, a.Version)
		if err != nil {
			return nil, err
		}
		if err = insertAlertEvent(ctx, tx, AlertEvent{AlertID: a.ID, EventType: "RESOLVED", ObservedValue: &value, Detail: "metric recovered", OccurredAt: now}); err != nil {
			return nil, err
		}
		cool := now.Add(time.Duration(current.CooldownSeconds) * time.Second)
		_, err = tx.ExecContext(ctx, `UPDATE operational_alert_policies SET cooldown_until=$2 WHERE id=$1::uuid`, policy.ID, cool)
		if err != nil {
			return nil, err
		}
		return nil, tx.Commit()
	}
	current.CurrentBreachCount++
	_, err = tx.ExecContext(ctx, `UPDATE operational_alert_policies SET current_breach_count=$2,last_evaluated_at=$3,updated_at=$3 WHERE id=$1::uuid`, policy.ID, current.CurrentBreachCount, now)
	if err != nil {
		return nil, err
	}
	if current.CurrentBreachCount < current.ConsecutiveEvaluations {
		return nil, tx.Commit()
	}
	a, aerr := scanAlert(tx.QueryRowContext(ctx, `SELECT `+alertColumns+` FROM operational_alerts WHERE policy_id=$1::uuid AND status IN ('ACTIVE','ACKNOWLEDGED') FOR UPDATE`, policy.ID))
	if aerr == nil {
		a.ObservedValue = value
		a.OccurrenceCount++
		a.LastObservedAt = now
		a.Version++
		a.UpdatedAt = now
		_, err = tx.ExecContext(ctx, `UPDATE operational_alerts SET observed_value=$2,occurrence_count=$3,last_observed_at=$4,version=$5,updated_at=$4 WHERE id=$1::uuid`, a.ID, value, a.OccurrenceCount, now, a.Version)
		if err != nil {
			return nil, err
		}
		if err = insertAlertEvent(ctx, tx, AlertEvent{AlertID: a.ID, EventType: "OBSERVED", ObservedValue: &value, Detail: "threshold remains breached", OccurredAt: now}); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return &a, nil
	}
	if !errors.Is(aerr, sql.ErrNoRows) {
		return nil, aerr
	}
	if current.CooldownUntil != nil && current.CooldownUntil.After(now) {
		return nil, tx.Commit()
	}
	identifier, err := id.New()
	if err != nil {
		return nil, err
	}
	a = Alert{ID: identifier, PolicyID: policy.ID, Status: AlertActive, Severity: policy.Severity, ObservedValue: value, Threshold: policy.Threshold, OccurrenceCount: 1, FirstTriggeredAt: now, LastObservedAt: now, Version: 1, UpdatedAt: now}
	if len(policy.EscalationSteps) > 0 {
		x := now.Add(time.Duration(policy.EscalationSteps[0].AfterSeconds) * time.Second)
		a.NextEscalationAt = &x
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO operational_alerts(id,policy_id,status,severity,observed_value,threshold,occurrence_count,first_triggered_at,last_observed_at,escalation_index,next_escalation_at,version,updated_at) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,1,$7,$7,0,$8,1,$7)`, a.ID, a.PolicyID, a.Status, a.Severity, a.ObservedValue, a.Threshold, now, a.NextEscalationAt)
	if err != nil {
		return nil, err
	}
	if err = insertAlertEvent(ctx, tx, AlertEvent{AlertID: a.ID, EventType: "TRIGGERED", ObservedValue: &value, Detail: "governed threshold breached", OccurredAt: now}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &a, nil
}
func (p *PostgreSQLAlertStore) ListAlerts(ctx context.Context, status AlertStatus, severity Severity, limit int) ([]Alert, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+alertColumns+` FROM operational_alerts WHERE ($1='' OR status=$1) AND ($2='' OR severity=$2) ORDER BY last_observed_at DESC,id DESC LIMIT $3`, status, severity, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		v, e := scanAlert(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (p *PostgreSQLAlertStore) GetAlert(ctx context.Context, key string) (Alert, error) {
	v, err := scanAlert(p.DB.QueryRowContext(ctx, `SELECT `+alertColumns+` FROM operational_alerts WHERE id=$1::uuid`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return Alert{}, ErrNotFound
	}
	return v, err
}
func (p *PostgreSQLAlertStore) AcknowledgeAlert(ctx context.Context, key string, expected int64, actor, reason string, now time.Time) (Alert, error) {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return Alert{}, err
	}
	defer tx.Rollback()
	v, err := scanAlert(tx.QueryRowContext(ctx, `SELECT `+alertColumns+` FROM operational_alerts WHERE id=$1::uuid FOR UPDATE`, key))
	if err != nil {
		return v, err
	}
	if v.Version != expected || v.Status != AlertActive {
		return Alert{}, ErrConflict
	}
	v.Status = AlertAcknowledged
	v.AcknowledgedBy = actor
	v.AcknowledgedAt = &now
	v.NextEscalationAt = nil
	v.Version++
	v.UpdatedAt = now
	_, err = tx.ExecContext(ctx, `UPDATE operational_alerts SET status='ACKNOWLEDGED',acknowledged_by=$2::uuid,acknowledged_at=$3,next_escalation_at=NULL,escalation_lease_owner=NULL,escalation_lease_expires_at=NULL,version=$4,updated_at=$3 WHERE id=$1::uuid`, key, actor, now, v.Version)
	if err != nil {
		return v, err
	}
	if err = insertAlertEvent(ctx, tx, AlertEvent{AlertID: key, EventType: "ACKNOWLEDGED", ActorID: actor, Detail: reason, OccurredAt: now}); err != nil {
		return v, err
	}
	return v, tx.Commit()
}
func (p *PostgreSQLAlertStore) ListAlertEvents(ctx context.Context, key string, limit int) ([]AlertEvent, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,alert_id::text,event_type,coalesce(actor_id::text,''),observed_value,detail,evidence,occurred_at FROM operational_alert_events WHERE alert_id=$1::uuid ORDER BY occurred_at ASC,id ASC LIMIT $2`, key, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertEvent{}
	for rows.Next() {
		var v AlertEvent
		var value sql.NullFloat64
		var raw []byte
		if err = rows.Scan(&v.ID, &v.AlertID, &v.EventType, &v.ActorID, &value, &v.Detail, &raw, &v.OccurredAt); err != nil {
			return nil, err
		}
		if value.Valid {
			x := value.Float64
			v.ObservedValue = &x
		}
		if err = json.Unmarshal(raw, &v.Evidence); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (p *PostgreSQLAlertStore) ClaimEscalations(ctx context.Context, worker string, now time.Time, lease time.Duration, limit int) ([]Alert, error) {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id::text FROM operational_alerts WHERE status='ACTIVE' AND next_escalation_at<=$1 AND (escalation_lease_expires_at IS NULL OR escalation_lease_expires_at<=$1) ORDER BY next_escalation_at,id FOR UPDATE SKIP LOCKED LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, key)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	expires := now.Add(lease)
	out := make([]Alert, 0, len(ids))
	for _, key := range ids {
		v, updateErr := scanAlert(tx.QueryRowContext(ctx, `UPDATE operational_alerts SET escalation_lease_owner=$2,escalation_lease_version=escalation_lease_version+1,escalation_lease_expires_at=$3,updated_at=$4 WHERE id=$1::uuid RETURNING `+alertColumns, key, worker, expires, now))
		if updateErr != nil {
			return nil, updateErr
		}
		out = append(out, v)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
func (p *PostgreSQLAlertStore) EscalateAlert(ctx context.Context, v Alert, step EscalationStep, now time.Time) (Alert, error) {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	cur, err := scanAlert(tx.QueryRowContext(ctx, `SELECT `+alertColumns+` FROM operational_alerts WHERE id=$1::uuid FOR UPDATE`, v.ID))
	if err != nil {
		return v, err
	}
	if cur.Version != v.Version || cur.Status != AlertActive || cur.LeaseOwner != v.LeaseOwner || cur.LeaseVersion != v.LeaseVersion || cur.LeaseExpiresAt == nil || !cur.LeaseExpiresAt.After(now) {
		return Alert{}, ErrConflict
	}
	policy, err := scanAlertPolicy(tx.QueryRowContext(ctx, `SELECT `+alertPolicyColumns+` FROM operational_alert_policies WHERE id=$1::uuid`, cur.PolicyID))
	if err != nil {
		return v, err
	}
	cur.EscalationIndex++
	cur.Version++
	cur.UpdatedAt = now
	if cur.EscalationIndex < len(policy.EscalationSteps) {
		x := cur.FirstTriggeredAt.Add(time.Duration(policy.EscalationSteps[cur.EscalationIndex].AfterSeconds) * time.Second)
		cur.NextEscalationAt = &x
	} else {
		cur.NextEscalationAt = nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE operational_alerts SET escalation_index=$2,next_escalation_at=$3,escalation_lease_owner=NULL,escalation_lease_expires_at=NULL,version=$4,updated_at=$5 WHERE id=$1::uuid`, cur.ID, cur.EscalationIndex, cur.NextEscalationAt, cur.Version, now)
	if err != nil {
		return cur, err
	}
	if err = insertAlertEvent(ctx, tx, AlertEvent{AlertID: cur.ID, EventType: "ESCALATED", Detail: "escalated to " + step.Target, Evidence: map[string]any{"target": step.Target, "index": cur.EscalationIndex}, OccurredAt: now}); err != nil {
		return cur, err
	}
	nid, err := id.New()
	if err != nil {
		return cur, err
	}
	payload, err := json.Marshal(map[string]any{"alertId": cur.ID, "severity": cur.Severity, "target": step.Target})
	if err != nil {
		return cur, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO operational_notifications(id,alert_id,incident_id,channel,target,status,payload,attempt_count,delivered_at,created_at,updated_at) VALUES($1::uuid,$2::uuid,NULLIF($3,'')::uuid,'PORTAL',$4,'DELIVERED',$5::jsonb,1,$6,$6,$6)`, nid, cur.ID, cur.LinkedIncidentID, step.Target, string(payload), now)
	if err != nil {
		return cur, err
	}
	return cur, tx.Commit()
}
func (p *PostgreSQLAlertStore) CreateIncidentForAlert(ctx context.Context, a Alert, policy AlertPolicy, now time.Time) (string, error) {
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	cur, err := scanAlert(tx.QueryRowContext(ctx, `SELECT `+alertColumns+` FROM operational_alerts WHERE id=$1::uuid FOR UPDATE`, a.ID))
	if err != nil {
		return "", err
	}
	if cur.LinkedIncidentID != "" {
		return cur.LinkedIncidentID, tx.Commit()
	}
	iid, err := id.New()
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO operations_incidents(id,category,severity,status,summary,detail,created_at,updated_at,version) VALUES($1::uuid,'AUTOMATED_ALERT',$2,'OPEN',$3,$4,$5,$5,1)`, iid, policy.Severity, "Operational alert: "+policy.Name, "Automatically created from alert "+a.ID, now)
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `UPDATE operational_alerts SET linked_incident_id=$2::uuid,version=version+1,updated_at=$3 WHERE id=$1::uuid`, a.ID, iid, now)
	if err != nil {
		return "", err
	}
	if err = insertAlertEvent(ctx, tx, AlertEvent{AlertID: a.ID, EventType: "INCIDENT_CREATED", Detail: "incident created", Evidence: map[string]any{"incidentId": iid}, OccurredAt: now}); err != nil {
		return "", err
	}
	if err = insertIncidentEvent(ctx, tx, IncidentEvent{IncidentID: iid, EventType: "CREATED", Detail: "Operational alert: " + policy.Name, OccurredAt: now}); err != nil {
		return "", err
	}
	if err = insertIncidentEvent(ctx, tx, IncidentEvent{IncidentID: iid, EventType: "ALERT_LINKED", Detail: "linked to alert " + a.ID, Evidence: map[string]any{"alertId": a.ID}, OccurredAt: now}); err != nil {
		return "", err
	}
	return iid, tx.Commit()
}
func insertIncidentEvent(ctx context.Context, q interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, v IncidentEvent) error {
	if v.ID == "" {
		var err error
		v.ID, err = id.New()
		if err != nil {
			return err
		}
	}
	raw, err := json.Marshal(v.Evidence)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO operational_incident_events(id,incident_id,event_type,actor_id,detail,evidence,occurred_at) VALUES($1::uuid,$2::uuid,$3,NULLIF($4,'')::uuid,$5,$6::jsonb,$7)`, v.ID, v.IncidentID, v.EventType, v.ActorID, v.Detail, string(raw), v.OccurredAt)
	return err
}
func (p *PostgreSQLAlertStore) AddIncidentEvent(ctx context.Context, v IncidentEvent) error {
	return insertIncidentEvent(ctx, p.DB, v)
}
func (p *PostgreSQLAlertStore) ListIncidentEvents(ctx context.Context, key string, limit int) ([]IncidentEvent, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,incident_id::text,event_type,coalesce(actor_id::text,''),detail,evidence,occurred_at FROM operational_incident_events WHERE incident_id=$1::uuid ORDER BY occurred_at ASC,id ASC LIMIT $2`, key, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IncidentEvent{}
	for rows.Next() {
		var v IncidentEvent
		var raw []byte
		if err = rows.Scan(&v.ID, &v.IncidentID, &v.EventType, &v.ActorID, &v.Detail, &raw, &v.OccurredAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &v.Evidence); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (p *PostgreSQLAlertStore) ListNotifications(ctx context.Context, status string, limit int) ([]Notification, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,coalesce(alert_id::text,''),coalesce(incident_id::text,''),channel,target,status,payload,attempt_count,delivered_at,coalesce(last_error_code,''),created_at,updated_at FROM operational_notifications WHERE ($1='' OR status=$1) ORDER BY created_at DESC,id DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var v Notification
		var raw []byte
		var delivered sql.NullTime
		if err = rows.Scan(&v.ID, &v.AlertID, &v.IncidentID, &v.Channel, &v.Target, &v.Status, &raw, &v.AttemptCount, &delivered, &v.LastErrorCode, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		if delivered.Valid {
			x := delivered.Time.UTC()
			v.DeliveredAt = &x
		}
		if err = json.Unmarshal(raw, &v.Payload); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (p *PostgreSQLAlertStore) String() string { return fmt.Sprintf("PostgreSQLAlertStore(%p)", p.DB) }
