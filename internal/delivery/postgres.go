package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type PostgreSQLRepository struct{ DB *sql.DB }

func (r *PostgreSQLRepository) Create(ctx context.Context, v Recipient) error {
	if r.DB == nil {
		return errors.New("database is required")
	}
	const q = `INSERT INTO campaign_recipients(id,campaign_id,contact_id,message_version_id,idempotency_key,status,attempt_count,authorised_at,updated_at,version,reconciliation_required,contradictory_event_count) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8,1,false,0)`
	_, err := r.DB.ExecContext(ctx, q, v.ID, v.CampaignID, v.ContactID, v.MessageVersionID, v.IdempotencyKey, v.Status, v.AttemptCount, v.UpdatedAt.UTC())
	return err
}
func (r *PostgreSQLRepository) Get(ctx context.Context, id string) (Recipient, error) {
	if r.DB == nil {
		return Recipient{}, errors.New("database is required")
	}
	v, err := scanRecipient(r.DB.QueryRowContext(ctx, recipientSelect+` WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Recipient{}, ErrRecipientNotFound
	}
	return v, err
}
func (r *PostgreSQLRepository) GetByProviderMessageID(ctx context.Context, providerMessageID string) (Recipient, error) {
	if r.DB == nil {
		return Recipient{}, errors.New("database is required")
	}
	providerMessageID = strings.TrimSpace(providerMessageID)
	if providerMessageID == "" {
		return Recipient{}, ErrRecipientNotFound
	}
	v, err := scanRecipient(r.DB.QueryRowContext(ctx, recipientSelect+` WHERE provider_message_id=$1`, providerMessageID))
	if errors.Is(err, sql.ErrNoRows) {
		return Recipient{}, ErrRecipientNotFound
	}
	return v, err
}
func (r *PostgreSQLRepository) ApplyEvent(ctx context.Context, id string, event Event) (Recipient, bool, error) {
	if r.DB == nil {
		return Recipient{}, false, errors.New("database is required")
	}
	if event.DeduplicationKey == "" || event.OccurredAt.IsZero() {
		return Recipient{}, false, errors.New("event key and occurrence time are required")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Recipient{}, false, err
	}
	defer tx.Rollback()
	payload, _ := json.Marshal(map[string]any{"errorCode": event.ErrorCode, "errorDetail": event.ErrorDetail})
	fingerprint := Fingerprint(event)
	var eventID string
	err = tx.QueryRowContext(ctx, `INSERT INTO delivery_events(campaign_recipient_id,provider_event_id,provider_message_id,event_type,occurred_at,payload,event_deduplication_key,event_fingerprint) VALUES($1,NULLIF($2,''),NULLIF($3,''),$4,$5,$6,$7,$8) ON CONFLICT(event_deduplication_key) WHERE event_deduplication_key IS NOT NULL DO NOTHING RETURNING id`, id, event.ProviderEventID, event.ProviderMessageID, event.Type, event.OccurredAt.UTC(), string(payload), event.DeduplicationKey, fingerprint).Scan(&eventID)
	if errors.Is(err, sql.ErrNoRows) {
		var existingRecipientID, existingFingerprint string
		lookupErr := tx.QueryRowContext(ctx, `SELECT campaign_recipient_id::text,event_fingerprint FROM delivery_events WHERE event_deduplication_key=$1`, event.DeduplicationKey).Scan(&existingRecipientID, &existingFingerprint)
		if lookupErr != nil {
			return Recipient{}, false, lookupErr
		}
		if existingRecipientID != id || existingFingerprint != fingerprint {
			return Recipient{}, false, ErrEventDedupMismatch
		}
		current, getErr := scanRecipient(tx.QueryRowContext(ctx, recipientSelect+` WHERE id=$1`, id))
		if getErr != nil {
			return Recipient{}, false, getErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return Recipient{}, false, commitErr
		}
		return current, false, nil
	}
	if err != nil {
		return Recipient{}, false, fmt.Errorf("insert delivery event: %w", err)
	}
	current, err := scanRecipient(tx.QueryRowContext(ctx, recipientSelect+` WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Recipient{}, false, ErrRecipientNotFound
	}
	if err != nil {
		return Recipient{}, false, err
	}
	if event.Type == EventSubmitting {
		var campaignStatus string
		if err = tx.QueryRowContext(ctx, `SELECT status FROM campaigns WHERE id=$1::uuid FOR UPDATE`, current.CampaignID).Scan(&campaignStatus); err != nil {
			return Recipient{}, false, fmt.Errorf("lock campaign for submission: %w", err)
		}
		if campaignStatus != "SCHEDULED" && campaignStatus != "DISPATCHING" {
			return Recipient{}, false, fmt.Errorf("%w: %s", ErrCampaignNotDispatchable, campaignStatus)
		}
	}
	next, changed, err := Apply(current, event)
	if err != nil {
		return Recipient{}, false, err
	}
	if changed {
		_, err = tx.ExecContext(ctx, `UPDATE campaign_recipients SET status=$2,provider_message_id=NULLIF($3,''),attempt_count=$4,last_error_code=NULLIF($5,''),last_error_detail=NULLIF($6,''),submitted_at=$7,completed_at=$8,updated_at=$9,last_event_at=$10,reconciliation_required=$11,contradictory_event_count=$12,highest_acknowledgement=NULLIF($13,''),version=version+1 WHERE id=$1`, id, next.Status, next.ProviderMessageID, next.AttemptCount, next.LastErrorCode, next.LastErrorDetail, next.SubmittedAt, next.CompletedAt, next.UpdatedAt, next.LastEventAt, next.ReconciliationRequired, next.ContradictoryEventCount, next.HighestAcknowledgement)
		if err != nil {
			return Recipient{}, false, fmt.Errorf("update recipient ledger: %w", err)
		}
		delta := Delta(current.Status, next.Status)
		_, err = tx.ExecContext(ctx, `
INSERT INTO campaign_metrics(campaign_id,updated_at) VALUES($1,$11)
ON CONFLICT (campaign_id) DO UPDATE SET
 authorised_total=GREATEST(0,campaign_metrics.authorised_total+$2),
 queued_total=GREATEST(0,campaign_metrics.queued_total+$3),
 submitted_total=GREATEST(0,campaign_metrics.submitted_total+$4),
 sent_total=GREATEST(0,campaign_metrics.sent_total+$5),
 delivered_total=GREATEST(0,campaign_metrics.delivered_total+$6),
 read_total=GREATEST(0,campaign_metrics.read_total+$7),
 failed_total=GREATEST(0,campaign_metrics.failed_total+$8),
 unknown_total=GREATEST(0,campaign_metrics.unknown_total+$9),
 suppressed_total=GREATEST(0,campaign_metrics.suppressed_total+$10),
 updated_at=$11`, current.CampaignID, delta.AuthorisedTotal, delta.QueuedTotal, delta.SubmittedTotal, delta.SentTotal, delta.DeliveredTotal, delta.ReadTotal, delta.FailedTotal, delta.UnknownTotal, delta.SuppressedTotal, next.UpdatedAt)
		if err != nil {
			return Recipient{}, false, fmt.Errorf("update campaign metrics: %w", err)
		}
		if canonicalSenderSuccessEvent(event, next) {
			_, err = tx.ExecContext(ctx, `UPDATE sender_sessions
SET last_success_at=CASE WHEN last_success_at IS NULL OR last_success_at<$2 THEN $2 ELSE last_success_at END
WHERE id=(SELECT assigned_session_id FROM campaign_recipients WHERE id=$1::uuid)`, id, event.OccurredAt.UTC())
			if err != nil {
				return Recipient{}, false, fmt.Errorf("update sender success evidence: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return Recipient{}, false, err
	}
	return next, changed, nil
}

func canonicalSenderSuccessEvent(event Event, next Recipient) bool {
	providerMessageID := strings.TrimSpace(event.ProviderMessageID)
	if providerMessageID == "" || providerMessageID != strings.TrimSpace(next.ProviderMessageID) {
		return false
	}
	switch event.Type {
	case EventSent, EventDelivered, EventRead:
		return true
	default:
		return false
	}
}

const recipientSelect = `SELECT id,campaign_id,contact_id,message_version_id,idempotency_key,status,coalesce(highest_acknowledgement,''),coalesce(provider_message_id,''),attempt_count,coalesce(last_error_code,''),coalesce(last_error_detail,''),submitted_at,completed_at,updated_at,last_event_at,reconciliation_required,contradictory_event_count FROM campaign_recipients`

type recipientScanner interface{ Scan(...any) error }

func scanRecipient(row recipientScanner) (Recipient, error) {
	var v Recipient
	var submitted, completed, lastEvent sql.NullTime
	var status string
	err := row.Scan(&v.ID, &v.CampaignID, &v.ContactID, &v.MessageVersionID, &v.IdempotencyKey, &status, &v.HighestAcknowledgement, &v.ProviderMessageID, &v.AttemptCount, &v.LastErrorCode, &v.LastErrorDetail, &submitted, &completed, &v.UpdatedAt, &lastEvent, &v.ReconciliationRequired, &v.ContradictoryEventCount)
	if err != nil {
		return Recipient{}, err
	}
	v.Status = Status(status)
	if submitted.Valid {
		t := submitted.Time
		v.SubmittedAt = &t
	}
	if completed.Valid {
		t := completed.Time
		v.CompletedAt = &t
	}
	if lastEvent.Valid {
		t := lastEvent.Time
		v.LastEventAt = &t
	}
	return v, nil
}

var _ = time.Time{}

func (r *PostgreSQLRepository) ResolveReconciliation(ctx context.Context, id string, expected, resolved Status, actor, action, evidence, reason string, now time.Time) (Recipient, error) {
	return r.resolveReconciliation(ctx, id, expected, resolved, actor, action, evidence, reason, now, nil)
}

func (r *PostgreSQLRepository) ResolveReconciliationWithEvidence(ctx context.Context, id string, expected, resolved Status, actor, action, evidence, reason string, now time.Time, writer ReconciliationEvidenceWriter) (Recipient, error) {
	return r.resolveReconciliation(ctx, id, expected, resolved, actor, action, evidence, reason, now, writer)
}

func (r *PostgreSQLRepository) resolveReconciliation(ctx context.Context, id string, expected, resolved Status, actor, action, evidence, reason string, now time.Time, writer ReconciliationEvidenceWriter) (Recipient, error) {
	if r == nil || r.DB == nil {
		return Recipient{}, errors.New("database is required")
	}
	if strings.TrimSpace(id) == "" || resolved == "" || strings.TrimSpace(actor) == "" || strings.TrimSpace(action) == "" || strings.TrimSpace(evidence) == "" || strings.TrimSpace(reason) == "" || now.IsZero() {
		return Recipient{}, errors.New("complete reconciliation evidence is required")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return Recipient{}, err
	}
	defer tx.Rollback()
	current, err := scanRecipient(tx.QueryRowContext(ctx, recipientSelect+` WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Recipient{}, ErrRecipientNotFound
	}
	if err != nil {
		return Recipient{}, err
	}
	if err := validateReconciliationResolution(current, expected, resolved, action); err != nil {
		return Recipient{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO delivery_exception_resolutions(campaign_recipient_id,action,evidence_reference,reason,actor_id,resolved_at,result_status) VALUES($1::uuid,$2,$3,$4,$5::uuid,$6,$7)`, id, action, evidence, reason, actor, now.UTC(), resolved); err != nil {
		return Recipient{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE campaign_recipients
		SET status=$2,reconciliation_required=false,
		    last_error_code=CASE WHEN $2 IN ('FAILED_RETRYABLE','FAILED_PERMANENT') THEN 'OPERATOR_RECONCILIATION' ELSE NULL END,
		    last_error_detail=NULL,updated_at=$3,version=version+1
		WHERE id=$1::uuid`, id, resolved, now.UTC()); err != nil {
		return Recipient{}, err
	}
	if writer != nil {
		if err := writer(ctx, tx); err != nil {
			return Recipient{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Recipient{}, err
	}
	current.Status = resolved
	current.ReconciliationRequired = false
	current.LastErrorDetail = ""
	switch resolved {
	case StatusFailedRetryable, StatusFailedPermanent:
		current.LastErrorCode = "OPERATOR_RECONCILIATION"
	default:
		current.LastErrorCode = ""
	}
	current.UpdatedAt = now.UTC()
	return current, nil
}
