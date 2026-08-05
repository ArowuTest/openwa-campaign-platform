package orchestration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"campaign-platform/internal/delivery"
	pgretry "campaign-platform/internal/persistence/postgres"
	"campaign-platform/internal/shared/id"
)

// TransactionalEligibilityChecker evaluates live consent and suppression inside the
// same serializable transaction that creates campaign obligations.
type TransactionalEligibilityChecker interface {
	CheckTx(context.Context, *sql.Tx, string, string, string, string, time.Time) (EligibilityDecision, error)
}

// SQLFinalEligibilityChecker implements the mandatory last eligibility check. It
// deliberately checks the current source-of-truth tables rather than trusting snapshot data.
type SQLFinalEligibilityChecker struct{}

func (SQLFinalEligibilityChecker) Check(context.Context, string, string, string, string, time.Time) (EligibilityDecision, error) {
	return EligibilityDecision{}, errors.New("SQL final eligibility must run inside a transaction")
}

func (SQLFinalEligibilityChecker) CheckTx(ctx context.Context, tx *sql.Tx, contactID, organisationID, purposeID, channel string, asOf time.Time) (EligibilityDecision, error) {
	const query = `
WITH latest_grant AS (
  SELECT g.status, g.expires_at
  FROM consent_grants g
  WHERE g.contact_id = $1::uuid
    AND g.organisation_id = $2::uuid
    AND g.purpose_id = $3
    AND upper(g.channel) = upper($4)
    AND g.effective_from <= $5
  ORDER BY g.effective_from DESC, g.created_at DESC, g.id DESC
  LIMIT 1
)
SELECT CASE
  WHEN c.status <> 'ACTIVE' THEN 'CONTACT_INACTIVE'
  WHEN EXISTS (
    SELECT 1 FROM suppressions s
    WHERE s.contact_id = c.id
      AND s.active
      AND s.effective_at <= $5
      AND (s.expires_at IS NULL OR s.expires_at > $5)
      AND (
        s.scope = 'GLOBAL'
        OR (s.scope = 'ORGANISATION' AND s.organisation_id = $2::uuid)
        OR (s.scope = 'PURPOSE' AND s.purpose_id = $3)
        OR (s.scope = 'CHANNEL' AND upper(s.channel) = upper($4))
        OR (s.scope = 'TEMPORARY'
            AND (s.organisation_id IS NULL OR s.organisation_id = $2::uuid)
            AND (s.purpose_id IS NULL OR s.purpose_id = $3)
            AND (s.channel IS NULL OR upper(s.channel) = upper($4)))
      )
  ) THEN 'SUPPRESSED'
  WHEN EXISTS (
    SELECT 1
    FROM organisation_policy_versions op
    CROSS JOIN LATERAL jsonb_to_recordset(op.frequency_caps)
      AS fc("purposeId" text, "channel" text, "maxMessages" integer, "windowHours" integer)
    WHERE op.organisation_id = $2::uuid
      AND op.status = 'ACTIVE'
      AND op.effective_from <= $5
      AND (op.effective_to IS NULL OR op.effective_to > $5)
      AND (coalesce(fc."purposeId", '') = '' OR fc."purposeId" = $3)
      AND upper(fc."channel") = upper($4)
      AND (
        SELECT count(*)
        FROM campaign_recipients recent
        JOIN campaigns recent_campaign ON recent_campaign.id = recent.campaign_id
        WHERE recent.contact_id = c.id
          AND recent_campaign.organisation_id = $2::uuid
          AND recent_campaign.purpose_id = $3
          AND recent.status NOT IN ('CANCELLED','SUPPRESSED_BEFORE_SEND')
          AND recent.authorised_at > $5 - make_interval(hours => fc."windowHours")
      ) >= fc."maxMessages"
  ) THEN 'FREQUENCY_CAPPED'
  WHEN NOT EXISTS (SELECT 1 FROM latest_grant) THEN 'NO_ACTIVE_CONSENT'
  WHEN (SELECT status FROM latest_grant) = 'WITHDRAWN' THEN 'CONSENT_WITHDRAWN'
  WHEN (SELECT status FROM latest_grant) = 'REVOKED' THEN 'CONSENT_REVOKED'
  WHEN (SELECT status FROM latest_grant) = 'EXPIRED'
    OR ((SELECT status FROM latest_grant) = 'ACTIVE'
        AND (SELECT expires_at FROM latest_grant) IS NOT NULL
        AND (SELECT expires_at FROM latest_grant) <= $5) THEN 'CONSENT_EXPIRED'
  WHEN (SELECT status FROM latest_grant) <> 'ACTIVE' THEN 'NO_ACTIVE_CONSENT'
  ELSE ''
END AS exclusion_reason
FROM contacts c
WHERE c.id = $1::uuid
FOR SHARE`
	var reason string
	err := tx.QueryRowContext(ctx, query, contactID, organisationID, purposeID, channel, asOf.UTC()).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return EligibilityDecision{Eligible: false, ExclusionReason: "CONTACT_NOT_FOUND"}, nil
	}
	if err != nil {
		return EligibilityDecision{}, fmt.Errorf("evaluate final eligibility: %w", err)
	}
	reason = strings.TrimSpace(reason)
	if reason != "" {
		return EligibilityDecision{Eligible: false, ExclusionReason: reason}, nil
	}
	return EligibilityDecision{Eligible: true}, nil
}

