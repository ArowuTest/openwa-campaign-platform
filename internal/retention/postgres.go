package retention

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"campaign-platform/internal/shared/id"
	"campaign-platform/internal/storage"
)

type PostgreSQLStore struct{ DB *sql.DB }
type scanner interface{ Scan(...any) error }

const policyColumns = `id::text,name,object_type,action,scope_type,scope_id,coalesce(retention_days,0),respect_legal_holds,status,effective_from,effective_to,created_by::text,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,version,created_at,updated_at`

func scanPolicy(s scanner) (Policy, error) {
	var v Policy
	var end sql.NullTime
	err := s.Scan(&v.ID, &v.Name, &v.ObjectType, &v.Action, &v.ScopeType, &v.ScopeID, &v.RetentionDays, &v.RespectLegalHolds, &v.Status, &v.EffectiveFrom, &end, &v.CreatedBy, &v.SubmittedBy, &v.ApprovedBy, &v.Reason, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if end.Valid {
		x := end.Time.UTC()
		v.EffectiveTo = &x
	}
	return v, err
}
func (p *PostgreSQLStore) ListPolicies(ctx context.Context, status Status, limit int) ([]Policy, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+policyColumns+` FROM retention_policies WHERE ($1='' OR status=$1) ORDER BY created_at DESC,id DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Policy{}
	for rows.Next() {
		v, e := scanPolicy(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (p *PostgreSQLStore) ListPolicyPage(ctx context.Context, status Status, limit int, before *time.Time, beforeID string) ([]Policy, error) {
	if p == nil || p.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	rows, err := p.DB.QueryContext(ctx, `SELECT `+policyColumns+` FROM retention_policies
WHERE ($1='' OR status=$1)
  AND ($3::timestamptz IS NULL OR created_at<$3 OR (created_at=$3 AND id<NULLIF($4,'')::uuid))
ORDER BY created_at DESC,id DESC LIMIT $2`, string(status), limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Policy, 0, limit)
	for rows.Next() {
		value, scanErr := scanPolicy(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (p *PostgreSQLStore) GetPolicy(ctx context.Context, key string) (Policy, error) {
	v, err := scanPolicy(p.DB.QueryRowContext(ctx, `SELECT `+policyColumns+` FROM retention_policies WHERE id=$1::uuid`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return Policy{}, ErrNotFound
	}
	return v, err
}
func insertPolicyEvent(ctx context.Context, q interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, ev Event) error {
	if ev.ID == "" {
		var err error
		ev.ID, err = id.New()
		if err != nil {
			return err
		}
	}
	raw, err := json.Marshal(ev.Evidence)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO retention_policy_events(id,retention_policy_id,event_type,version,actor_id,reason,evidence,occurred_at) VALUES($1::uuid,$2::uuid,$3,$4,$5::uuid,$6,$7::jsonb,$8)`, ev.ID, ev.PolicyID, ev.EventType, ev.Version, ev.ActorID, ev.Reason, string(raw), ev.OccurredAt)
	return err
}
func (p *PostgreSQLStore) CreatePolicy(ctx context.Context, v Policy, ev Event) (Policy, error) {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return Policy{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO retention_policies(id,name,object_type,action,scope_type,scope_id,retention_days,respect_legal_holds,status,effective_from,effective_to,created_by,reason,version,created_at,updated_at) VALUES($1::uuid,$2,$3,$4,$5,$6,NULLIF($7,0),$8,$9,$10,$11,$12::uuid,$13,$14,$15,$16)`, v.ID, v.Name, v.ObjectType, v.Action, v.ScopeType, v.ScopeID, v.RetentionDays, v.RespectLegalHolds, v.Status, v.EffectiveFrom, v.EffectiveTo, v.CreatedBy, v.Reason, v.Version, v.CreatedAt, v.UpdatedAt)
	if err != nil {
		return Policy{}, err
	}
	if err = insertPolicyEvent(ctx, tx, ev); err != nil {
		return Policy{}, err
	}
	return v, tx.Commit()
}
func (p *PostgreSQLStore) updatePolicy(ctx context.Context, v Policy, expected int64, ev Event, activate bool) (Policy, error) {
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Policy{}, err
	}
	defer tx.Rollback()
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT version FROM retention_policies WHERE id=$1::uuid FOR UPDATE`, v.ID).Scan(&version); errors.Is(err, sql.ErrNoRows) {
		return Policy{}, ErrNotFound
	} else if err != nil {
		return Policy{}, err
	}
	if version != expected {
		return Policy{}, ErrConflict
	}
	if activate {
		rows, queryErr := tx.QueryContext(ctx, `SELECT id::text,effective_from,effective_to,version FROM retention_policies WHERE id<>$1::uuid AND object_type=$2 AND scope_type=$3 AND scope_id=$4 AND status='ACTIVE' AND (effective_to IS NULL OR effective_to>$5) AND ($6::timestamptz IS NULL OR effective_from<$6) FOR UPDATE`, v.ID, v.ObjectType, v.ScopeType, v.ScopeID, v.EffectiveFrom, v.EffectiveTo)
		if queryErr != nil {
			return Policy{}, queryErr
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
				return Policy{}, scanErr
			}
			overlaps = append(overlaps, o)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			_ = rows.Close()
			return Policy{}, rowsErr
		}
		if closeErr := rows.Close(); closeErr != nil {
			return Policy{}, closeErr
		}
		for _, o := range overlaps {
			if !v.EffectiveFrom.After(o.start) {
				return Policy{}, ErrConflict
			}
			res, updateErr := tx.ExecContext(ctx, `UPDATE retention_policies SET effective_to=$2,version=version+1,updated_at=$3 WHERE id=$1::uuid AND version=$4`, o.id, v.EffectiveFrom, v.UpdatedAt, o.version)
			if updateErr != nil {
				return Policy{}, updateErr
			}
			changed, rowsErr := res.RowsAffected()
			if rowsErr != nil {
				return Policy{}, rowsErr
			}
			if changed != 1 {
				return Policy{}, ErrConflict
			}
			superseded := Event{PolicyID: o.id, EventType: "SUPERSEDED", Version: o.version + 1, ActorID: ev.ActorID, Reason: ev.Reason, Evidence: map[string]any{"supersededById": v.ID, "effectiveTo": v.EffectiveFrom}, OccurredAt: ev.OccurredAt}
			if eventErr := insertPolicyEvent(ctx, tx, superseded); eventErr != nil {
				return Policy{}, eventErr
			}
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE retention_policies SET name=$2,action=$3,retention_days=NULLIF($4,0),respect_legal_holds=$5,status=$6,effective_from=$7,effective_to=$8,submitted_by=NULLIF($9,'')::uuid,approved_by=NULLIF($10,'')::uuid,reason=$11,version=$12,updated_at=$13 WHERE id=$1::uuid AND version=$14`, v.ID, v.Name, v.Action, v.RetentionDays, v.RespectLegalHolds, v.Status, v.EffectiveFrom, v.EffectiveTo, v.SubmittedBy, v.ApprovedBy, v.Reason, v.Version, v.UpdatedAt, expected)
	if err != nil {
		return Policy{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Policy{}, err
	}
	if n != 1 {
		return Policy{}, ErrConflict
	}
	if err = insertPolicyEvent(ctx, tx, ev); err != nil {
		return Policy{}, err
	}
	return v, tx.Commit()
}
func (p *PostgreSQLStore) UpdatePolicy(ctx context.Context, v Policy, e int64, ev Event) (Policy, error) {
	return p.updatePolicy(ctx, v, e, ev, false)
}
func (p *PostgreSQLStore) ActivatePolicy(ctx context.Context, v Policy, e int64, ev Event) (Policy, error) {
	return p.updatePolicy(ctx, v, e, ev, true)
}
func (p *PostgreSQLStore) ListEvents(ctx context.Context, key string, limit int) ([]Event, error) {
	return p.ListEventPage(ctx, key, limit, nil, "")
}
func (p *PostgreSQLStore) ListEventPage(ctx context.Context, key string, limit int, before *time.Time, beforeID string) ([]Event, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,retention_policy_id::text,event_type,version,actor_id::text,reason,evidence,occurred_at FROM retention_policy_events WHERE retention_policy_id=$1::uuid AND ($3::timestamptz IS NULL OR occurred_at<$3 OR (occurred_at=$3 AND id<NULLIF($4,'')::uuid)) ORDER BY occurred_at DESC,id DESC LIMIT $2`, key, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var v Event
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

func (p *PostgreSQLStore) ScheduleDue(ctx context.Context, now time.Time, limit int) (int, error) {
	policies, err := p.activePolicies(ctx, now)
	if err != nil {
		return 0, err
	}
	scheduled := 0
	for _, policy := range policies {
		if policy.Action == ActionRetainIndefinitely {
			continue
		}
		remaining := limit - scheduled
		if remaining <= 0 {
			break
		}
		n, e := p.schedulePolicy(ctx, policy, now, remaining)
		if e != nil {
			return scheduled, fmt.Errorf("schedule %s: %w", policy.ObjectType, e)
		}
		scheduled += n
	}
	return scheduled, nil
}
func (p *PostgreSQLStore) activePolicies(ctx context.Context, now time.Time) ([]Policy, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+policyColumns+` FROM retention_policies WHERE status='ACTIVE' AND effective_from<=$1 AND (effective_to IS NULL OR effective_to>$1) ORDER BY CASE scope_type WHEN 'ORGANISATION' THEN 1 ELSE 2 END,effective_from DESC`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Policy{}
	for rows.Next() {
		v, e := scanPolicy(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (p *PostgreSQLStore) schedulePolicy(ctx context.Context, policy Policy, now time.Time, limit int) (int, error) {
	cutoff := now.AddDate(0, 0, -policy.RetentionDays)
	// Organisation-specific policies override the platform policy for the same
	// object type and effective instant. The NOT EXISTS clauses below prevent
	// the same object being scheduled once under each policy.
	override := `NOT EXISTS (
		SELECT 1 FROM retention_policies rp
		WHERE rp.object_type=$6 AND rp.scope_type='ORGANISATION'
		  AND rp.scope_id=org_id::text AND rp.status='ACTIVE'
		  AND rp.effective_from<=$4 AND (rp.effective_to IS NULL OR rp.effective_to>$4)
	)`
	var query string
	args := []any{policy.ID, policy.Action, limit, now, policy.ScopeID, policy.ObjectType}
	switch policy.ObjectType {
	case ObjectInboundContent:
		query = `WITH candidates AS (
			SELECT r.id,r.content_retain_until,c.organisation_id AS org_id
			FROM inbound_replies r JOIN campaigns c ON c.id=r.campaign_id
			WHERE r.content_redacted_at IS NULL AND r.legal_hold=false AND r.content_retain_until<=$4
			  AND ($5='' OR c.organisation_id::text=$5)
		)
		INSERT INTO retention_jobs(retention_policy_id,object_type,object_id,action,status,available_at,evidence)
		SELECT $1::uuid,'INBOUND_CONTENT',id::text,$2,'PENDING',$4,jsonb_build_object('retainUntil',content_retain_until)
		FROM candidates WHERE ($5<>'' OR ` + override + `)
		ORDER BY content_retain_until,id LIMIT $3 ON CONFLICT DO NOTHING`
	case ObjectAudienceImportSource:
		query = `WITH candidates AS (
			SELECT i.id,i.object_key,i.source_expires_at,i.organisation_id AS org_id
			FROM audience_imports i
			WHERE i.source_deleted_at IS NULL AND i.source_expires_at<=$4
			  AND i.status IN ('COMPLETED','COMPLETED_WITH_EXCEPTIONS','ROLLED_BACK','REJECTED','FAILED','CANCELLED')
			  AND ($5='' OR i.organisation_id::text=$5)
		)
		INSERT INTO retention_jobs(retention_policy_id,object_type,object_id,object_key,action,status,available_at,evidence)
		SELECT $1::uuid,'AUDIENCE_IMPORT_SOURCE',id::text,object_key,$2,'PENDING',$4,jsonb_build_object('sourceExpiresAt',source_expires_at)
		FROM candidates WHERE ($5<>'' OR ` + override + `)
		ORDER BY source_expires_at,id LIMIT $3 ON CONFLICT DO NOTHING`
	case ObjectExportObject:
		query = `WITH candidates AS (
			SELECT e.id,e.object_key,e.expires_at,c.organisation_id AS org_id
			FROM export_requests e LEFT JOIN campaigns c ON e.kind='CAMPAIGN_REPORT' AND c.id=e.object_id
			WHERE e.status='READY' AND e.expires_at<=$4 AND e.object_key IS NOT NULL
			  AND ($5='' OR c.organisation_id::text=$5)
		)
		INSERT INTO retention_jobs(retention_policy_id,object_type,object_id,object_key,action,status,available_at,evidence)
		SELECT $1::uuid,'EXPORT_OBJECT',id::text,object_key,$2,'PENDING',$4,jsonb_build_object('expiresAt',expires_at)
		FROM candidates WHERE ($5<>'' OR org_id IS NULL OR ` + override + `)
		ORDER BY expires_at,id LIMIT $3 ON CONFLICT DO NOTHING`
	case ObjectIncident:
		args = append(args, cutoff)
		query = `WITH candidates AS (
			SELECT i.id,i.created_at,c.organisation_id AS org_id
			FROM operations_incidents i LEFT JOIN campaigns c ON c.id=i.campaign_id
			WHERE i.status='CLOSED' AND i.created_at<=$7 AND ($5='' OR c.organisation_id::text=$5)
		)
		INSERT INTO retention_jobs(retention_policy_id,object_type,object_id,action,status,available_at,evidence)
		SELECT $1::uuid,'INCIDENT',id::text,$2,'PENDING',$4,jsonb_build_object('createdAt',created_at)
		FROM candidates WHERE ($5<>'' OR org_id IS NULL OR ` + override + `)
		ORDER BY created_at,id LIMIT $3 ON CONFLICT DO NOTHING`
	case ObjectPrivacyCase:
		args = append(args, cutoff)
		query = `WITH candidates AS (
			SELECT pc.id,pc.created_at,pc.organisation_id AS org_id,pc.subject_lookup_hmac
			FROM privacy_cases pc
			WHERE pc.status IN ('COMPLETED','REJECTED','CANCELLED') AND pc.created_at<=$7
			  AND ($5='' OR pc.organisation_id::text=$5)
			  AND NOT EXISTS (
				SELECT 1 FROM privacy_legal_holds h
				WHERE h.subject_lookup_hmac=pc.subject_lookup_hmac AND h.status='ACTIVE'
				  AND h.released_at IS NULL AND (h.expires_at IS NULL OR h.expires_at>$4)
			  )
		)
		INSERT INTO retention_jobs(retention_policy_id,object_type,object_id,action,status,available_at,evidence)
		SELECT $1::uuid,'PRIVACY_CASE',id::text,$2,'PENDING',$4,jsonb_build_object('createdAt',created_at)
		FROM candidates WHERE ($5<>'' OR org_id IS NULL OR ` + override + `)
		ORDER BY created_at,id LIMIT $3 ON CONFLICT DO NOTHING`
	case ObjectDeliveryEvent, ObjectProviderEvent:
		args = append(args, cutoff)
		providerFilter := ""
		if policy.ObjectType == ObjectProviderEvent {
			providerFilter = " AND de.provider_event_id IS NOT NULL"
		}
		query = `WITH candidates AS (
			SELECT de.id,de.received_at,c.organisation_id AS org_id
			FROM delivery_events de
			JOIN campaign_recipients cr ON cr.id=de.campaign_recipient_id
			JOIN campaigns c ON c.id=cr.campaign_id
			WHERE de.received_at<=$7` + providerFilter + ` AND ($5='' OR c.organisation_id::text=$5)
		)
		INSERT INTO retention_jobs(retention_policy_id,object_type,object_id,action,status,available_at,evidence)
		SELECT $1::uuid,$6,id::text,$2,'PENDING',$4,jsonb_build_object('receivedAt',received_at)
		FROM candidates WHERE ($5<>'' OR ` + override + `)
		ORDER BY received_at,id LIMIT $3 ON CONFLICT DO NOTHING`
	case ObjectAuditEvent:
		args = append(args, cutoff)
		query = `WITH candidates AS (
			SELECT ae.id,ae.created_at,ae.organisation_id AS org_id
			FROM audit_events ae
			WHERE ae.created_at<=$7 AND ($5='' OR ae.organisation_id::text=$5)
		)
		INSERT INTO retention_jobs(retention_policy_id,object_type,object_id,action,status,available_at,evidence)
		SELECT $1::uuid,'AUDIT_EVENT',id::text,$2,'PENDING',$4,jsonb_build_object('createdAt',created_at)
		FROM candidates WHERE ($5<>'' OR org_id IS NULL OR ` + override + `)
		ORDER BY created_at,id LIMIT $3 ON CONFLICT DO NOTHING`
	default:
		return 0, nil
	}
	res, err := p.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

const jobColumns = `id::text,retention_policy_id::text,object_type,object_id,coalesce(object_key,''),action,status,available_at,coalesce(lease_owner,''),lease_version,lease_expires_at,attempt_count,coalesce(last_error_code,''),coalesce(last_error_reference,''),evidence,created_at,updated_at,completed_at`

func scanJob(s scanner) (Job, error) {
	var v Job
	var lease, complete sql.NullTime
	var raw []byte
	err := s.Scan(&v.ID, &v.PolicyID, &v.ObjectType, &v.ObjectID, &v.ObjectKey, &v.Action, &v.Status, &v.AvailableAt, &v.LeaseOwner, &v.LeaseVersion, &lease, &v.AttemptCount, &v.LastErrorCode, &v.LastErrorReference, &raw, &v.CreatedAt, &v.UpdatedAt, &complete)
	if lease.Valid {
		x := lease.Time.UTC()
		v.LeaseExpiresAt = &x
	}
	if complete.Valid {
		x := complete.Time.UTC()
		v.CompletedAt = &x
	}
	if len(raw) > 0 {
		if e := json.Unmarshal(raw, &v.Evidence); e != nil {
			return Job{}, e
		}
	}
	return v, err
}
func (p *PostgreSQLStore) ClaimJobs(ctx context.Context, worker string, now time.Time, lease time.Duration, limit int) ([]Job, error) {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT `+jobColumns+` FROM retention_jobs WHERE (status IN ('PENDING','FAILED') OR (status='CLAIMED' AND lease_expires_at<$1)) AND available_at<=$1 ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	items := []Job{}
	for rows.Next() {
		v, e := scanJob(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		items = append(items, v)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	for i := range items {
		x := now.Add(lease)
		res, e := tx.ExecContext(ctx, `UPDATE retention_jobs SET status='CLAIMED',lease_owner=$2,lease_version=lease_version+1,lease_expires_at=$3,attempt_count=attempt_count+1,updated_at=$4 WHERE id=$1::uuid`, items[i].ID, worker, x, now)
		if e != nil {
			return nil, e
		}
		n, e := res.RowsAffected()
		if e != nil || n != 1 {
			return nil, ErrConflict
		}
		items[i].Status = JobClaimed
		items[i].LeaseOwner = worker
		items[i].LeaseVersion++
		items[i].LeaseExpiresAt = &x
		items[i].AttemptCount++
	}
	return items, tx.Commit()
}
func (p *PostgreSQLStore) finish(ctx context.Context, v Job, status JobStatus, evidence map[string]any, code, ref string, now time.Time, retry time.Duration) error {
	var raw any
	if evidence != nil {
		encoded, err := json.Marshal(evidence)
		if err != nil {
			return err
		}
		raw = string(encoded)
	}
	available := now
	if retry > 0 {
		available = now.Add(retry)
	}
	var complete any
	if status == JobCompleted {
		complete = now
	}
	res, err := p.DB.ExecContext(ctx, `UPDATE retention_jobs SET status=$2,lease_owner=NULL,lease_expires_at=NULL,last_error_code=NULLIF($3,''),last_error_reference=NULLIF($4,''),evidence=coalesce($5::jsonb,evidence),available_at=$6,completed_at=$7,updated_at=$8 WHERE id=$1::uuid AND status='CLAIMED' AND lease_owner=$9 AND lease_version=$10`, v.ID, status, code, ref, raw, available, complete, now, v.LeaseOwner, v.LeaseVersion)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}
func (p *PostgreSQLStore) CompleteJob(ctx context.Context, v Job, e map[string]any, now time.Time) error {
	return p.finish(ctx, v, JobCompleted, e, "", "", now, 0)
}
func (p *PostgreSQLStore) FailJob(ctx context.Context, v Job, code, ref string, now time.Time, retry time.Duration) error {
	return p.finish(ctx, v, JobFailed, v.Evidence, code, ref, now, retry)
}
func (p *PostgreSQLStore) HoldJob(ctx context.Context, v Job, reason string, e map[string]any, now time.Time) error {
	return p.finish(ctx, v, JobHeldReview, e, "REVIEW_REQUIRED", reason, now, 0)
}
func (p *PostgreSQLStore) ListJobs(ctx context.Context, status JobStatus, limit int) ([]Job, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+jobColumns+` FROM retention_jobs WHERE ($1='' OR status=$1) ORDER BY created_at DESC,id DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		v, e := scanJob(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type PostgreSQLExecutor struct {
	DB      *sql.DB
	Objects storage.ObjectStore
}

func (e *PostgreSQLExecutor) Execute(ctx context.Context, job Job, now time.Time) (map[string]any, error) {
	if e == nil || e.DB == nil {
		return nil, errors.New("retention executor database is required")
	}
	switch job.ObjectType {
	case ObjectInboundContent:
		if job.Action != ActionAnonymise && job.Action != ActionDelete {
			return nil, ReviewRequiredError{Reason: "inbound content action requires review", Evidence: job.Evidence}
		}
		res, err := e.DB.ExecContext(ctx, `UPDATE inbound_replies SET message_text='',message_text_cipher=NULL,content_redacted_at=$2,version=version+1 WHERE id=$1::uuid AND legal_hold=false AND content_redacted_at IS NULL`, job.ObjectID, now)
		if err != nil {
			return nil, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, ReviewRequiredError{Reason: "inbound content is held, absent, or already redacted", Evidence: job.Evidence}
		}
		return map[string]any{"redactedAt": now}, nil
	case ObjectAudienceImportSource:
		if job.Action != ActionDelete {
			return nil, ReviewRequiredError{Reason: "audience import source action requires review", Evidence: job.Evidence}
		}
		if e.Objects == nil {
			return nil, errors.New("object store is required")
		}
		alreadyAbsent, err := deleteRetentionObject(ctx, e.Objects, job.ObjectKey)
		if err != nil {
			return nil, err
		}
		res, err := e.DB.ExecContext(ctx, `UPDATE audience_imports SET source_deleted_at=$2,source_deletion_last_error=NULL,version=version+1,updated_at=$2 WHERE id=$1::uuid AND source_deleted_at IS NULL`, job.ObjectID, now)
		if err != nil {
			return nil, err
		}
		n, rowsErr := res.RowsAffected()
		if rowsErr != nil {
			return nil, rowsErr
		}
		if n == 0 {
			return nil, ReviewRequiredError{Reason: "audience import source is absent or already deleted", Evidence: job.Evidence}
		}
		return map[string]any{"deletedAt": now, "objectKey": job.ObjectKey, "objectAlreadyAbsent": alreadyAbsent}, nil
	case ObjectExportObject:
		if job.Action != ActionDelete {
			return nil, ReviewRequiredError{Reason: "export object action requires review", Evidence: job.Evidence}
		}
		if e.Objects == nil {
			return nil, errors.New("object store is required")
		}
		alreadyAbsent, err := deleteRetentionObject(ctx, e.Objects, job.ObjectKey)
		if err != nil {
			return nil, err
		}
		res, err := e.DB.ExecContext(ctx, `UPDATE export_requests SET status='EXPIRED',object_key=NULL,updated_at=$2,version=version+1 WHERE id=$1::uuid AND status IN ('READY','EXPIRING')`, job.ObjectID, now)
		if err != nil {
			return nil, err
		}
		n, rowsErr := res.RowsAffected()
		if rowsErr != nil {
			return nil, rowsErr
		}
		if n == 0 {
			return nil, ReviewRequiredError{Reason: "export is absent or no longer eligible", Evidence: job.Evidence}
		}
		return map[string]any{"deletedAt": now, "objectKey": job.ObjectKey, "objectAlreadyAbsent": alreadyAbsent}, nil
	default:
		return nil, ReviewRequiredError{Reason: fmt.Sprintf("%s requires governed review before %s", job.ObjectType, job.Action), Evidence: job.Evidence}
	}
}

func deleteRetentionObject(ctx context.Context, objects storage.ObjectStore, key string) (bool, error) {
	err := objects.Delete(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return true, nil
	}
	return false, err
}

func (p *PostgreSQLStore) ListJobPage(ctx context.Context, status JobStatus, limit int, before *time.Time, beforeID string) ([]Job, error) {
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	rows, err := p.DB.QueryContext(ctx, `SELECT `+jobColumns+` FROM retention_jobs
WHERE ($1='' OR status=$1)
  AND ($3::timestamptz IS NULL OR created_at<$3 OR (created_at=$3 AND id<NULLIF($4,'')::uuid))
ORDER BY created_at DESC,id DESC LIMIT $2`, status, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Job, 0, limit)
	for rows.Next() {
		value, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, value)
	}
	return items, rows.Err()
}
