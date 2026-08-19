package operations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/audit"
)

type PostgreSQLRepository struct {
	DB                        *sql.DB
	RuntimeHealth             GatewayRuntimeHealthPolicyResolver
	FallbackGatewayStaleAfter time.Duration
}

func (r *PostgreSQLRepository) gatewayStaleBefore(ctx context.Context, now time.Time) (time.Time, GatewayRuntimeHealthPolicy, error) {
	fallback := r.FallbackGatewayStaleAfter
	if fallback <= 0 {
		fallback = 2 * time.Minute
	}
	policy := GatewayRuntimeHealthPolicy{StaleAfter: fallback, Source: "DEPLOYMENT_BOOTSTRAP"}
	if r.RuntimeHealth != nil {
		resolved, err := r.RuntimeHealth.ResolveGatewayRuntimeHealth(ctx, now.UTC())
		if err != nil {
			return time.Time{}, GatewayRuntimeHealthPolicy{}, err
		}
		policy = resolved
	}
	if policy.StaleAfter <= 0 {
		return time.Time{}, GatewayRuntimeHealthPolicy{}, errors.New("gateway stale threshold must be positive")
	}
	return now.UTC().Add(-policy.StaleAfter), policy, nil
}

const capacityShortfallQuery = `WITH reserved AS (
  SELECT sender_pool_id,sum(reserved_daily_units) AS reserved
  FROM campaign_pool_capacity_reservations
  WHERE status IN ('HELD','ACTIVE') AND reservation_start<=$1 AND reservation_end>$1
  GROUP BY sender_pool_id
) SELECT count(*) FROM reserved r JOIN sender_pools p ON p.id=r.sender_pool_id WHERE r.reserved>p.daily_capacity`

func capacityShortfallStatement(now time.Time) (string, []any) {
	return capacityShortfallQuery, []any{now.UTC()}
}

