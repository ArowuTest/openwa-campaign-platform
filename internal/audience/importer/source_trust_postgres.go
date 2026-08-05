package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type PostgreSQLSourceTrustRepository struct{ DB *sql.DB }

func (r *PostgreSQLSourceTrustRepository) List(ctx context.Context, organisationID string) ([]SourceTrustPolicy, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT organisation_id::text,source_system,trust_level,reason,updated_by::text,version,updated_at FROM audience_source_trust_policies WHERE organisation_id=$1::uuid ORDER BY source_system`, organisationID)
	if err != nil {
		return nil, fmt.Errorf("list source trust policies: %w", err)
	}
	defer rows.Close()
	out := []SourceTrustPolicy{}
	for rows.Next() {
		var v SourceTrustPolicy
		if err := rows.Scan(&v.OrganisationID, &v.SourceSystem, &v.TrustLevel, &v.Reason, &v.UpdatedBy, &v.Version, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgreSQLSourceTrustRepository) Upsert(ctx context.Context, input SourceTrustPolicy, expectedVersion int64) (SourceTrustPolicy, error) {
	if r == nil || r.DB == nil {
		return SourceTrustPolicy{}, errors.New("database is required")
	}
	if expectedVersion == 0 {
		err := r.DB.QueryRowContext(ctx, `INSERT INTO audience_source_trust_policies(organisation_id,source_system,trust_level,reason,updated_by,version,updated_at) VALUES($1::uuid,$2,$3,$4,$5::uuid,1,$6) ON CONFLICT(organisation_id,source_system) DO NOTHING RETURNING version`, input.OrganisationID, input.SourceSystem, input.TrustLevel, input.Reason, input.UpdatedBy, input.UpdatedAt).Scan(&input.Version)
		if errors.Is(err, sql.ErrNoRows) {
			return SourceTrustPolicy{}, ErrSourceTrustVersion
		}
		if err != nil {
			return SourceTrustPolicy{}, err
		}
		return input, nil
	}
	err := r.DB.QueryRowContext(ctx, `UPDATE audience_source_trust_policies SET trust_level=$3,reason=$4,updated_by=$5::uuid,updated_at=$6,version=version+1 WHERE organisation_id=$1::uuid AND source_system=$2 AND version=$7 RETURNING version`, input.OrganisationID, input.SourceSystem, input.TrustLevel, input.Reason, input.UpdatedBy, input.UpdatedAt, expectedVersion).Scan(&input.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceTrustPolicy{}, ErrSourceTrustVersion
	}
	if err != nil {
		return SourceTrustPolicy{}, err
	}
	return input, nil
}
