package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"campaign-platform/internal/campaign"
	postgresrepo "campaign-platform/internal/persistence/postgres"
)

// PostgreSQLLifecycleCommitter makes the campaign CAS, reservation mutation,
// and version-linked lifecycle event one serializable PostgreSQL transaction.
type PostgreSQLLifecycleCommitter struct {
	DB    *sql.DB
	Retry postgresrepo.RetryPolicy
}

func (c *PostgreSQLLifecycleCommitter) Commit(
	ctx context.Context,
	value LifecycleCommit,
) (campaign.Campaign, error) {
	if err := value.Validate(); err != nil {
		return campaign.Campaign{}, err
	}
	if c == nil || c.DB == nil {
		return campaign.Campaign{}, errors.New("postgres lifecycle database is required")
	}
	// Reject unserialisable details before opening a transaction. The store
	// marshals again when inserting so it remains the single SQL event writer.
	if _, err := json.Marshal(value.Details); err != nil {
		return campaign.Campaign{}, err
	}
	return postgresrepo.RetryValue(ctx, c.Retry, func() (campaign.Campaign, error) {
		return c.commitOnce(ctx, value)
	})
}

func (c *PostgreSQLLifecycleCommitter) commitOnce(
	ctx context.Context,
	value LifecycleCommit,
) (campaign.Campaign, error) {
	tx, err := c.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return campaign.Campaign{}, err
	}
	defer func() { _ = tx.Rollback() }()

	campaigns := &postgresrepo.CampaignRepository{DB: c.DB}
	if err = campaigns.CompareAndSwapInTx(
		ctx, tx, value.Campaign, value.ExpectedVersion,
	); err != nil {
		return campaign.Campaign{}, err
	}
	if value.ExecutionLease != nil {
		var currentFence int64
		err = tx.QueryRowContext(ctx, `
SELECT fence_token
FROM campaign_execution_leases
WHERE campaign_id=$1::uuid
  AND owner=$2
  AND fence_token=$3
  AND expires_at>clock_timestamp()
FOR UPDATE
`, value.Campaign.ID, value.ExecutionLease.Owner,
			value.ExecutionLease.FenceToken,
		).Scan(&currentFence)
		if errors.Is(err, sql.ErrNoRows) {
			return campaign.Campaign{}, ErrExecutionLeaseConflict
		}
		if err != nil {
			return campaign.Campaign{}, err
		}
	}
	if value.Action == campaign.ActionCancel {
		if err = cancelUndispatchedRecipientsInTx(ctx, tx, value); err != nil {
			return campaign.Campaign{}, err
		}
	}
	routing := &PostgreSQLRoutingPlanStore{DB: c.DB}
	switch value.ReservationOperation {
	case ReservationValidateHeld:
		err = validateHeldRoutingReservations(
			ctx, tx, value.RoutingPlanID, value.Campaign.ID,
		)
	case ReservationActivate:
		err = routing.ActivateReservationsInTx(
			ctx, tx, value.RoutingPlanID, value.Campaign.ID, value.OccurredAt,
		)
	case ReservationRelease:
		err = routing.ReleaseReservationsInTx(
			ctx, tx, value.RoutingPlanID, value.Campaign.ID,
			value.ActorID, value.OccurredAt,
		)
	case ReservationNone:
		// No reservation mutation is required for this lifecycle action.
	}
	if err != nil {
		return campaign.Campaign{}, err
	}
	events := &PostgreSQLStore{DB: c.DB}
	if err = events.RecordLifecycleEventInTx(ctx, tx, value); err != nil {
		return campaign.Campaign{}, err
	}
	if err = tx.Commit(); err != nil {
		return campaign.Campaign{}, err
	}
	return value.Campaign, nil
}

func cancelUndispatchedRecipientsInTx(ctx context.Context, tx *sql.Tx, value LifecycleCommit) error {
	var authorised, queued, failed int64
	err := tx.QueryRowContext(ctx, `
WITH targets AS MATERIALIZED (
 SELECT id,status
 FROM campaign_recipients
 WHERE campaign_id=$1::uuid
   AND status IN ('AUTHORISED','QUEUED','CLAIMED','FAILED_RETRYABLE')
 FOR UPDATE
), cancelled AS (
 UPDATE campaign_recipients cr
 SET status='CANCELLED',
     version=cr.version+1,
     completed_at=COALESCE(cr.completed_at,$2),
     last_event_at=$2,
     last_error_code='CAMPAIGN_CANCELLED',
     last_error_detail=NULLIF($3,''),
     reconciliation_required=false,
     updated_at=$2
 FROM targets t
 WHERE cr.id=t.id
 RETURNING cr.id
), cancellation_events AS (
 INSERT INTO delivery_events(
   campaign_recipient_id,event_type,occurred_at,received_at,payload,
   event_deduplication_key,event_fingerprint
 )
 SELECT c.id,'message.cancelled',$2,$2,
        jsonb_build_object(
          'campaignId',$1::text,
          'actorId',$4::text,
          'reason',$3::text,
          'source','campaign_lifecycle'
        ),
        'campaign-cancel:' || $1::text || ':' || c.id::text || ':' || $5::bigint::text,
        encode(digest(
          'campaign-cancel:' || $1::text || ':' || c.id::text || ':' || $5::bigint::text || '|message.cancelled',
          'sha256'
        ),'hex')
 FROM cancelled c
 ON CONFLICT (event_deduplication_key) WHERE event_deduplication_key IS NOT NULL DO NOTHING
 RETURNING campaign_recipient_id
), cancelled_jobs AS (
 UPDATE durable_jobs j
 SET status='CANCELLED',updated_at=$2
 WHERE j.job_type='DISPATCH_CAMPAIGN_RECIPIENT'
   AND j.status='PENDING'
   AND EXISTS (
     SELECT 1 FROM cancelled c
     WHERE c.id::text=j.payload->>'campaignRecipientId'
   )
 RETURNING j.id
)
SELECT
 count(*) FILTER (WHERE status='AUTHORISED'),
 count(*) FILTER (WHERE status IN ('QUEUED','CLAIMED')),
 count(*) FILTER (WHERE status='FAILED_RETRYABLE')
FROM targets`, value.Campaign.ID, value.OccurredAt.UTC(), value.Reason, value.ActorID, value.Campaign.Version).Scan(&authorised, &queued, &failed)
	if err != nil {
		return err
	}
	if authorised == 0 && queued == 0 && failed == 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx, `
UPDATE campaign_metrics
SET authorised_total=GREATEST(authorised_total-$2,0),
    queued_total=GREATEST(queued_total-$3,0),
    failed_total=GREATEST(failed_total-$4,0),
    updated_at=$5
WHERE campaign_id=$1::uuid`, value.Campaign.ID, authorised, queued, failed, value.OccurredAt.UTC())
	return err
}
