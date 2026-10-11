package importer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"campaign-platform/internal/storage"
)

type SourceDeletionWork struct {
	ImportID        string   `json:"importId"`
	ObjectKey       string   `json:"objectKey"`
	UploadSessionID string   `json:"uploadSessionId,omitempty"`
	ObjectKeys      []string `json:"-"`
	LeaseOwner      string   `json:"leaseOwner"`
	LeaseVersion    int64    `json:"leaseVersion"`
}

type SourceRetentionRepository interface {
	ClaimSourcesForDeletion(context.Context, string, time.Time, time.Duration, int) ([]SourceDeletionWork, error)
	CompleteSourceDeletion(context.Context, SourceDeletionWork, time.Time) error
	FailSourceDeletion(context.Context, SourceDeletionWork, string, time.Time) error
}

type SourceRetentionWorker struct {
	Repository    SourceRetentionRepository
	Objects       storage.ObjectStore
	WorkerID      string
	LeaseDuration time.Duration
	PollInterval  time.Duration
	BatchSize     int
	active        atomic.Int64
}

func (w *SourceRetentionWorker) Active() int64 {
	if w == nil {
		return 0
	}
	return w.active.Load()
}

func (w *SourceRetentionWorker) Run(ctx context.Context) error {
	if w == nil || w.Repository == nil || w.Objects == nil || strings.TrimSpace(w.WorkerID) == "" {
		return errors.New("source retention worker dependencies are required")
	}
	poll := w.PollInterval
	if poll <= 0 {
		poll = time.Minute
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		processed, err := w.Process(ctx)
		if err != nil && ctx.Err() == nil {
			return err
		}
		if processed > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *SourceRetentionWorker) Process(ctx context.Context) (int, error) {
	if w == nil || w.Repository == nil || w.Objects == nil {
		return 0, errors.New("source retention worker dependencies are required")
	}
	lease := w.LeaseDuration
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	limit := w.BatchSize
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	now := time.Now().UTC()
	items, err := w.Repository.ClaimSourcesForDeletion(ctx, w.WorkerID, now, lease, limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	var failures []error
	for _, item := range items {
		keys := append([]string(nil), item.ObjectKeys...)
		if len(keys) == 0 {
			keys = []string{item.ObjectKey}
		}
		w.active.Add(1)
		var deleteErr error
		for _, key := range keys {
			if err := w.Objects.Delete(ctx, key); err != nil {
				deleteErr = fmt.Errorf("delete source object %s: %w", hashObjectKey(key), err)
				break
			}
		}
		w.active.Add(-1)
		if deleteErr != nil {
			failureErr := w.Repository.FailSourceDeletion(ctx, item, truncate(deleteErr.Error(), 1000), time.Now().UTC())
			if failureErr != nil {
				failures = append(failures, fmt.Errorf("record source deletion failure for import %s: %w", item.ImportID, failureErr))
			}
			continue
		}
		if err := w.Repository.CompleteSourceDeletion(ctx, item, time.Now().UTC()); err != nil {
			failures = append(failures, fmt.Errorf("complete source deletion for import %s: %w", item.ImportID, err))
			continue
		}
		processed++
	}
	return processed, errors.Join(failures...)
}

type PostgreSQLSourceRetentionRepository struct{ DB *sql.DB }

func (r *PostgreSQLSourceRetentionRepository) ClaimSourcesForDeletion(ctx context.Context, worker string, now time.Time, lease time.Duration, limit int) ([]SourceDeletionWork, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id::text,object_key,coalesce(upload_session_id::text,''),coalesce(source_deletion_lease_owner,''),version FROM audience_imports WHERE source_deleted_at IS NULL AND source_expires_at<=$1 AND status IN ('COMPLETED','COMPLETED_WITH_EXCEPTIONS','ROLLED_BACK','REJECTED','FAILED','CANCELLED') AND (source_deletion_lease_expires_at IS NULL OR source_deletion_lease_expires_at<$1) ORDER BY source_expires_at,id FOR UPDATE SKIP LOCKED LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	items := []SourceDeletionWork{}
	for rows.Next() {
		var v SourceDeletionWork
		if err := rows.Scan(&v.ImportID, &v.ObjectKey, &v.UploadSessionID, &v.LeaseOwner, &v.LeaseVersion); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, v)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		if strings.TrimSpace(items[i].UploadSessionID) != "" {
			partRows, partErr := tx.QueryContext(ctx, `SELECT object_key FROM audience_import_upload_parts WHERE session_id=$1::uuid AND state='UPLOADED' ORDER BY part_number`, items[i].UploadSessionID)
			if partErr != nil {
				return nil, partErr
			}
			for partRows.Next() {
				var key string
				if err := partRows.Scan(&key); err != nil {
					partRows.Close()
					return nil, err
				}
				items[i].ObjectKeys = append(items[i].ObjectKeys, key)
			}
			if err := partRows.Close(); err != nil {
				return nil, err
			}
			if err := partRows.Err(); err != nil {
				return nil, err
			}
			if len(items[i].ObjectKeys) == 0 {
				return nil, fmt.Errorf("resumable import %s has no uploaded source parts", items[i].ImportID)
			}
		} else {
			items[i].ObjectKeys = []string{items[i].ObjectKey}
		}
		res, execErr := tx.ExecContext(ctx, `UPDATE audience_imports SET source_deletion_lease_owner=$2,source_deletion_lease_expires_at=$3,source_deletion_attempts=source_deletion_attempts+1,source_deletion_last_error=NULL,version=version+1,updated_at=$1 WHERE id=$4::uuid AND version=$5`, now, worker, now.Add(lease), items[i].ImportID, items[i].LeaseVersion)
		if execErr != nil {
			return nil, execErr
		}
		n, rowsErr := res.RowsAffected()
		if rowsErr != nil {
			return nil, rowsErr
		}
		if n != 1 {
			return nil, ErrImportConflict
		}
		items[i].LeaseOwner = worker
		items[i].LeaseVersion++
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *PostgreSQLSourceRetentionRepository) CompleteSourceDeletion(ctx context.Context, v SourceDeletionWork, now time.Time) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var attempts int
	res, err := tx.ExecContext(ctx, `UPDATE audience_imports SET source_deleted_at=$1,source_deletion_lease_owner=NULL,source_deletion_lease_expires_at=NULL,source_deletion_last_error=NULL,version=version+1,updated_at=$1 WHERE id=$2::uuid AND version=$3 AND source_deletion_lease_owner=$4`, now, v.ImportID, v.LeaseVersion, v.LeaseOwner)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrImportConflict
	}
	if err = tx.QueryRowContext(ctx, `SELECT source_deletion_attempts FROM audience_imports WHERE id=$1::uuid`, v.ImportID).Scan(&attempts); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audience_import_source_deletion_events(audience_import_id,event_type,worker_id,object_key_hash,attempt,occurred_at) VALUES($1::uuid,'SOURCE_DELETION_COMPLETED',$2,$3,$4,$5)`, v.ImportID, v.LeaseOwner, hashObjectKey(v.ObjectKey), attempts, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PostgreSQLSourceRetentionRepository) FailSourceDeletion(ctx context.Context, v SourceDeletionWork, detail string, now time.Time) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE audience_imports SET source_deletion_lease_owner=NULL,source_deletion_lease_expires_at=NULL,source_deletion_last_error=$1,updated_at=$2,version=version+1 WHERE id=$3::uuid AND version=$4 AND source_deletion_lease_owner=$5`, detail, now, v.ImportID, v.LeaseVersion, v.LeaseOwner)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrImportConflict
	}
	var attempts int
	if err = tx.QueryRowContext(ctx, `SELECT source_deletion_attempts FROM audience_imports WHERE id=$1::uuid`, v.ImportID).Scan(&attempts); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audience_import_source_deletion_events(audience_import_id,event_type,worker_id,object_key_hash,attempt,failure_detail,occurred_at) VALUES($1::uuid,'SOURCE_DELETION_FAILED',$2,$3,$4,$5,$6)`, v.ImportID, v.LeaseOwner, hashObjectKey(v.ObjectKey), attempts, detail, now); err != nil {
		return err
	}
	return tx.Commit()
}

func hashObjectKey(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}
