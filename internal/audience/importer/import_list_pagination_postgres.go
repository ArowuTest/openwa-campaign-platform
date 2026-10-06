package importer

import (
	"context"
	"errors"
	"strings"
	"time"
)

func (r *PostgreSQLImportRepository) ListImportPage(
	ctx context.Context,
	organisationID string,
	limit int,
	before *time.Time,
	beforeID string,
) ([]ImportBatch, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 501 {
		limit = 101
	}
	rows, err := r.DB.QueryContext(ctx, importSelect+`
WHERE ($1::text='' OR organisation_id=NULLIF($1::text,'')::uuid)
  AND (
    $3::timestamptz IS NULL
    OR created_at<$3
    OR (created_at=$3 AND id<NULLIF($4::text,'')::uuid)
  )
ORDER BY created_at DESC,id DESC
LIMIT $2`,
		strings.TrimSpace(organisationID), limit, before, strings.TrimSpace(beforeID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ImportBatch, 0, limit)
	for rows.Next() {
		value, err := scanImport(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, rows.Err()
}