func (r *PostgreSQLRepository) Dashboard(ctx context.Context, now time.Time) (Dashboard, error) {
	if r == nil || r.DB == nil {
		return Dashboard{}, errors.New("database is required")
	}
	d := Dashboard{GeneratedAt: now.UTC(), Campaigns: map[string]int64{}, Recipients: map[string]int64{}, Senders: map[string]int64{}}
	staleBefore, runtimeHealthPolicy, err := r.gatewayStaleBefore(ctx, now)
	if err != nil {
		return d, err
	}
	d.GatewayStaleAfterSeconds = int64(runtimeHealthPolicy.StaleAfter / time.Second)
	d.GatewayRuntimeHealthSource = runtimeHealthPolicy.Source
	d.GatewayRuntimeHealthConfigurationID = runtimeHealthPolicy.ConfigurationID
	d.GatewayRuntimeHealthConfigurationVersion = runtimeHealthPolicy.Version
	if err := loadStatusCounts(ctx, r.DB, `SELECT status,count(*) FROM campaigns GROUP BY status`, d.Campaigns); err != nil {
		return d, err
	}

	var authorised, queued, submitted, sent, delivered, read, failed, unknown, suppressed int64
	if err := r.DB.QueryRowContext(ctx, `SELECT
 coalesce(sum(authorised_total),0),coalesce(sum(queued_total),0),coalesce(sum(submitted_total),0),
 coalesce(sum(sent_total),0),coalesce(sum(delivered_total),0),coalesce(sum(read_total),0),
 coalesce(sum(failed_total),0),coalesce(sum(unknown_total),0),coalesce(sum(suppressed_total),0)
FROM campaign_metrics`).Scan(&authorised, &queued, &submitted, &sent, &delivered, &read, &failed, &unknown, &suppressed); err != nil {
		return d, err
	}
	d.Recipients = map[string]int64{
		"AUTHORISED": authorised,
		"QUEUED":     queued,
		"SUBMITTED":  submitted,
		"SENT":       sent,
		"DELIVERED":  delivered,
		"READ":       read,
		"FAILED":     failed,
		"UNKNOWN":    unknown,
		"SUPPRESSED": suppressed,
	}
	d.UnknownOutcomes = unknown

	var submitting, retryable int64
	if err := r.DB.QueryRowContext(ctx, `SELECT
 count(*) FILTER (WHERE status='SUBMITTING'),
 count(*) FILTER (WHERE status='FAILED_RETRYABLE')
FROM campaign_recipients
WHERE status IN('SUBMITTING','FAILED_RETRYABLE')`).Scan(&submitting, &retryable); err != nil {
		return d, err
	}
	// Queue depth represents work that has not yet received a provider acceptance.
	// GATEWAY_ACCEPTED is reported in submitted totals but is no longer queued work.
	d.QueueDepth = authorised + queued + submitting + retryable

	if err := loadStatusCounts(ctx, r.DB, `SELECT status,count(*) FROM sender_sessions GROUP BY status`, d.Senders); err != nil {
		return d, err
	}
	var oldest sql.NullTime
	if err := r.DB.QueryRowContext(ctx, `SELECT min(updated_at) FROM campaign_recipients WHERE status IN('AUTHORISED','QUEUED','CLAIMED','SUBMITTING','FAILED_RETRYABLE')`).Scan(&oldest); err != nil {
		return d, err
	}
	if oldest.Valid {
		value := oldest.Time.UTC()
		d.OldestQueuedAt = &value
	}
	if err := r.DB.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE status NOT IN ('RESOLVED','CLOSED')),count(*) FILTER(WHERE status NOT IN ('RESOLVED','CLOSED') AND severity='CRITICAL') FROM operations_incidents`).Scan(&d.OpenIncidents, &d.CriticalIncidents); err != nil {
		return d, err
	}

	if err := r.DB.QueryRowContext(ctx, `SELECT count(*) FROM sender_nodes WHERE last_heartbeat_at IS NULL OR last_heartbeat_at < $1`, staleBefore).Scan(&d.StaleWorkerNodes); err != nil {
		return d, err
	}
	if err := r.DB.QueryRowContext(ctx, `SELECT count(*) FROM sender_nodes WHERE status IN ('UNHEALTHY','OFFLINE') OR last_heartbeat_at IS NULL OR last_heartbeat_at < $1`, staleBefore).Scan(&d.UnavailableGatewayNodes); err != nil {
		return d, err
	}
	if err := r.DB.QueryRowContext(ctx, `SELECT count(*) FROM sender_sessions WHERE status IN ('DISCONNECTED','RESTRICTED','QUARANTINED','RECOVERY_FAILED')`).Scan(&d.UnhealthySenderSessions); err != nil {
		return d, err
	}
	if err := r.DB.QueryRowContext(ctx, `WITH latest AS (
  SELECT DISTINCT ON (campaign_id) campaign_id,decision,forecast_completion_at,deadline_at
  FROM campaign_capacity_assessments ORDER BY campaign_id,evaluated_at DESC
) SELECT count(*) FROM latest l JOIN campaigns c ON c.id=l.campaign_id
WHERE c.status IN ('SCHEDULED','DISPATCHING','PAUSED') AND (l.decision IN ('HOLD','REJECT') OR l.forecast_completion_at>l.deadline_at)`).Scan(&d.CampaignsAtRisk); err != nil {
		return d, err
	}
	capacityQuery, capacityArgs := capacityShortfallStatement(now)
	if err := r.DB.QueryRowContext(ctx, capacityQuery, capacityArgs...).Scan(&d.CapacityShortfallPools); err != nil {
		return d, err
	}
	if err := r.DB.QueryRowContext(ctx, `SELECT
  (SELECT count(*) FROM campaign_recipients WHERE reconciliation_required=true) +
  (SELECT count(*) FROM campaign_metric_reconciliations WHERE consecutive_drift_count>0)`).Scan(&d.ReconciliationBacklog); err != nil {
		return d, err
	}
	return d, nil
}

func loadStatusCounts(ctx context.Context, db *sql.DB, query string, target map[string]int64) error {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return err
		}
		target[status] = count
	}
	return rows.Err()
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
	n, err := res.RowsAffected()
	if err != nil {
		return Incident{}, err
	}
	if n == 0 {
		return Incident{}, ErrConflict
	}
	v.Version = expected + 1
	return v, nil
}

func (r *PostgreSQLRepository) CreateIncidentWithEvents(ctx context.Context, v Incident, events []IncidentEvent) (Incident, error) {
	return r.createIncidentWithEvents(ctx, v, events, nil)
}

func (r *PostgreSQLRepository) CreateIncidentWithEventsAndAudit(ctx context.Context, v Incident, events []IncidentEvent, input audit.Input) (Incident, error) {
	return r.createIncidentWithEvents(ctx, v, events, &input)
}

func (r *PostgreSQLRepository) createIncidentWithEvents(ctx context.Context, v Incident, events []IncidentEvent, auditInput *audit.Input) (Incident, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return Incident{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO operations_incidents(id,campaign_id,sender_session_id,category,severity,status,summary,detail,owner_id,resolution,created_at,updated_at,resolved_at,version) VALUES($1::uuid,NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,'')::uuid,NULLIF($10,''),$11,$12,$13,$14)`, v.ID, v.CampaignID, v.SenderSessionID, v.Category, v.Severity, v.Status, v.Summary, v.Detail, v.OwnerID, v.Resolution, v.CreatedAt, v.UpdatedAt, v.ResolvedAt, v.Version)
	if err != nil {
		return Incident{}, err
	}
	for _, event := range events {
		if err = insertIncidentEvent(ctx, tx, event); err != nil {
			return Incident{}, err
		}
	}
	if auditInput != nil {
		if _, err = audit.EnqueueTx(ctx, tx, *auditInput); err != nil {
			return Incident{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Incident{}, err
	}
	return v, nil
}

func (r *PostgreSQLRepository) UpdateIncidentWithEvents(ctx context.Context, v Incident, expected int64, events []IncidentEvent) (Incident, error) {
	return r.updateIncidentWithEvents(ctx, v, expected, events, nil)
}

func (r *PostgreSQLRepository) UpdateIncidentWithEventsAndAudit(ctx context.Context, v Incident, expected int64, events []IncidentEvent, input audit.Input) (Incident, error) {
	return r.updateIncidentWithEvents(ctx, v, expected, events, &input)
}

func (r *PostgreSQLRepository) updateIncidentWithEvents(ctx context.Context, v Incident, expected int64, events []IncidentEvent, auditInput *audit.Input) (Incident, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return Incident{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE operations_incidents SET status=$2,owner_id=NULLIF($3,'')::uuid,resolution=NULLIF($4,''),resolved_at=$5,updated_at=$6,version=version+1 WHERE id=$1::uuid AND version=$7`, v.ID, v.Status, v.OwnerID, v.Resolution, v.ResolvedAt, v.UpdatedAt, expected)
	if err != nil {
		return Incident{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Incident{}, err
	}
	if n != 1 {
		return Incident{}, ErrConflict
	}
	v.Version = expected + 1
	for _, event := range events {
		if event.Evidence == nil {
			event.Evidence = map[string]any{}
		}
		event.Evidence["version"] = v.Version
		if err = insertIncidentEvent(ctx, tx, event); err != nil {
			return Incident{}, err
		}
	}
	if auditInput != nil {
		if _, err = audit.EnqueueTx(ctx, tx, *auditInput); err != nil {
			return Incident{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Incident{}, err
	}
	return v, nil
}

func (r *PostgreSQLRepository) CampaignReport(ctx context.Context, id string, now time.Time) (CampaignReport, error) {
	var v CampaignReport
	var started, completed sql.NullTime
	err := r.DB.QueryRowContext(ctx, `SELECT c.id::text,c.organisation_id::text,c.name,cp.name,c.status,c.execution_started_at,c.execution_completed_at FROM campaigns c JOIN consent_purposes cp ON cp.id=c.purpose_id WHERE c.id=$1::uuid`, id).Scan(&v.CampaignID, &v.OrganisationID, &v.Name, &v.Purpose, &v.Status, &started, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	var a, q, sub, sent, del, read, fail, unk, supp, opt int64
	metricsErr := r.DB.QueryRowContext(ctx, `SELECT authorised_total,queued_total,submitted_total,sent_total,delivered_total,read_total,failed_total,unknown_total,suppressed_total,opt_out_total FROM campaign_metrics WHERE campaign_id=$1::uuid`, id).Scan(&a, &q, &sub, &sent, &del, &read, &fail, &unk, &supp, &opt)
	if metricsErr != nil && !errors.Is(metricsErr, sql.ErrNoRows) {
		return v, metricsErr
	}
	v.Audience = map[string]int64{"authorised": a, "suppressed": supp}
	v.Delivery = map[string]int64{"queued": q, "submitted": sub, "sent": sent, "delivered": del, "read": read}
	v.Engagement = map[string]int64{"optOuts": opt}
	v.Exceptions = map[string]int64{"failed": fail, "unknown": unk}
	v.Pools = []CampaignPoolReport{}
	v.Warnings = []string{}
	if errors.Is(metricsErr, sql.ErrNoRows) {
		v.Warnings = append(v.Warnings, "CAMPAIGN_METRICS_UNAVAILABLE")
	}
	var paymentAt sql.NullTime
	commercialErr := r.DB.QueryRowContext(ctx, `SELECT status,quotation_reference,invoice_reference,currency,approved_recipients,unit_price_minor,management_fee_minor,total_amount_minor,coalesce(payment_reference,''),payment_received_at FROM campaign_commercial_approvals WHERE campaign_id=$1::uuid`, id).Scan(&v.Commercial.Status, &v.Commercial.QuotationReference, &v.Commercial.InvoiceReference, &v.Commercial.Currency, &v.Commercial.ApprovedRecipients, &v.Commercial.UnitPriceMinor, &v.Commercial.ManagementFeeMinor, &v.Commercial.TotalAmountMinor, &v.Commercial.PaymentReference, &paymentAt)
	if commercialErr != nil && !errors.Is(commercialErr, sql.ErrNoRows) {
		return v, commercialErr
	}
	if paymentAt.Valid {
		t := paymentAt.Time.UTC()
		v.Commercial.PaymentReceivedAt = &t
	}
	rows, err := r.DB.QueryContext(ctx, `
WITH latest_plan AS (
 SELECT id FROM campaign_routing_plans WHERE campaign_id=$1::uuid ORDER BY plan_version DESC,approved_at DESC LIMIT 1
)
SELECT p.sender_pool_id::text,sp.name,p.gateway_pool_id::text,p.provider,p.engine,p.maximum_recipients,p.reserved_messages_per_minute,p.reserved_hourly_units,p.reserved_daily_units,
 count(cr.id) FILTER (WHERE cr.id IS NOT NULL),
 count(cr.id) FILTER (WHERE cr.status IN('AUTHORISED','QUEUED','CLAIMED')),
 count(cr.id) FILTER (WHERE cr.status IN('SUBMITTING','GATEWAY_ACCEPTED')),
 count(cr.id) FILTER (WHERE cr.status='SENT'),
 count(cr.id) FILTER (WHERE cr.status='DELIVERED'),
 count(cr.id) FILTER (WHERE cr.status='READ'),
 count(cr.id) FILTER (WHERE cr.status IN('FAILED_RETRYABLE','FAILED_PERMANENT')),
 count(cr.id) FILTER (WHERE cr.status='UNKNOWN')
FROM latest_plan lp
JOIN campaign_routing_plan_pools p ON p.routing_plan_id=lp.id
JOIN sender_pools sp ON sp.id=p.sender_pool_id
LEFT JOIN sender_sessions ss ON ss.sender_pool_id=p.sender_pool_id
LEFT JOIN campaign_recipients cr ON cr.campaign_id=$1::uuid AND cr.assigned_session_id=ss.id
GROUP BY p.sender_pool_id,sp.name,p.gateway_pool_id,p.provider,p.engine,p.maximum_recipients,p.reserved_messages_per_minute,p.reserved_hourly_units,p.reserved_daily_units
ORDER BY sp.name,p.sender_pool_id`, id)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var pool CampaignPoolReport
		var total, queued, submitted, psent, pdelivered, pread, pfailed, punknown int64
		if err := rows.Scan(&pool.SenderPoolID, &pool.SenderPoolName, &pool.GatewayPoolID, &pool.Provider, &pool.Engine, &pool.MaximumRecipients, &pool.ReservedMessagesPerMinute, &pool.ReservedHourlyUnits, &pool.ReservedDailyUnits, &total, &queued, &submitted, &psent, &pdelivered, &pread, &pfailed, &punknown); err != nil {
			return v, err
		}
		pool.Recipients = map[string]int64{"total": total, "queued": queued, "submittedOrAccepted": submitted, "sent": psent, "delivered": pdelivered, "read": pread, "failed": pfailed, "unknown": punknown}
		v.Pools = append(v.Pools, pool)
	}
	if err := rows.Err(); err != nil {
		return v, err
	}
	if v.Commercial.Status == "" {
		v.Warnings = append(v.Warnings, "COMMERCIAL_EVIDENCE_UNAVAILABLE")
	}
	if len(v.Pools) == 0 {
		v.Warnings = append(v.Warnings, "ROUTING_PLAN_UNAVAILABLE")
	}
	if unk > 0 {
		v.Warnings = append(v.Warnings, "UNKNOWN_OUTCOMES_REQUIRE_RECONCILIATION")
	}
	v.RawBreakdowns, err = r.campaignBreakdowns(ctx, id)
	if err != nil {
		return v, err
	}
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

func (r *PostgreSQLRepository) OrganisationPerformanceReport(ctx context.Context, id string, now time.Time) (OrganisationPerformanceReport, error) {
	var v OrganisationPerformanceReport
	err := r.DB.QueryRowContext(ctx, `SELECT id::text,legal_name FROM organisations WHERE id=$1::uuid`, id).Scan(&v.OrganisationID, &v.OrganisationName)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	v.Campaigns = map[string]int64{}
	v.Recipients = map[string]int64{}
	v.Delivery = map[string]int64{}
	v.Commercial = []CurrencyCommercialSummary{}
	v.Warnings = []string{}
	rows, err := r.DB.QueryContext(ctx, `SELECT status,count(*) FROM campaigns WHERE organisation_id=$1::uuid GROUP BY status`, id)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			rows.Close()
			return v, err
		}
		v.Campaigns[status] = count
	}
	if err := rows.Close(); err != nil {
		return v, err
	}
	var authorised, queued, submitted, sent, delivered, read, failed, unknown, suppressed, optOut int64
	err = r.DB.QueryRowContext(ctx, `SELECT coalesce(sum(m.authorised_total),0),coalesce(sum(m.queued_total),0),coalesce(sum(m.submitted_total),0),coalesce(sum(m.sent_total),0),coalesce(sum(m.delivered_total),0),coalesce(sum(m.read_total),0),coalesce(sum(m.failed_total),0),coalesce(sum(m.unknown_total),0),coalesce(sum(m.suppressed_total),0),coalesce(sum(m.opt_out_total),0) FROM campaigns c LEFT JOIN campaign_metrics m ON m.campaign_id=c.id WHERE c.organisation_id=$1::uuid`, id).Scan(&authorised, &queued, &submitted, &sent, &delivered, &read, &failed, &unknown, &suppressed, &optOut)
	if err != nil {
		return v, err
	}
	v.Recipients = map[string]int64{"authorised": authorised, "suppressed": suppressed, "optOuts": optOut}
	v.Delivery = map[string]int64{"queued": queued, "submitted": submitted, "sent": sent, "delivered": delivered, "read": read, "failed": failed, "unknown": unknown}
	commercialRows, err := r.DB.QueryContext(ctx, `SELECT currency,count(*),coalesce(sum(approved_recipients),0),coalesce(sum(total_amount_minor),0) FROM campaign_commercial_approvals WHERE organisation_id=$1::uuid AND status='APPROVED' GROUP BY currency ORDER BY currency`, id)
	if err != nil {
		return v, err
	}
	for commercialRows.Next() {
		var c CurrencyCommercialSummary
		if err := commercialRows.Scan(&c.Currency, &c.Campaigns, &c.ApprovedRecipients, &c.ApprovedAmountMinor); err != nil {
			commercialRows.Close()
			return v, err
		}
		v.Commercial = append(v.Commercial, c)
	}
	if err := commercialRows.Close(); err != nil {
		return v, err
	}
	if unknown > 0 {
		v.Warnings = append(v.Warnings, "UNKNOWN_OUTCOMES_REQUIRE_RECONCILIATION")
	}
	if len(v.Commercial) == 0 && len(v.Campaigns) > 0 {
		v.Warnings = append(v.Warnings, "NO_APPROVED_COMMERCIAL_RECORDS")
	}
	v.RawBreakdowns, err = r.organisationBreakdowns(ctx, id)
	if err != nil {
		return v, err
	}
	v.GeneratedAt = now
	return v, nil
}

func scanBreakdowns(rows *sql.Rows) (map[string]map[string]int64, error) {
	defer rows.Close()
	out := map[string]map[string]int64{}
	for rows.Next() {
		var dimension, label string
		var count int64
		if err := rows.Scan(&dimension, &label, &count); err != nil {
			return nil, err
		}
		if out[dimension] == nil {
			out[dimension] = map[string]int64{}
		}
		out[dimension][label] = count
	}
	return out, rows.Err()
}

const campaignBreakdownSQL = `
WITH population AS (
 SELECT c.state_id,c.lga_id,c.gender_code,c.reported_age
 FROM campaign_recipients cr
 JOIN contacts c ON c.id=cr.contact_id
 WHERE cr.campaign_id=$1::uuid
)
SELECT 'state' AS dimension,coalesce(a.name,'UNKNOWN') AS label,count(*)::bigint
FROM population p LEFT JOIN administrative_areas a ON a.id=p.state_id
GROUP BY coalesce(a.name,'UNKNOWN')
UNION ALL
SELECT 'lga',coalesce(a.name,'UNKNOWN'),count(*)::bigint
FROM population p LEFT JOIN administrative_areas a ON a.id=p.lga_id
GROUP BY coalesce(a.name,'UNKNOWN')
UNION ALL
SELECT 'gender',coalesce(g.display_name,'UNKNOWN'),count(*)::bigint
FROM population p LEFT JOIN gender_options g ON g.code=p.gender_code
GROUP BY coalesce(g.display_name,'UNKNOWN')
UNION ALL
SELECT 'ageBand',CASE
 WHEN reported_age IS NULL THEN 'UNKNOWN'
 WHEN reported_age < 18 THEN 'UNDER_18'
 WHEN reported_age <= 24 THEN '18-24'
 WHEN reported_age <= 34 THEN '25-34'
 WHEN reported_age <= 44 THEN '35-44'
 WHEN reported_age <= 54 THEN '45-54'
 WHEN reported_age <= 64 THEN '55-64'
 ELSE '65_PLUS' END,count(*)::bigint
FROM population
GROUP BY 2
ORDER BY 1,2`

func (r *PostgreSQLRepository) campaignBreakdowns(ctx context.Context, campaignID string) (map[string]map[string]int64, error) {
	rows, err := r.DB.QueryContext(ctx, campaignBreakdownSQL, campaignID)
	if err != nil {
		return nil, err
	}
	return scanBreakdowns(rows)
}

const organisationBreakdownSQL = `
WITH population AS (
 SELECT ct.state_id,ct.lga_id,ct.gender_code,ct.reported_age
 FROM campaign_recipients cr
 JOIN campaigns c ON c.id=cr.campaign_id
 JOIN contacts ct ON ct.id=cr.contact_id
 WHERE c.organisation_id=$1::uuid
)
SELECT 'state' AS dimension,coalesce(a.name,'UNKNOWN') AS label,count(*)::bigint
FROM population p LEFT JOIN administrative_areas a ON a.id=p.state_id
GROUP BY coalesce(a.name,'UNKNOWN')
UNION ALL
SELECT 'lga',coalesce(a.name,'UNKNOWN'),count(*)::bigint
FROM population p LEFT JOIN administrative_areas a ON a.id=p.lga_id
GROUP BY coalesce(a.name,'UNKNOWN')
UNION ALL
SELECT 'gender',coalesce(g.display_name,'UNKNOWN'),count(*)::bigint
FROM population p LEFT JOIN gender_options g ON g.code=p.gender_code
GROUP BY coalesce(g.display_name,'UNKNOWN')
UNION ALL
SELECT 'ageBand',CASE
 WHEN reported_age IS NULL THEN 'UNKNOWN'
 WHEN reported_age < 18 THEN 'UNDER_18'
 WHEN reported_age <= 24 THEN '18-24'
 WHEN reported_age <= 34 THEN '25-34'
 WHEN reported_age <= 44 THEN '35-44'
 WHEN reported_age <= 54 THEN '45-54'
 WHEN reported_age <= 64 THEN '55-64'
 ELSE '65_PLUS' END,count(*)::bigint
FROM population
GROUP BY 2
ORDER BY 1,2`

func (r *PostgreSQLRepository) organisationBreakdowns(ctx context.Context, organisationID string) (map[string]map[string]int64, error) {
	rows, err := r.DB.QueryContext(ctx, organisationBreakdownSQL, organisationID)
	if err != nil {
		return nil, err
	}
	return scanBreakdowns(rows)
}

func (r *PostgreSQLRepository) CampaignFinancialReconciliation(ctx context.Context, id string, now time.Time) (CampaignFinancialReconciliation, error) {
	var v CampaignFinancialReconciliation
	err := r.DB.QueryRowContext(ctx, `SELECT id::text,organisation_id::text,name,status FROM campaigns WHERE id=$1::uuid`, id).Scan(&v.CampaignID, &v.OrganisationID, &v.CampaignName, &v.CampaignStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	v.Warnings = []string{}
	var paymentAt sql.NullTime
	commercialErr := r.DB.QueryRowContext(ctx, `SELECT status,quotation_reference,invoice_reference,currency,approved_recipients,total_amount_minor,coalesce(payment_reference,''),payment_received_at FROM campaign_commercial_approvals WHERE campaign_id=$1::uuid`, id).Scan(&v.CommercialStatus, &v.QuotationReference, &v.InvoiceReference, &v.Currency, &v.ApprovedRecipients, &v.ApprovedAmountMinor, &v.PaymentReference, &paymentAt)
	if commercialErr != nil && !errors.Is(commercialErr, sql.ErrNoRows) {
		return v, commercialErr
	}
	if paymentAt.Valid {
		t := paymentAt.Time.UTC()
		v.PaymentReceivedAt = &t
	}
	if err := r.DB.QueryRowContext(ctx, `SELECT count(*) FROM campaign_recipients WHERE campaign_id=$1::uuid`, id).Scan(&v.RecipientObligations); err != nil {
		return v, err
	}
	var accepted, sent, delivered, read, failed, unknown int64
	metricsErr := r.DB.QueryRowContext(ctx, `SELECT submitted_total,sent_total,delivered_total,read_total,failed_total,unknown_total FROM campaign_metrics WHERE campaign_id=$1::uuid`, id).Scan(&accepted, &sent, &delivered, &read, &failed, &unknown)
	if metricsErr != nil && !errors.Is(metricsErr, sql.ErrNoRows) {
		return v, metricsErr
	}
	v.ProviderAccepted = accepted
	v.Sent = sent
	v.Delivered = delivered
	v.Read = read
	v.Failed = failed
	v.Unknown = unknown
	v.RecipientVariance = v.RecipientObligations - v.ApprovedRecipients
	switch {
	case v.CommercialStatus == "":
		v.ReconciliationStatus = FinancialReconciliationCommercialMissing
		v.Warnings = append(v.Warnings, "COMMERCIAL_EVIDENCE_UNAVAILABLE")
	case v.PaymentReference == "" || v.PaymentReceivedAt == nil:
		v.ReconciliationStatus = FinancialReconciliationPaymentMissing
		v.Warnings = append(v.Warnings, "PAYMENT_EVIDENCE_UNAVAILABLE")
	case v.RecipientVariance > 0:
		v.ReconciliationStatus = FinancialReconciliationOverAllocated
		v.Warnings = append(v.Warnings, "RECIPIENT_OBLIGATIONS_EXCEED_APPROVED_VOLUME")
	case v.RecipientVariance < 0:
		v.ReconciliationStatus = FinancialReconciliationUnderAllocated
		v.Warnings = append(v.Warnings, "APPROVED_VOLUME_NOT_FULLY_ALLOCATED")
	default:
		v.ReconciliationStatus = FinancialReconciliationBalanced
	}
	if v.Unknown > 0 {
		v.Warnings = append(v.Warnings, "UNKNOWN_OUTCOMES_REQUIRE_RECONCILIATION")
	}
	v.GeneratedAt = now
	return v, nil
}

func scanExport(s interface{ Scan(...any) error }) (ExportRequest, error) {
	var v ExportRequest
	var exp, asOf, generated, leaseExp, lastDownloaded, revoked sql.NullTime
	var criteria, frozen []byte
	err := s.Scan(
		&v.ID, &v.Kind, &v.ObjectID, &v.Format, &v.Status, &v.RequestedBy, &v.ApprovedBy,
		&v.Reason, &v.RejectionReason, &criteria, &v.TemplateVersion, &asOf, &frozen,
		&v.AuditHeadSequence, &v.AuditHeadHash, &v.WatermarkText,
		&v.CreatedAt, &v.UpdatedAt, &exp, &v.ObjectKey, &v.ContentType, &v.SHA256,
		&v.SizeBytes, &v.FailureCode, &v.FailureDetail, &generated,
		&v.DownloadCount, &lastDownloaded, &revoked, &v.RevokedBy, &v.RevocationReason,
		&v.LeaseOwner, &leaseExp, &v.Version,
	)
	if exp.Valid {
		t := exp.Time.UTC()
		v.ExpiresAt = &t
	}
	if asOf.Valid {
		t := asOf.Time.UTC()
		v.AsOf = &t
	}
	if generated.Valid {
		t := generated.Time.UTC()
		v.GeneratedAt = &t
	}
	if lastDownloaded.Valid {
		t := lastDownloaded.Time.UTC()
		v.LastDownloadedAt = &t
	}
	if revoked.Valid {
		t := revoked.Time.UTC()
		v.RevokedAt = &t
	}
	if leaseExp.Valid {
		t := leaseExp.Time.UTC()
		v.LeaseExpiresAt = &t
	}
	if len(criteria) > 0 {
		v.Criteria = append(json.RawMessage(nil), criteria...)
	}
	if len(frozen) > 0 {
		v.FrozenPayload = append(json.RawMessage(nil), frozen...)
	}
	return v, err
}

const exportSelect = `SELECT
 id::text,kind,coalesce(object_id::text,''),format,status,requested_by::text,coalesce(approved_by::text,''),
 reason,coalesce(rejection_reason,''),criteria,template_version,as_of,frozen_payload,
 coalesce(audit_head_sequence,0),coalesce(audit_head_hash,''),coalesce(watermark_text,''),
 created_at,updated_at,expires_at,coalesce(object_key,''),coalesce(content_type,''),coalesce(sha256,''),
 coalesce(size_bytes,0),coalesce(failure_code,''),coalesce(failure_detail,''),generated_at,
 download_count,last_downloaded_at,revoked_at,coalesce(revoked_by::text,''),coalesce(revocation_reason,''),
 coalesce(lease_owner,''),lease_expires_at,version
 FROM export_requests`

type exportExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertExport(ctx context.Context, exec exportExecer, v ExportRequest) error {
	criteria := string(v.Criteria)
	if strings.TrimSpace(criteria) == "" {
		criteria = `{}`
	}
	_, err := exec.ExecContext(ctx, `INSERT INTO export_requests(
 id,kind,object_id,format,status,requested_by,reason,criteria,template_version,watermark_text,
 created_at,updated_at,version
) VALUES($1::uuid,$2,NULLIF($3,'')::uuid,$4,$5,$6::uuid,$7,$8::jsonb,$9,NULLIF($10,''),$11,$12,$13)`,
		v.ID, v.Kind, v.ObjectID, v.Format, v.Status, v.RequestedBy, v.Reason, criteria,
		v.TemplateVersion, v.WatermarkText, v.CreatedAt, v.UpdatedAt, v.Version)
	return err
}

func (r *PostgreSQLRepository) CreateExport(ctx context.Context, v ExportRequest) (ExportRequest, error) {
	return v, insertExport(ctx, r.DB, v)
}

func (r *PostgreSQLRepository) CreateExportWithAudit(ctx context.Context, v ExportRequest, input audit.Input) (ExportRequest, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return ExportRequest{}, err
	}
	defer tx.Rollback()
	if err := insertExport(ctx, tx, v); err != nil {
		return ExportRequest{}, err
	}
	if _, err := audit.EnqueueTx(ctx, tx, input); err != nil {
		return ExportRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return ExportRequest{}, err
	}
	return v, nil
}

func (r *PostgreSQLRepository) GetExport(ctx context.Context, id string) (ExportRequest, error) {
	v, err := scanExport(r.DB.QueryRowContext(ctx, exportSelect+` WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

func (r *PostgreSQLRepository) ListExports(ctx context.Context, query ExportQuery) (ExportPage, error) {
	if r == nil || r.DB == nil {
		return ExportPage{}, errors.New("database is required")
	}
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	clauses := []string{"1=1"}
	args := []any{}
	add := func(format string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(format, len(args)))
	}
	if query.Status != "" {
		add("status=$%d", query.Status)
	}
	if value := strings.ToUpper(strings.TrimSpace(query.Kind)); value != "" {
		add("kind=$%d", value)
	}
	if value := strings.TrimSpace(query.RequestedBy); value != "" {
		add("requested_by=NULLIF($%d,'')::uuid", value)
	}
	if query.AfterCreatedAt != nil {
		args = append(args, query.AfterCreatedAt.UTC(), strings.TrimSpace(query.AfterID))
		createdArg, idArg := len(args)-1, len(args)
		clauses = append(clauses, fmt.Sprintf("(created_at < $%d OR (created_at=$%d AND id::text < $%d))", createdArg, createdArg, idArg))
	}
	args = append(args, limit+1)
	statement := exportSelect + " WHERE " + strings.Join(clauses, " AND ") + fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT $%d", len(args))
	rows, err := r.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return ExportPage{}, err
	}
	defer rows.Close()
	items := make([]ExportRequest, 0, limit+1)
	for rows.Next() {
		item, scanErr := scanExport(rows)
		if scanErr != nil {
			return ExportPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ExportPage{}, err
	}
	page := ExportPage{}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextAfter = last.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID
	} else {
		page.Items = items
	}
	return page, nil
}

func updateExport(ctx context.Context, exec exportExecer, v ExportRequest, expected int64) (ExportRequest, error) {
	var frozen any
	if len(v.FrozenPayload) > 0 {
		frozen = string(v.FrozenPayload)
	}
	res, err := exec.ExecContext(ctx, `UPDATE export_requests SET
 status=$2,approved_by=NULLIF($3,'')::uuid,rejection_reason=NULLIF($4,''),expires_at=$5,
 criteria=$6::jsonb,template_version=$7,as_of=$8,frozen_payload=$9::jsonb,
 audit_head_sequence=NULLIF($10,0),audit_head_hash=NULLIF($11,''),watermark_text=NULLIF($12,''),
 revoked_at=$13,revoked_by=NULLIF($14,'')::uuid,revocation_reason=NULLIF($15,''),
 updated_at=$16,version=version+1
 WHERE id=$1::uuid AND version=$17`,
		v.ID, v.Status, v.ApprovedBy, v.RejectionReason, v.ExpiresAt, string(v.Criteria),
		v.TemplateVersion, v.AsOf, frozen, v.AuditHeadSequence, v.AuditHeadHash, v.WatermarkText,
		v.RevokedAt, v.RevokedBy, v.RevocationReason, v.UpdatedAt, expected)
	if err != nil {
		return v, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return v, err
	}
	if n == 0 {
		return v, ErrConflict
	}
	v.Version = expected + 1
	return v, nil
}

func (r *PostgreSQLRepository) UpdateExport(ctx context.Context, v ExportRequest, expected int64) (ExportRequest, error) {
	return updateExport(ctx, r.DB, v, expected)
}

func (r *PostgreSQLRepository) UpdateExportWithAudit(ctx context.Context, v ExportRequest, expected int64, input audit.Input) (ExportRequest, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return ExportRequest{}, err
	}
	defer tx.Rollback()
	updated, err := updateExport(ctx, tx, v, expected)
	if err != nil {
		return ExportRequest{}, err
	}
	if _, err := audit.EnqueueTx(ctx, tx, input); err != nil {
		return ExportRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return ExportRequest{}, err
	}
	return updated, nil
}

