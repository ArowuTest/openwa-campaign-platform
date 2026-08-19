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
	routing := &PostgreSQLRoutingPlanStore{DB: c.DB}
	switch value.ReservationOperation {
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
