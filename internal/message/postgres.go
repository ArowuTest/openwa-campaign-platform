package message

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	pgretry "campaign-platform/internal/persistence/postgres"
)

type PostgreSQLRepository struct{ DB *sql.DB }

func (r *PostgreSQLRepository) CreateDraft(ctx context.Context, input Input, now time.Time) (Version, error) {
	if r == nil || r.DB == nil {
		return Version{}, errors.New("database is required")
	}
	return pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (Version, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return Version{}, err
		}
		defer tx.Rollback()
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT true FROM campaigns WHERE id=$1::uuid FOR UPDATE`, input.CampaignID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Version{}, ErrCampaignMismatch
			}
			return Version{}, err
		}

		existing, err := scanVersion(tx.QueryRowContext(ctx, messageSelect+` WHERE campaign_id=$1::uuid AND client_request_id=$2`, input.CampaignID, input.IdempotencyKey))
		if err == nil {
			input.Version = existing.Version
			candidate, validationErr := NewDraft(input, existing.CreatedAt)
			if validationErr != nil {
				return Version{}, validationErr
			}
			if existing.ContentHash != candidate.ContentHash || existing.CreatedBy != strings.TrimSpace(input.CreatedBy) {
				return Version{}, ErrIdempotencyConflict
			}
			if err := tx.Commit(); err != nil {
				return Version{}, err
			}
			return existing, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Version{}, err
		}

		if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(version),0)+1 FROM message_versions WHERE campaign_id=$1::uuid`, input.CampaignID).Scan(&input.Version); err != nil {
			return Version{}, err
		}
		value, err := NewDraft(input, now)
		if err != nil {
			return Version{}, err
		}
		var duplicate bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM message_versions WHERE campaign_id=$1::uuid AND content_hash=$2)`, value.CampaignID, value.ContentHash).Scan(&duplicate)
		if err != nil {
			return Version{}, err
		}
		if duplicate {
			return Version{}, ErrDuplicateContent
		}
		links, err := json.Marshal(value.Links)
		if err != nil {
			return Version{}, err
		}
		variables, err := json.Marshal(value.Variables)
		if err != nil {
			return Version{}, err
		}
		var assetID, objectKey, mediaSHA, mediaType, scanStatus any
		var mediaSize any
		if value.Media != nil {
			assetID, objectKey, mediaSHA, mediaType, mediaSize, scanStatus = value.Media.AssetID, value.Media.ObjectKey, value.Media.SHA256, value.Media.MediaType, value.Media.Size, value.Media.ScanStatus
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO message_versions(
 id,campaign_id,version,message_type,body,media_asset_id,media_object_key,media_sha256,media_type,media_size,media_scan_status,
 destination_links,variables,content_hash,client_request_id,status,created_by,created_at
) VALUES($1::uuid,$2::uuid,$3,$4,NULLIF($5,''),NULLIF($6,'')::uuid,$7,$8,$9,$10,$11,$12::jsonb,$13::jsonb,$14,$15,'DRAFT',NULLIF($16,'')::uuid,$17)`,
			value.ID, value.CampaignID, value.Version, value.Type, value.Body, assetID, objectKey, mediaSHA, mediaType, mediaSize, scanStatus, links, variables, value.ContentHash, value.IdempotencyKey, value.CreatedBy, value.CreatedAt)
		if err != nil {
			return Version{}, fmt.Errorf("insert message version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return Version{}, err
		}
		return value, nil
	})
}

func (r *PostgreSQLRepository) Get(ctx context.Context, identifier string) (Version, error) {
	if r == nil || r.DB == nil {
		return Version{}, errors.New("database is required")
	}
	value, err := scanVersion(r.DB.QueryRowContext(ctx, messageSelect+` WHERE id=$1::uuid`, identifier))
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, ErrNotFound
	}
	return value, err
}

