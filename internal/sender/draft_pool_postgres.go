package sender

import (
	"context"
	"database/sql"
	"errors"
)

func (p *PostgreSQLGovernanceStore) GetPool(ctx context.Context, identifier string) (Pool, error) {
	if p == nil || p.DB == nil {
		return Pool{}, errors.New("database is required")
	}
	var v Pool
	err := p.DB.QueryRowContext(ctx, "SELECT "+poolColumns+" FROM sender_pools WHERE id=$1::uuid", identifier).Scan(&v.ID, &v.Name, &v.OrganisationID, &v.Status, &v.MaxMessagesPerMinute, &v.DailyCapacity, &v.ReservedCapacity, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Pool{}, ErrSenderNotFound
	}
	if err != nil {
		return Pool{}, err
	}
	return v, nil
}