// PostgreSQLStore atomically creates recipient obligations and outbox records. It uses
// SERIALIZABLE isolation so concurrent entitlement changes, withdrawals or suppressions
// cannot silently produce a partially authorised batch.
type PostgreSQLStore struct {
	DB *sql.DB
}

func (s *PostgreSQLStore) Authorise(ctx context.Context, cmd Command, checker EligibilityChecker) (Result, error) {
	return pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (Result, error) {
		return s.authoriseOnce(ctx, cmd, checker)
	})
}

func (s *PostgreSQLStore) authoriseOnce(ctx context.Context, cmd Command, checker EligibilityChecker) (Result, error) {
	if s == nil || s.DB == nil {
		return Result{}, errors.New("database is required")
	}
	if err := validateCommand(cmd); err != nil {
		return Result{}, err
	}
	if len(cmd.Members) > 5_000 {
		return Result{}, errors.New("release transaction batch exceeds 5000 members")
	}
	txChecker, ok := checker.(TransactionalEligibilityChecker)
	if !ok {
		return Result{}, errors.New("PostgreSQL release requires a transactional eligibility checker")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback()

	var maximum int64
	var status, snapshotID, messageVersionID, organisationID, purposeID, organisationStatus, reviewStatus, reviewChannel string
	var reviewExpiresAt time.Time
	const campaignQuery = `
SELECT c.maximum_unique_recipients, c.status, coalesce(c.audience_snapshot_id::text,''),
       coalesce(c.approved_message_version_id::text,''), c.organisation_id::text, c.purpose_id::text,
       o.status, cr.status, cr.channel, cr.expires_at
FROM campaigns c
JOIN organisations o ON o.id = c.organisation_id
JOIN consent_reviews cr ON cr.id = c.consent_review_id
WHERE c.id=$1::uuid FOR UPDATE OF c`
	if err := tx.QueryRowContext(ctx, campaignQuery, cmd.CampaignID).Scan(&maximum, &status, &snapshotID, &messageVersionID, &organisationID, &purposeID, &organisationStatus, &reviewStatus, &reviewChannel, &reviewExpiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Result{}, errors.New("campaign not found")
		}
		return Result{}, fmt.Errorf("lock campaign: %w", err)
	}
	if status != "SCHEDULED" && status != "DISPATCHING" {
		return Result{}, fmt.Errorf("campaign status %s cannot release recipients", status)
	}
	if organisationStatus != "ACTIVE" {
		return Result{}, errors.New("campaign organisation is not active")
	}
	if reviewStatus != "APPROVED" || !reviewExpiresAt.After(cmd.AsOf.UTC()) || !strings.EqualFold(reviewChannel, cmd.Channel) {
		return Result{}, errors.New("campaign consent review is no longer valid for release")
	}
	if snapshotID != cmd.SnapshotID || messageVersionID != cmd.MessageVersionID || organisationID != cmd.OrganisationID || purposeID != cmd.PurposeID {
		return Result{}, ErrReleaseConflict
	}
	if cmd.MaximumUniqueRecipients != maximum {
		return Result{}, errors.New("release entitlement does not match authoritative campaign entitlement")
	}

	members := append([]Member(nil), cmd.Members...)
	sort.Slice(members, func(i, j int) bool { return members[i].ContactID < members[j].ContactID })
	seen := make(map[string]struct{}, len(members))
	result := Result{ExclusionReasons: make(map[string]int)}
	type authorisedMember struct {
		Member
		RecipientID, Key string
	}
	type excludedMember struct {
		Member
		Reason string
	}
	authorised := make([]authorisedMember, 0, len(members))
	excluded := make([]excludedMember, 0)
	for _, member := range members {
		if strings.TrimSpace(member.ContactID) == "" || strings.TrimSpace(member.EligibilityEvidenceHash) == "" {
			return Result{}, errors.New("contact ID and eligibility evidence hash are required")
		}
		if _, duplicate := seen[member.ContactID]; duplicate {
			return Result{}, fmt.Errorf("duplicate contact %s in release batch", member.ContactID)
		}
		seen[member.ContactID] = struct{}{}
		decision, err := txChecker.CheckTx(ctx, tx, member.ContactID, cmd.OrganisationID, cmd.PurposeID, cmd.Channel, cmd.AsOf)
		if err != nil {
			return Result{}, err
		}
		if !decision.Eligible {
			reason := strings.TrimSpace(decision.ExclusionReason)
			if reason == "" {
				reason = "INELIGIBLE_FINAL_CHECK"
			}
			excluded = append(excluded, excludedMember{Member: member, Reason: reason})
			continue
		}
		key, err := delivery.NewIdempotencyKey(cmd.CampaignID, member.ContactID, cmd.MessageVersionID)
		if err != nil {
			return Result{}, err
		}
		recipientID, err := id.New()
		if err != nil {
			return Result{}, err
		}
		authorised = append(authorised, authorisedMember{Member: member, RecipientID: recipientID, Key: key})
	}

	var existingCount int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM campaign_recipients WHERE campaign_id=$1::uuid`, cmd.CampaignID).Scan(&existingCount); err != nil {
		return Result{}, fmt.Errorf("count campaign recipients: %w", err)
	}
	now := cmd.AsOf.UTC()
	for _, item := range excluded {
		exclusionID, err := id.New()
		if err != nil {
			return Result{}, err
		}
		tag, err := tx.ExecContext(ctx, `
INSERT INTO campaign_release_exclusions(
 id,campaign_id,snapshot_id,contact_id,eligibility_evidence_hash,reason_code,evaluated_at,created_at
) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,$7)
ON CONFLICT (campaign_id,snapshot_id,contact_id) DO NOTHING`, exclusionID, cmd.CampaignID, cmd.SnapshotID, item.ContactID, item.EligibilityEvidenceHash, item.Reason, now)
		if err != nil {
			return Result{}, fmt.Errorf("insert release exclusion: %w", err)
		}
		inserted, err := tag.RowsAffected()
		if err != nil {
			return Result{}, err
		}
		if inserted == 0 {
			result.Existing++
			continue
		}
		result.Excluded++
		result.ExclusionReasons[item.Reason]++
	}

	for _, item := range authorised {
		resultTag, err := tx.ExecContext(ctx, `
