package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type PostgreSQLAssetRepository struct{ DB *sql.DB }

type assetRowScanner interface{ Scan(...any) error }

func scanAsset(row assetRowScanner) (Asset, error) {
	var asset Asset
	var purpose, status string
	err := row.Scan(&asset.ID, &purpose, &asset.ObjectKey, &asset.OriginalFilename, &asset.MediaType, &asset.Size, &asset.SHA256, &status, &asset.ScanSignature, &asset.CreatedBy, &asset.CreatedAt, &asset.UpdatedAt, &asset.Version)
	asset.Purpose = AssetPurpose(purpose)
	asset.Status = AssetStatus(status)
	return asset, err
}

const assetSelect = `SELECT id::text,purpose,object_key,original_filename,media_type,byte_size,sha256_checksum,status,coalesce(scan_signature,''),created_by::text,created_at,updated_at,version FROM trusted_assets`

func (r *PostgreSQLAssetRepository) Create(ctx context.Context, asset Asset) (Asset, error) {
	if r == nil || r.DB == nil {
		return Asset{}, errors.New("database is required")
	}
	return scanAsset(r.DB.QueryRowContext(ctx, `
WITH created AS (
  INSERT INTO trusted_assets(
    id,purpose,object_key,original_filename,media_type,byte_size,sha256_checksum,status,
    scan_signature,created_by,created_at,updated_at,version
  ) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10::uuid,$11,$11,1)
  RETURNING *
), event AS (
  INSERT INTO trusted_asset_events(asset_id,event_type,actor_id,reason,asset_version,evidence)
  SELECT id,'UPLOADED',created_by,'trusted asset uploaded',version,
         jsonb_build_object('purpose',purpose,'sha256',sha256_checksum,'byteSize',byte_size)
  FROM created
)
SELECT id::text,purpose,object_key,original_filename,media_type,byte_size,sha256_checksum,
       status,coalesce(scan_signature,''),created_by::text,created_at,updated_at,version
FROM created`, asset.ID, asset.Purpose, asset.ObjectKey, asset.OriginalFilename, asset.MediaType,
		asset.Size, asset.SHA256, asset.Status, asset.ScanSignature, asset.CreatedBy, asset.CreatedAt))
}

func (r *PostgreSQLAssetRepository) Get(ctx context.Context, id string) (Asset, error) {
	if r == nil || r.DB == nil {
		return Asset{}, errors.New("database is required")
	}
	asset, err := scanAsset(r.DB.QueryRowContext(ctx, assetSelect+` WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Asset{}, ErrAssetNotFound
	}
	return asset, err
}

func (r *PostgreSQLAssetRepository) UpdateScan(ctx context.Context, id string, expected int64, status AssetStatus, signature string, now time.Time) (Asset, error) {
	if r == nil || r.DB == nil {
		return Asset{}, errors.New("database is required")
	}
	eventType := map[AssetStatus]string{
		AssetStatusClean:      "SCAN_CLEAN",
		AssetStatusInfected:   "SCAN_INFECTED",
		AssetStatusScanFailed: "SCAN_FAILED",
	}[status]
	if eventType == "" {
		return Asset{}, errors.New("invalid trusted asset scan transition")
	}
	asset, err := scanAsset(r.DB.QueryRowContext(ctx, `
WITH updated AS (
  UPDATE trusted_assets SET status=$3,scan_signature=NULLIF($4,''),updated_at=$5,version=version+1
  WHERE id=$1::uuid AND version=$2 AND status='SCANNING'
  RETURNING *
), event AS (
  INSERT INTO trusted_asset_events(asset_id,event_type,actor_id,reason,asset_version,evidence)
  SELECT id,$6,created_by,$7,version,jsonb_build_object('signature',nullif($4,'')) FROM updated
)
SELECT id::text,purpose,object_key,original_filename,media_type,byte_size,sha256_checksum,
       status,coalesce(scan_signature,''),created_by::text,created_at,updated_at,version
FROM updated`, id, expected, status, signature, now, eventType, "trusted asset malware scan completed"))
	if errors.Is(err, sql.ErrNoRows) {
		return Asset{}, ErrAssetConflict
	}
	if err != nil {
		return Asset{}, fmt.Errorf("update trusted asset scan: %w", err)
	}
	return asset, nil
}

func (r *PostgreSQLAssetRepository) Revoke(ctx context.Context, id string, expected int64, actor, reason string, now time.Time) (Asset, error) {
	if r == nil || r.DB == nil {
		return Asset{}, errors.New("database is required")
	}
	asset, err := scanAsset(r.DB.QueryRowContext(ctx, `
WITH existing AS (
  SELECT id,status FROM trusted_assets WHERE id=$1::uuid AND version=$2 AND status<>'REVOKED' FOR UPDATE
), updated AS (
  UPDATE trusted_assets a SET status='REVOKED',updated_at=$5,version=a.version+1
  FROM existing e WHERE a.id=e.id
  RETURNING a.*,e.status AS previous_status
), event AS (
  INSERT INTO trusted_asset_events(asset_id,event_type,actor_id,reason,asset_version,evidence)
  SELECT id,'REVOKED',$3::uuid,$4,version,jsonb_build_object('previousStatus',previous_status) FROM updated
)
SELECT id::text,purpose,object_key,original_filename,media_type,byte_size,sha256_checksum,
       status,coalesce(scan_signature,''),created_by::text,created_at,updated_at,version
FROM updated`, id, expected, actor, reason, now))
	if errors.Is(err, sql.ErrNoRows) {
		return Asset{}, ErrAssetConflict
	}
	return asset, err
}