func (r *PostgreSQLRepository) ListByCampaign(ctx context.Context, campaignID string) ([]Version, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := r.DB.QueryContext(ctx, messageSelect+` WHERE campaign_id=$1::uuid ORDER BY version DESC LIMIT 1000`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Version{}
	for rows.Next() {
		value, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) ListByCampaignPage(ctx context.Context, campaignID string, limit int, beforeVersion int) ([]Version, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := r.DB.QueryContext(ctx, messageSelect+` WHERE campaign_id=$1::uuid AND ($3=0 OR version<$3) ORDER BY version DESC LIMIT $2`, campaignID, limit, beforeVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Version{}
	for rows.Next() {
		value, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) Approve(ctx context.Context, identifier, actorID, expectedContentHash string, now time.Time) (Version, error) {
	if r == nil || r.DB == nil {
		return Version{}, errors.New("database is required")
	}
	return pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (Version, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return Version{}, err
		}
		defer tx.Rollback()
		current, err := scanVersion(tx.QueryRowContext(ctx, messageSelect+` WHERE id=$1::uuid FOR UPDATE`, identifier))
		if errors.Is(err, sql.ErrNoRows) {
			return Version{}, ErrNotFound
		}
		if err != nil {
			return Version{}, err
		}
		if current.ContentHash != expectedContentHash {
			return Version{}, ErrApprovalStale
		}
		if current.Status == StatusApproved {
			if current.ApprovedBy != actorID {
				return Version{}, ErrConflict
			}
			if err := tx.Commit(); err != nil {
				return Version{}, err
			}
			return current, nil
		}
		approved, err := current.Approve(actorID, now)
		if err != nil {
			return Version{}, err
		}
		result, err := tx.ExecContext(ctx, `UPDATE message_versions SET status='APPROVED',approved_by=NULLIF($2,'')::uuid,approved_at=$3 WHERE id=$1::uuid AND status='DRAFT' AND content_hash=$4`, identifier, actorID, approved.ApprovedAt, expectedContentHash)
		if err != nil {
			return Version{}, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return Version{}, err
		}
		if count != 1 {
			return Version{}, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return Version{}, err
		}
		return approved, nil
	})
}

const messageSelect = `SELECT id::text,campaign_id::text,version,message_type,coalesce(body,''),coalesce(media_asset_id::text,''),coalesce(media_object_key,''),coalesce(media_sha256,''),coalesce(media_type,''),media_size,coalesce(media_scan_status,''),destination_links,variables,content_hash,coalesce(client_request_id,''),status,coalesce(created_by::text,''),coalesce(approved_by::text,''),created_at,approved_at FROM message_versions`

type messageScanner interface{ Scan(...any) error }

func scanVersion(row messageScanner) (Version, error) {
	var value Version
	var typ, status string
	var assetID, objectKey, mediaSHA, mediaType, scanStatus string
	var mediaSize sql.NullInt64
	var links, variables []byte
	var approvedAt sql.NullTime
	err := row.Scan(&value.ID, &value.CampaignID, &value.Version, &typ, &value.Body, &assetID, &objectKey, &mediaSHA, &mediaType, &mediaSize, &scanStatus, &links, &variables, &value.ContentHash, &value.IdempotencyKey, &status, &value.CreatedBy, &value.ApprovedBy, &value.CreatedAt, &approvedAt)
	if err != nil {
		return Version{}, err
	}
	value.Type = Type(typ)
	value.Status = Status(status)
	if objectKey != "" {
		value.Media = &Media{AssetID: assetID, ObjectKey: objectKey, SHA256: mediaSHA, MediaType: mediaType, ScanStatus: scanStatus}
		if mediaSize.Valid {
			value.Media.Size = mediaSize.Int64
		}
	}
	if len(links) > 0 {
		if err := json.Unmarshal(links, &value.Links); err != nil {
			return Version{}, err
		}
	}
	if len(variables) > 0 {
		if err := json.Unmarshal(variables, &value.Variables); err != nil {
			return Version{}, err
		}
	}
	if approvedAt.Valid {
		v := approvedAt.Time
		value.ApprovedAt = &v
	}
	return value, nil
}
