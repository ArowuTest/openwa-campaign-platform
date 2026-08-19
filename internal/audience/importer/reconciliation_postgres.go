package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type PostgreSQLReconciliationRepository struct{ DB *sql.DB }

func (r *PostgreSQLReconciliationRepository) Ensure(ctx context.Context, record ReconciliationRecord) (ReconciliationRecord, bool, error) {
	if r == nil || r.DB == nil {
		return ReconciliationRecord{}, false, errors.New("database is required")
	}
	evidence, err := json.Marshal(record.Evidence)
	if err != nil {
		return ReconciliationRecord{}, false, err
	}
	var insertedID string
	err = r.DB.QueryRowContext(ctx, `INSERT INTO audience_import_reconciliations(id,audience_import_id,evidence,evidence_hash,reason,performed_by,created_at) VALUES($1::uuid,$2::uuid,$3::jsonb,$4,$5,$6::uuid,$7) ON CONFLICT(audience_import_id) DO NOTHING RETURNING id::text`, record.ID, record.ImportID, string(evidence), record.EvidenceHash, record.Reason, record.PerformedBy, record.CreatedAt).Scan(&insertedID)
	if errors.Is(err, sql.ErrNoRows) {
		existing, loadErr := r.GetByImport(ctx, record.ImportID)
		if loadErr != nil {
			return ReconciliationRecord{}, false, loadErr
		}
		if existing.EvidenceHash != record.EvidenceHash {
			return ReconciliationRecord{}, false, ErrReconciliationConflict
		}
		return existing, false, nil
	}
	if err != nil {
		return ReconciliationRecord{}, false, fmt.Errorf("store audience import reconciliation: %w", err)
	}
	return record, true, nil
}

func (r *PostgreSQLReconciliationRepository) GetByImport(ctx context.Context, importID string) (ReconciliationRecord, error) {
	if r == nil || r.DB == nil {
		return ReconciliationRecord{}, errors.New("database is required")
	}
	var value ReconciliationRecord
	var evidence []byte
	err := r.DB.QueryRowContext(ctx, `SELECT id::text,audience_import_id::text,evidence,evidence_hash,reason,performed_by::text,created_at FROM audience_import_reconciliations WHERE audience_import_id=$1::uuid`, importID).Scan(&value.ID, &value.ImportID, &evidence, &value.EvidenceHash, &value.Reason, &value.PerformedBy, &value.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ReconciliationRecord{}, ErrImportNotFound
	}
	if err != nil {
		return ReconciliationRecord{}, err
	}
	if err := json.Unmarshal(evidence, &value.Evidence); err != nil {
		return ReconciliationRecord{}, err
	}
	return value, nil
}