func (r *PostgreSQLRepository) RevokeExportWithAudit(ctx context.Context, v ExportRequest, expected int64, now time.Time, input audit.Input) (ExportRequest, error) {
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ExportRequest{}, err
	}
	defer tx.Rollback()
	updated, err := updateExport(ctx, tx, v, expected)
	if err != nil {
		return ExportRequest{}, err
	}
	if err := revokeDownloadGrants(ctx, tx, v.ID, now); err != nil {
		return ExportRequest{}, err
	}
	if _, err := audit.EnqueueTx(ctx, tx, input); err != nil {
		return ExportRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return ExportRequest{}, err
	}
	return updated, nil
}

func insertDownloadGrant(ctx context.Context, exec exportExecer, grant DownloadGrant) error {
	_, err := exec.ExecContext(ctx, `INSERT INTO export_download_grants(
 id,export_id,actor_id,token_hash,request_id,expires_at,created_at
) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7)`,
		grant.ID, grant.ExportID, grant.ActorID, grant.TokenHash, grant.RequestID, grant.ExpiresAt, grant.CreatedAt)
	return err
}

func (r *PostgreSQLRepository) CreateDownloadGrant(ctx context.Context, grant DownloadGrant) (DownloadGrant, error) {
	return grant, insertDownloadGrant(ctx, r.DB, grant)
}

