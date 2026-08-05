package operations

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type PostgreSQLRepository struct{ DB *sql.DB }

func (r *PostgreSQLRepository) Dashboard(ctx context.Context, now time.Time) (Dashboard, error) {
	d := Dashboard{GeneratedAt: now, Campaigns: map[string]int64{}, Recipients: map[string]int64{}, Senders: map[string]int64{}}
	rows, err := r.DB.QueryContext(ctx, `SELECT status,count(*) FROM campaigns GROUP BY status`)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var k string
		var v int64
		if err = rows.Scan(&k, &v); err != nil {
			return d, err
		}
		d.Campaigns[k] = v
	}
	rows.Close()
	rows, err = r.DB.QueryContext(ctx, `SELECT status,count(*) FROM campaign_recipients GROUP BY status`)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var k string
		var v int64
		if err = rows.Scan(&k, &v); err != nil {
			return d, err
		}
		d.Recipients[k] = v
		if k == "UNKNOWN" {
			d.UnknownOutcomes = v
		}
		if k == "AUTHORISED" || k == "QUEUED" || k == "CLAIMED" || k == "SUBMITTING" || k == "FAILED_RETRYABLE" {
			d.QueueDepth += v
		}
	}
	rows.Close()
	rows, err = r.DB.QueryContext(ctx, `SELECT status,count(*) FROM sender_sessions GROUP BY status`)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var k string
		var v int64
		if err = rows.Scan(&k, &v); err != nil {
			return d, err
		}
		d.Senders[k] = v
	}
	rows.Close()
	_ = r.DB.QueryRowContext(ctx, `SELECT min(updated_at) FROM campaign_recipients WHERE status IN('AUTHORISED','QUEUED','CLAIMED','SUBMITTING','FAILED_RETRYABLE')`).Scan(&d.OldestQueuedAt)
	_ = r.DB.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE status<>'RESOLVED'),count(*) FILTER(WHERE status<>'RESOLVED' AND severity='CRITICAL') FROM operations_incidents`).Scan(&d.OpenIncidents, &d.CriticalIncidents)
	_ = r.DB.QueryRowContext(ctx, `SELECT count(*) FROM sender_nodes WHERE last_heartbeat_at IS NULL OR last_heartbeat_at < $1`, now.Add(-2*time.Minute)).Scan(&d.StaleWorkerNodes)
	return d, nil
}
func scanIncident(s interface{ Scan(...any) error }) (Incident, error) {
	var v Incident
	var resolved sql.NullTime
	err := s.Scan(&v.ID, &v.CampaignID, &v.SenderSessionID, &v.Category, &v.Severity, &v.Status, &v.Summary, &v.Detail, &v.OwnerID, &v.Resolution, &v.CreatedAt, &v.UpdatedAt, &resolved, &v.Version)
	if resolved.Valid {
		t := resolved.Time
		v.ResolvedAt = &t
	}
	return v, err
}

const incidentSelect = `SELECT id::text,coalesce(campaign_id::text,''),coalesce(sender_session_id::text,''),category,severity,status,summary,coalesce(detail,''),coalesce(owner_id::text,''),coalesce(resolution,''),created_at,updated_at,resolved_at,version FROM operations_incidents`

func (r *PostgreSQLRepository) ListIncidents(ctx context.Context, status IncidentStatus, limit int) ([]Incident, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := incidentSelect + ` WHERE ($1='' OR status=$1) ORDER BY CASE severity WHEN 'CRITICAL' THEN 1 WHEN 'WARNING' THEN 2 ELSE 3 END,created_at DESC LIMIT $2`
	rows, err := r.DB.QueryContext(ctx, q, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Incident{}
	for rows.Next() {
		v, e := scanIncident(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgreSQLRepository) GetIncident(ctx context.Context, id string) (Incident, error) {
	v, err := scanIncident(r.DB.QueryRowContext(ctx, incidentSelect+` WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Incident{}, ErrNotFound
	}
	return v, err
}
func (r *PostgreSQLRepository) CreateIncident(ctx context.Context, v Incident) (Incident, error) {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO operations_incidents(id,campaign_id,sender_session_id,category,severity,status,summary,detail,owner_id,resolution,created_at,updated_at,resolved_at,version) VALUES($1::uuid,NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,'')::uuid,NULLIF($10,''),$11,$12,$13,$14)`, v.ID, v.CampaignID, v.SenderSessionID, v.Category, v.Severity, v.Status, v.Summary, v.Detail, v.OwnerID, v.Resolution, v.CreatedAt, v.UpdatedAt, v.ResolvedAt, v.Version)
	return v, err
}
func (r *PostgreSQLRepository) UpdateIncident(ctx context.Context, v Incident, expected int64) (Incident, error) {
	res, err := r.DB.ExecContext(ctx, `UPDATE operations_incidents SET status=$2,owner_id=NULLIF($3,'')::uuid,resolution=NULLIF($4,''),resolved_at=$5,updated_at=$6,version=version+1 WHERE id=$1::uuid AND version=$7`, v.ID, v.Status, v.OwnerID, v.Resolution, v.ResolvedAt, v.UpdatedAt, expected)
	if err != nil {
		return Incident{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Incident{}, ErrConflict
	}
	v.Version = expected + 1
	return v, nil
}
func (r *PostgreSQLRepository) CampaignReport(ctx context.Context, id string, now time.Time) (CampaignReport, error) {
	var v CampaignReport
	var started, completed sql.NullTime
	err := r.DB.QueryRowContext(ctx, `SELECT c.id::text,c.organisation_id::text,c.name,cp.name,c.status,c.execution_started_at,c.execution_completed_at,coalesce(cm.authorised_total,0),coalesce(cm.queued_total,0),coalesce(cm.submitted_total,0),coalesce(cm.sent_total,0),coalesce(cm.delivered_total,0),coalesce(cm.read_total,0),coalesce(cm.failed_total,0),coalesce(cm.unknown_total,0),coalesce(cm.suppressed_total,0),coalesce(cm.opt_out_total,0) FROM campaigns c JOIN consent_purposes cp ON cp.id=c.purpose_id LEFT JOIN campaign_metrics cm ON cm.campaign_id=c.id WHERE c.id=$1::uuid`, id).Scan(&v.CampaignID, &v.OrganisationID, &v.Name, &v.Purpose, &v.Status, &started, &completed, new(int64), new(int64), new(int64), new(int64), new(int64), new(int64), new(int64), new(int64), new(int64), new(int64))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	// query metrics separately for clear mapping
	var a, q, sub, sent, del, read, fail, unk, supp, opt int64
	_ = r.DB.QueryRowContext(ctx, `SELECT authorised_total,queued_total,submitted_total,sent_total,delivered_total,read_total,failed_total,unknown_total,suppressed_total,opt_out_total FROM campaign_metrics WHERE campaign_id=$1::uuid`, id).Scan(&a, &q, &sub, &sent, &del, &read, &fail, &unk, &supp, &opt)
	v.Audience = map[string]int64{"authorised": a, "suppressed": supp}
	v.Delivery = map[string]int64{"queued": q, "submitted": sub, "sent": sent, "delivered": del, "read": read}
	v.Engagement = map[string]int64{"optOuts": opt}
	v.Exceptions = map[string]int64{"failed": fail, "unknown": unk}
	v.GeneratedAt = now
	if started.Valid {
		t := started.Time
		v.StartedAt = &t
	}
	if completed.Valid {
		t := completed.Time
		v.CompletedAt = &t
	}
	return v, nil
}
func scanExport(s interface{ Scan(...any) error }) (ExportRequest, error) {
	var v ExportRequest
	var exp sql.NullTime
	var generated, leaseExp sql.NullTime
	err := s.Scan(&v.ID, &v.Kind, &v.ObjectID, &v.Format, &v.Status, &v.RequestedBy, &v.ApprovedBy, &v.Reason, &v.RejectionReason, &v.CreatedAt, &v.UpdatedAt, &exp, &v.ObjectKey, &v.ContentType, &v.SHA256, &v.SizeBytes, &v.FailureCode, &v.FailureDetail, &generated, &v.LeaseOwner, &leaseExp, &v.Version)
	if exp.Valid {
		t := exp.Time
		v.ExpiresAt = &t
	}
	if generated.Valid {
		t := generated.Time
		v.GeneratedAt = &t
	}
	if leaseExp.Valid {
		t := leaseExp.Time
		v.LeaseExpiresAt = &t
	}
	return v, err
}

const exportSelect = `SELECT id::text,kind,coalesce(object_id::text,''),format,status,requested_by::text,coalesce(approved_by::text,''),reason,coalesce(rejection_reason,''),created_at,updated_at,expires_at,coalesce(object_key,''),coalesce(content_type,''),coalesce(sha256,''),coalesce(size_bytes,0),coalesce(failure_code,''),coalesce(failure_detail,''),generated_at,coalesce(lease_owner,''),lease_expires_at,version FROM export_requests`

func (r *PostgreSQLRepository) CreateExport(ctx context.Context, v ExportRequest) (ExportRequest, error) {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO export_requests(id,kind,object_id,format,status,requested_by,reason,created_at,updated_at,version) VALUES($1::uuid,$2,NULLIF($3,'')::uuid,$4,$5,$6::uuid,$7,$8,$9,$10)`, v.ID, v.Kind, v.ObjectID, v.Format, v.Status, v.RequestedBy, v.Reason, v.CreatedAt, v.UpdatedAt, v.Version)
	return v, err
}
func (r *PostgreSQLRepository) GetExport(ctx context.Context, id string) (ExportRequest, error) {
	v, err := scanExport(r.DB.QueryRowContext(ctx, exportSelect+` WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}
func (r *PostgreSQLRepository) UpdateExport(ctx context.Context, v ExportRequest, expected int64) (ExportRequest, error) {
	res, err := r.DB.ExecContext(ctx, `UPDATE export_requests SET status=$2,approved_by=NULLIF($3,'')::uuid,rejection_reason=NULLIF($4,''),expires_at=$5,updated_at=$6,version=version+1 WHERE id=$1::uuid AND version=$7`, v.ID, v.Status, v.ApprovedBy, v.RejectionReason, v.ExpiresAt, v.UpdatedAt, expected)
	if err != nil {
		return v, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return v, ErrConflict
	}
	v.Version = expected + 1
	return v, nil
}

func (r *PostgreSQLRepository) ListExceptions(ctx context.Context, campaignID string, limit int) ([]DeliveryException, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,campaign_id::text,status,coalesce(assigned_session_id::text,''),coalesce(provider_message_id,''),attempt_count,coalesce(last_error_code,''),updated_at FROM campaign_recipients WHERE ($1='' OR campaign_id=$1::uuid) AND (status IN('FAILED_RETRYABLE','FAILED_PERMANENT','UNKNOWN') OR reconciliation_required=true) ORDER BY updated_at DESC LIMIT $2`, campaignID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeliveryException{}
	for rows.Next() {
		var v DeliveryException
		if err = rows.Scan(&v.RecipientID, &v.CampaignID, &v.Status, &v.AssignedSessionID, &v.ProviderMessageID, &v.AttemptCount, &v.ErrorCode, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *PostgreSQLRepository) ClaimExport(ctx context.Context, worker string, now time.Time, lease time.Duration) (ExportRequest, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return ExportRequest{}, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, exportSelect+` WHERE status='APPROVED' OR (status='PROCESSING' AND lease_expires_at < $1) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`, now)
	v, err := scanExport(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ExportRequest{}, ErrNotFound
	}
	if err != nil {
		return ExportRequest{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE export_requests SET status='PROCESSING',lease_owner=$2,lease_expires_at=$3,attempt_count=attempt_count+1,updated_at=$4,version=version+1 WHERE id=$1::uuid`, v.ID, worker, now.Add(lease), now)
	if err != nil {
		return ExportRequest{}, err
	}
	if err = tx.Commit(); err != nil {
		return ExportRequest{}, err
	}
	v.Status = ExportProcessing
	v.LeaseOwner = worker
	t := now.Add(lease)
	v.LeaseExpiresAt = &t
	v.Version++
	return v, nil
}

func (r *PostgreSQLRepository) CompleteExport(ctx context.Context, id, key, contentType, checksum string, size int64, generated, expires time.Time) error {
	res, err := r.DB.ExecContext(ctx, `UPDATE export_requests SET status='READY',object_key=$2,content_type=$3,sha256=$4,size_bytes=$5,generated_at=$6,expires_at=$7,lease_owner=NULL,lease_expires_at=NULL,failure_code=NULL,failure_detail=NULL,updated_at=$6,version=version+1 WHERE id=$1::uuid AND status='PROCESSING'`, id, key, contentType, checksum, size, generated, expires)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}
func (r *PostgreSQLRepository) FailExport(ctx context.Context, id, code, detail string, now time.Time) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE export_requests SET status='FAILED',failure_code=$2,failure_detail=$3,lease_owner=NULL,lease_expires_at=NULL,updated_at=$4,version=version+1 WHERE id=$1::uuid AND status='PROCESSING'`, id, code, detail, now)
	return err
}
func (r *PostgreSQLRepository) ExpireExports(ctx context.Context, now time.Time, limit int) ([]ExportRequest, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, exportSelect+` WHERE status='READY' AND expires_at <= $1 ORDER BY expires_at FOR UPDATE SKIP LOCKED LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExportRequest
	for rows.Next() {
		v, e := scanExport(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, v := range out {
		if _, err = tx.ExecContext(ctx, `UPDATE export_requests SET status='EXPIRED',updated_at=$2,version=version+1 WHERE id=$1::uuid`, v.ID, now); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
