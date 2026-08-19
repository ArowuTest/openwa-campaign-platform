package execution

import (
	postgresrepo "campaign-platform/internal/persistence/postgres"
	"context"
	"database/sql"
	"errors"
	"time"
)

const dueClaimPredicate = `c.status='SCHEDULED' AND c.requested_start_at<=$3`
const activeClaimPredicate = `c.status IN('DISPATCHING','PAUSED') AND $3::timestamptz IS NOT NULL`

func (s *PostgreSQLStore) ClaimDue(ctx context.Context, owner string, lease time.Duration, now time.Time, limit int) ([]CampaignLease, error) {
	return s.claim(ctx, owner, lease, now, limit, dueClaimPredicate)
}
func (s *PostgreSQLStore) ClaimActive(ctx context.Context, owner string, lease time.Duration, now time.Time, limit int) ([]CampaignLease, error) {
	return s.claim(ctx, owner, lease, now, limit, activeClaimPredicate)
}
func (s *PostgreSQLStore) claim(ctx context.Context, owner string, lease time.Duration, now time.Time, limit int, predicate string) ([]CampaignLease, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 {
		limit = 20
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := `SELECT c.id::text FROM campaigns c LEFT JOIN campaign_execution_leases l ON l.campaign_id=c.id WHERE (` + predicate + `) AND (l.campaign_id IS NULL OR l.expires_at<$2) ORDER BY coalesce(c.requested_start_at,c.created_at),c.id LIMIT $1 FOR UPDATE OF c SKIP LOCKED`
	rows, err := tx.QueryContext(ctx, q, limit, now, now)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	type leaseMetadata struct {
		id         string
		fenceToken int64
		expiresAt  time.Time
	}
	metadata := make([]leaseMetadata, 0, len(ids))
	for _, id := range ids {
		var claimed leaseMetadata
		claimed.id = id
		err = tx.QueryRowContext(ctx, `
INSERT INTO campaign_execution_leases(
	campaign_id,owner,fence_token,expires_at,updated_at
) VALUES($1::uuid,$2,1,$3,$4)
ON CONFLICT(campaign_id) DO UPDATE SET
	owner=EXCLUDED.owner,
	fence_token=campaign_execution_leases.fence_token+1,
	expires_at=EXCLUDED.expires_at,
	updated_at=EXCLUDED.updated_at
WHERE campaign_execution_leases.expires_at<$4
RETURNING fence_token,expires_at
`, id, owner, now.Add(lease), now).Scan(
			&claimed.fenceToken, &claimed.expiresAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		metadata = append(metadata, claimed)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	repo := &postgresrepo.CampaignRepository{DB: s.DB}
	items := make([]CampaignLease, 0, len(metadata))
	for _, claimed := range metadata {
		value, getErr := repo.Get(ctx, claimed.id)
		if getErr != nil {
			return nil, getErr
		}
		items = append(items, CampaignLease{
			Campaign: value, Owner: owner, FenceToken: claimed.fenceToken,
			ExpiresAt: claimed.expiresAt,
		})
	}
	return items, nil
}
func (s *PostgreSQLStore) Release(
	ctx context.Context, id, owner string, fenceToken int64, now time.Time,
) error {
	result, err := s.DB.ExecContext(ctx, `
UPDATE campaign_execution_leases
SET expires_at=$4,updated_at=$4
WHERE campaign_id=$1::uuid AND owner=$2 AND fence_token=$3
`, id, owner, fenceToken, now)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrExecutionLeaseConflict
	}
	return nil
}
