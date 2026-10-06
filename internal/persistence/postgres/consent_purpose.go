package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"campaign-platform/internal/consent"
)

type ConsentPurposeRepository struct{ DB *sql.DB }

func (r *ConsentPurposeRepository) ListPurposePage(ctx context.Context, organisationID, reviewID string, activeOnly bool, limit int, before *time.Time, beforeID string) ([]consent.Purpose, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, `
SELECT
  cp.id::text,
  coalesce(cp.organisation_id::text,''),
  coalesce(cp.consent_review_id::text,''),
  cp.code,
  cp.name,
  coalesce(cp.description,''),
  upper(cp.channel),
  cp.wording_version,
  coalesce(cp.permitted_message_category,''),
  cp.expires_after_days,
  cp.active,
  cp.created_at,
  cp.updated_at
FROM consent_purposes cp
WHERE ($1::text='' OR cp.organisation_id=NULLIF($1::text,'')::uuid)
  AND ($2::text='' OR cp.consent_review_id=NULLIF($2::text,'')::uuid)
  AND (NOT $3::boolean OR cp.active)
  AND ($5::timestamptz IS NULL OR cp.created_at<$5 OR (cp.created_at=$5 AND cp.id<NULLIF($6::text,'')::uuid))
ORDER BY cp.created_at DESC,cp.id DESC
LIMIT $4`, strings.TrimSpace(organisationID), strings.TrimSpace(reviewID), activeOnly, limit, before, strings.TrimSpace(beforeID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]consent.Purpose, 0, limit)
	for rows.Next() {
		var value consent.Purpose
		var expires sql.NullInt64
		if err := rows.Scan(
			&value.ID, &value.OrganisationID, &value.ConsentReviewID, &value.Code, &value.Name,
			&value.Description, &value.Channel, &value.WordingVersion, &value.PermittedMessageCategory,
			&expires, &value.Active, &value.CreatedAt, &value.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if expires.Valid {
			days := int(expires.Int64)
			value.ExpiresAfterDays = &days
		}
		items = append(items, value)
	}
	return items, rows.Err()
}