INSERT INTO campaign_recipients(
 id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,
 eligibility_evidence_hash,attempt_count,authorised_at,updated_at,version
) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'AUTHORISED',$7,0,$8,$8,1)
ON CONFLICT (campaign_id,contact_id,message_version_id) DO NOTHING`, item.RecipientID, cmd.CampaignID, cmd.SnapshotID, item.ContactID, cmd.MessageVersionID, item.Key, item.EligibilityEvidenceHash, now)
		if err != nil {
			return Result{}, fmt.Errorf("insert campaign recipient: %w", err)
		}
		inserted, err := resultTag.RowsAffected()
		if err != nil {
			return Result{}, err
		}
		if inserted == 0 {
			result.Existing++
			continue
		}
		payload, err := json.Marshal(map[string]any{
			"campaignRecipientId": item.RecipientID, "campaignId": cmd.CampaignID,
			"snapshotId": cmd.SnapshotID, "shard": shardFor(item.ContactID, cmd.ShardSize),
			"eligibilityEvidenceHash": item.EligibilityEvidenceHash,
		})
		if err != nil {
			return Result{}, err
		}
		outboxID, err := id.New()
		if err != nil {
			return Result{}, err
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO transactional_outbox(
 id,deduplication_key,aggregate_type,aggregate_id,event_type,payload,status,available_at,created_at
) VALUES($1::uuid,$2,'CAMPAIGN_RECIPIENT',$3::uuid,'CAMPAIGN_RECIPIENT_AUTHORISED',$4::jsonb,'PENDING',$5,$5)
ON CONFLICT (deduplication_key) DO NOTHING`, outboxID, "dispatch:"+item.Key, item.RecipientID, payload, now)
		if err != nil {
			return Result{}, fmt.Errorf("insert transactional outbox: %w", err)
		}
		result.Authorised++
		result.OutboxCreated++
		if existingCount+int64(result.Authorised) > maximum {
			return Result{}, ErrEntitlementExceeded
		}
	}
	if result.Authorised > 0 || result.Excluded > 0 {
		_, err = tx.ExecContext(ctx, `
INSERT INTO campaign_metrics(campaign_id,authorised_total,excluded_final_check_total,updated_at) VALUES($1::uuid,$2,$3,$4)
ON CONFLICT (campaign_id) DO UPDATE SET
 authorised_total=campaign_metrics.authorised_total+$2,
 excluded_final_check_total=campaign_metrics.excluded_final_check_total+$3,
 updated_at=$4`, cmd.CampaignID, result.Authorised, result.Excluded, now)
		if err != nil {
			return Result{}, fmt.Errorf("update authorised campaign metrics: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Result{}, fmt.Errorf("commit recipient release: %w", err)
	}
	return result, nil
}

func (s *PostgreSQLStore) Recipients(ctx context.Context, campaignID string) []delivery.Recipient {
	if s == nil || s.DB == nil {
		return nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,campaign_id,contact_id,message_version_id,idempotency_key,status,coalesce(highest_acknowledgement,''),coalesce(provider_message_id,''),attempt_count,coalesce(last_error_code,''),coalesce(last_error_detail,''),submitted_at,completed_at,updated_at,last_event_at,reconciliation_required,contradictory_event_count FROM campaign_recipients WHERE ($1='' OR campaign_id=$1::uuid) ORDER BY contact_id`, campaignID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	items := []delivery.Recipient{}
	for rows.Next() {
		var value delivery.Recipient
		var status string
		var submitted, completed, lastEvent sql.NullTime
		if err := rows.Scan(&value.ID, &value.CampaignID, &value.ContactID, &value.MessageVersionID, &value.IdempotencyKey, &status, &value.HighestAcknowledgement, &value.ProviderMessageID, &value.AttemptCount, &value.LastErrorCode, &value.LastErrorDetail, &submitted, &completed, &value.UpdatedAt, &lastEvent, &value.ReconciliationRequired, &value.ContradictoryEventCount); err != nil {
			return nil
		}
		value.Status = delivery.Status(status)
		if submitted.Valid {
			v := submitted.Time
			value.SubmittedAt = &v
		}
		if completed.Valid {
			v := completed.Time
			value.CompletedAt = &v
		}
		if lastEvent.Valid {
			v := lastEvent.Time
			value.LastEventAt = &v
		}
		items = append(items, value)
	}
	return items
}

func (s *PostgreSQLStore) Outbox(ctx context.Context) []Outbox {
	if s == nil || s.DB == nil {
		return nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,deduplication_key,event_type,aggregate_id::text,payload,created_at FROM transactional_outbox ORDER BY created_at`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	items := []Outbox{}
	for rows.Next() {
		var value Outbox
		if rows.Scan(&value.ID, &value.DedupKey, &value.EventType, &value.AggregateID, &value.Payload, &value.CreatedAt) == nil {
			items = append(items, value)
		}
	}
	return items
}