func (r *PostgreSQLRepository) CreateDownloadGrantWithAudit(ctx context.Context, grant DownloadGrant, input audit.Input) (DownloadGrant, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return DownloadGrant{}, err
	}
	defer tx.Rollback()
	if err := insertDownloadGrant(ctx, tx, grant); err != nil {
		return DownloadGrant{}, err
	}
	if _, err := audit.EnqueueTx(ctx, tx, input); err != nil {
		return DownloadGrant{}, err
	}
	if err := tx.Commit(); err != nil {
		return DownloadGrant{}, err
	}
	return grant, nil
}

func (r *PostgreSQLRepository) ConsumeDownloadGrant(ctx context.Context, exportID, tokenHash, actorID string, now time.Time) (ExportRequest, DownloadGrant, error) {
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	defer tx.Rollback()
	export, err := scanExport(tx.QueryRowContext(ctx, exportSelect+` WHERE id=$1::uuid FOR UPDATE`, exportID))
	if errors.Is(err, sql.ErrNoRows) {
		return ExportRequest{}, DownloadGrant{}, ErrNotFound
	}
	if err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	if export.Status != ExportReady || export.RevokedAt != nil || export.ExpiresAt == nil || !export.ExpiresAt.After(now) {
		return ExportRequest{}, DownloadGrant{}, ErrConflict
	}
	var grant DownloadGrant
	var used, revoked sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id::text,export_id::text,actor_id::text,token_hash,request_id,expires_at,used_at,revoked_at,created_at
 FROM export_download_grants
 WHERE export_id=$1::uuid AND token_hash=$2 AND actor_id=$3::uuid
 FOR UPDATE`, exportID, tokenHash, actorID).Scan(
		&grant.ID, &grant.ExportID, &grant.ActorID, &grant.TokenHash, &grant.RequestID,
		&grant.ExpiresAt, &used, &revoked, &grant.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ExportRequest{}, DownloadGrant{}, ErrNotFound
	}
	if err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	if used.Valid || revoked.Valid || !grant.ExpiresAt.After(now) {
		return ExportRequest{}, DownloadGrant{}, ErrConflict
	}
	res, err := tx.ExecContext(ctx, `UPDATE export_download_grants SET used_at=$2 WHERE id=$1::uuid AND used_at IS NULL AND revoked_at IS NULL`, grant.ID, now)
	if err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	if n != 1 {
		return ExportRequest{}, DownloadGrant{}, ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE export_requests SET download_count=download_count+1,last_downloaded_at=$2,updated_at=$2,version=version+1 WHERE id=$1::uuid`, exportID, now); err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	if err = tx.Commit(); err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	usedAt := now.UTC()
	grant.UsedAt = &usedAt
	export.DownloadCount++
	export.LastDownloadedAt = &usedAt
	export.UpdatedAt = usedAt
	export.Version++
	return export, grant, nil
}

func (r *PostgreSQLRepository) ConsumeDownloadGrantWithAudit(ctx context.Context, exportID, tokenHash, actorID string, now time.Time, input audit.Input) (ExportRequest, DownloadGrant, error) {
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	defer tx.Rollback()
	export, err := scanExport(tx.QueryRowContext(ctx, exportSelect+` WHERE id=$1::uuid FOR UPDATE`, exportID))
	if errors.Is(err, sql.ErrNoRows) {
		return ExportRequest{}, DownloadGrant{}, ErrNotFound
	}
	if err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	if export.Status != ExportReady || export.RevokedAt != nil || export.ExpiresAt == nil || !export.ExpiresAt.After(now) {
		return ExportRequest{}, DownloadGrant{}, ErrConflict
	}
	var grant DownloadGrant
	var used, revoked sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id::text,export_id::text,actor_id::text,token_hash,request_id,expires_at,used_at,revoked_at,created_at FROM export_download_grants WHERE export_id=$1::uuid AND token_hash=$2 AND actor_id=$3::uuid FOR UPDATE`, exportID, tokenHash, actorID).Scan(&grant.ID, &grant.ExportID, &grant.ActorID, &grant.TokenHash, &grant.RequestID, &grant.ExpiresAt, &used, &revoked, &grant.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ExportRequest{}, DownloadGrant{}, ErrNotFound
	}
	if err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	if used.Valid || revoked.Valid || !grant.ExpiresAt.After(now) {
		return ExportRequest{}, DownloadGrant{}, ErrConflict
	}
	res, err := tx.ExecContext(ctx, `UPDATE export_download_grants SET used_at=$2 WHERE id=$1::uuid AND used_at IS NULL AND revoked_at IS NULL`, grant.ID, now)
	if err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	if n != 1 {
		return ExportRequest{}, DownloadGrant{}, ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE export_requests SET download_count=download_count+1,last_downloaded_at=$2,updated_at=$2,version=version+1 WHERE id=$1::uuid`, exportID, now); err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	input.After = map[string]any{"grantId": grant.ID, "sha256": export.SHA256, "sizeBytes": export.SizeBytes}
	if _, err := audit.EnqueueTx(ctx, tx, input); err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	if err = tx.Commit(); err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	usedAt := now.UTC()
	grant.UsedAt = &usedAt
	export.DownloadCount++
	export.LastDownloadedAt = &usedAt
	export.UpdatedAt = usedAt
	export.Version++
	return export, grant, nil
}

func revokeDownloadGrants(ctx context.Context, exec exportExecer, exportID string, now time.Time) error {
	_, err := exec.ExecContext(ctx, `UPDATE export_download_grants SET revoked_at=$2 WHERE export_id=$1::uuid AND used_at IS NULL AND revoked_at IS NULL`, exportID, now)
	return err
}

func (r *PostgreSQLRepository) RevokeDownloadGrants(ctx context.Context, exportID string, now time.Time) error {
	return revokeDownloadGrants(ctx, r.DB, exportID, now)
}

func (r *PostgreSQLRepository) ListExceptions(ctx context.Context, campaignID string, limit int) ([]DeliveryException, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,campaign_id::text,status,coalesce(assigned_session_id::text,''),coalesce(provider_message_id,''),attempt_count,coalesce(last_error_code,''),updated_at FROM campaign_recipients WHERE ($1='' OR campaign_id=NULLIF($1,'')::uuid) AND (status IN('FAILED_RETRYABLE','FAILED_PERMANENT','UNKNOWN') OR reconciliation_required=true) ORDER BY updated_at DESC LIMIT $2`, campaignID, limit)
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
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrConflict
	}
	return nil
}
func (r *PostgreSQLRepository) FailExport(ctx context.Context, id, code, detail string, now time.Time) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE export_requests SET status='FAILED',failure_code=$2,failure_detail=$3,lease_owner=NULL,lease_expires_at=NULL,updated_at=$4,version=version+1 WHERE id=$1::uuid AND status='PROCESSING'`, id, code, detail, now)
	return err
}
func (r *PostgreSQLRepository) ClaimExpiringExports(ctx context.Context, now time.Time, limit int) ([]ExportRequest, error) {
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
	var out []ExportRequest
	for rows.Next() {
		v, scanErr := scanExport(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		out = append(out, v)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for index := range out {
		if _, err = tx.ExecContext(ctx, `UPDATE export_requests SET status='EXPIRING',updated_at=$2,version=version+1 WHERE id=$1::uuid AND status='READY'`, out[index].ID, now); err != nil {
			return nil, err
		}
		out[index].Status = ExportExpiring
		out[index].Version++
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *PostgreSQLRepository) CompleteExpiration(ctx context.Context, id string, now time.Time) error {
	res, err := r.DB.ExecContext(ctx, `UPDATE export_requests SET status='EXPIRED',object_key=NULL,updated_at=$2,version=version+1 WHERE id=$1::uuid AND status='EXPIRING'`, id, now)
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

func (r *PostgreSQLRepository) FailExpiration(ctx context.Context, id, detail string, now time.Time) error {
	res, err := r.DB.ExecContext(ctx, `UPDATE export_requests SET status='READY',failure_code='EXPIRY_DELETE_FAILED',failure_detail=$2,updated_at=$3,version=version+1 WHERE id=$1::uuid AND status='EXPIRING'`, id, detail, now)
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

func (r *PostgreSQLRepository) ListIncidentPage(ctx context.Context, status IncidentStatus, limit int, afterSeverity int, before *time.Time, beforeID string) ([]Incident, error) {
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	const rank = `CASE severity WHEN 'CRITICAL' THEN 1 WHEN 'WARNING' THEN 2 ELSE 3 END`
	query := incidentSelect + ` WHERE ($1='' OR status=$1)
AND ($3::timestamptz IS NULL OR ` + rank + `>$4 OR (` + rank + `=$4 AND (created_at<$3 OR (created_at=$3 AND id<NULLIF($5,'')::uuid))))
ORDER BY ` + rank + `,created_at DESC,id DESC LIMIT $2`
	rows, err := r.DB.QueryContext(ctx, query, status, limit, before, afterSeverity, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Incident, 0, limit)
	for rows.Next() {
		value, scanErr := scanIncident(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, value)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) ListExceptionPage(ctx context.Context, campaignID string, limit int, before *time.Time, beforeID string) ([]DeliveryException, error) {
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,campaign_id::text,status,coalesce(assigned_session_id::text,''),coalesce(provider_message_id,''),attempt_count,coalesce(last_error_code,''),updated_at
FROM campaign_recipients
WHERE ($1='' OR campaign_id=NULLIF($1,'')::uuid)
  AND (status IN('FAILED_RETRYABLE','FAILED_PERMANENT','UNKNOWN') OR reconciliation_required=true)
  AND ($3::timestamptz IS NULL OR updated_at<$3 OR (updated_at=$3 AND id<NULLIF($4,'')::uuid))
ORDER BY updated_at DESC,id DESC LIMIT $2`, campaignID, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]DeliveryException, 0, limit)
	for rows.Next() {
		var value DeliveryException
		if err := rows.Scan(&value.RecipientID, &value.CampaignID, &value.Status, &value.AssignedSessionID, &value.ProviderMessageID, &value.AttemptCount, &value.ErrorCode, &value.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, rows.Err()
}
