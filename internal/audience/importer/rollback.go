package importer

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"campaign-platform/internal/audit"
	"campaign-platform/internal/shared/id"
)

var (
	ErrRollbackNotAllowed = errors.New("audience import rollback is not allowed")
	ErrRollbackConsumed   = errors.New("audience import has already been consumed by a snapshot or campaign")
	ErrRollbackSuperseded = errors.New("audience import contact state has been superseded by later source data")
)

type RollbackResult struct {
	ImportID         string    `json:"importId"`
	RestoredContacts int64     `json:"restoredContacts"`
	DeletedContacts  int64     `json:"deletedContacts"`
	RevokedConsents  int64     `json:"revokedConsents"`
	RolledBackAt     time.Time `json:"rolledBackAt"`
	RolledBackBy     string    `json:"rolledBackBy"`
	Reason           string    `json:"reason"`
}

type RollbackRepository interface {
	Rollback(context.Context, string, int64, string, string, time.Time) (RollbackResult, error)
}

type RollbackService struct {
	Repository RollbackRepository
	Audit      *audit.Recorder
	Clock      func() time.Time
}

func (s *RollbackService) Rollback(ctx context.Context, importID string, expectedVersion int64, actor, reason, correlation string) (RollbackResult, error) {
	if s == nil || s.Repository == nil {
		return RollbackResult{}, errors.New("audience import rollback repository is required")
	}
	importID, actor, reason = strings.TrimSpace(importID), strings.TrimSpace(actor), strings.TrimSpace(reason)
	if importID == "" || expectedVersion <= 0 || actor == "" || len(reason) < 12 {
		return RollbackResult{}, ErrRollbackNotAllowed
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	result, err := s.Repository.Rollback(ctx, importID, expectedVersion, actor, reason, now)
	if err != nil {
		return result, err
	}
	if s.Audit != nil {
		_, err = s.Audit.Record(ctx, audit.Input{ActorType: "USER", ActorID: actor, Action: "AUDIENCE_IMPORT_ROLLED_BACK", ObjectType: "AUDIENCE_IMPORT", ObjectID: importID, After: result, Reason: reason, CorrelationID: correlation, OccurredAt: now})
	}
	return result, err
}

type PostgreSQLRollbackRepository struct{ DB *sql.DB }

func (r *PostgreSQLRollbackRepository) Rollback(ctx context.Context, importID string, expectedVersion int64, actor, reason string, now time.Time) (RollbackResult, error) {
	if r == nil || r.DB == nil {
		return RollbackResult{}, errors.New("database is required")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return RollbackResult{}, err
	}
	defer tx.Rollback()
	var status string
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT status,version FROM audience_imports WHERE id=$1::uuid FOR UPDATE`, importID).Scan(&status, &version); errors.Is(err, sql.ErrNoRows) {
		return RollbackResult{}, ErrImportNotFound
	} else if err != nil {
		return RollbackResult{}, err
	}
	if version != expectedVersion {
		return RollbackResult{}, ErrImportConflict
	}
	if status == string(ImportStatus("ROLLED_BACK")) {
		var result RollbackResult
		err = tx.QueryRowContext(ctx, `SELECT audience_import_id::text,restored_contacts,deleted_contacts,revoked_consents,occurred_at,actor_id::text,reason FROM audience_import_rollback_events WHERE audience_import_id=$1::uuid ORDER BY occurred_at DESC LIMIT 1`, importID).Scan(&result.ImportID, &result.RestoredContacts, &result.DeletedContacts, &result.RevokedConsents, &result.RolledBackAt, &result.RolledBackBy, &result.Reason)
		if err != nil {
			return result, err
		}
		if err = tx.Commit(); err != nil {
			return RollbackResult{}, err
		}
		return result, nil
	}
	if status != string(ImportCompleted) && status != string(ImportCompletedWithExceptions) {
		return RollbackResult{}, ErrRollbackNotAllowed
	}
	var mutationCount int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM audience_import_contact_mutations WHERE audience_import_id=$1::uuid`, importID).Scan(&mutationCount); err != nil {
		return RollbackResult{}, err
	}
	if mutationCount == 0 {
		return RollbackResult{}, ErrRollbackNotAllowed
	}
	var consumed bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM audience_import_contact_mutations m JOIN audience_snapshot_members sm ON sm.contact_id=m.contact_id WHERE m.audience_import_id=$1::uuid
 UNION ALL
 SELECT 1 FROM audience_import_contact_mutations m JOIN campaign_recipients cr ON cr.contact_id=m.contact_id WHERE m.audience_import_id=$1::uuid
)`, importID).Scan(&consumed)
	if err != nil {
		return RollbackResult{}, err
	}
	if consumed {
		return RollbackResult{}, ErrRollbackConsumed
	}
	var superseded bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM audience_import_contact_mutations m
 JOIN contact_profile_history h ON h.contact_id=m.contact_id AND h.audience_import_id<>m.audience_import_id AND h.recorded_at>m.recorded_at
 WHERE m.audience_import_id=$1::uuid
 UNION ALL
 SELECT 1 FROM audience_import_contact_mutations m
 JOIN contact_sources s ON s.contact_id=m.contact_id AND s.audience_import_id IS DISTINCT FROM m.audience_import_id AND s.first_seen_at>m.recorded_at
 WHERE m.audience_import_id=$1::uuid AND m.was_inserted
)`, importID).Scan(&superseded)
	if err != nil {
		return RollbackResult{}, err
	}
	if superseded {
		return RollbackResult{}, ErrRollbackSuperseded
	}

	var revoked int64
	rows, err := tx.QueryContext(ctx, `UPDATE consent_grants SET status='REVOKED',updated_at=$2,version=version+1 WHERE source_import_id=$1::uuid AND status IN ('ACTIVE','PENDING_VERIFICATION') RETURNING id,contact_id`, importID, now)
	if err != nil {
		return RollbackResult{}, err
	}
	for rows.Next() {
		var grantID, contactID string
		if err = rows.Scan(&grantID, &contactID); err != nil {
			rows.Close()
			return RollbackResult{}, err
		}
		eventID, idErr := id.New()
		if idErr != nil {
			rows.Close()
			return RollbackResult{}, idErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO consent_events(id,contact_id,grant_id,event_type,actor_id,reason,source_reference,occurred_at) VALUES($1::uuid,$2::uuid,$3::uuid,'IMPORT_ROLLBACK_REVOKED',$4::uuid,$5,$6,$7)`, eventID, contactID, grantID, actor, reason, "audience-import:"+importID, now); err != nil {
			rows.Close()
			return RollbackResult{}, err
		}
		revoked++
	}
	if err = rows.Close(); err != nil {
		return RollbackResult{}, err
	}
	if err = rows.Err(); err != nil {
		return RollbackResult{}, err
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM contact_sources WHERE audience_import_id=$1::uuid`, importID); err != nil {
		return RollbackResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM contact_profile_history WHERE audience_import_id=$1::uuid`, importID); err != nil {
		return RollbackResult{}, err
	}

	res, err := tx.ExecContext(ctx, `UPDATE contacts c SET
 encrypted_msisdn=m.before_encrypted_msisdn,masked_msisdn=m.before_masked_msisdn,country_id=m.before_country_id,
 state_id=m.before_state_id,lga_id=m.before_lga_id,reported_age=m.before_reported_age,age_recorded_at=m.before_age_recorded_at,
 age_source=m.before_age_source,age_verified=coalesce(m.before_age_verified,false),gender_code=m.before_gender_code,
 status=m.before_status,source_system=m.before_source_system,source_record_id=m.before_source_record_id,
 profile_recorded_at=m.before_profile_recorded_at,updated_at=$2
 FROM audience_import_contact_mutations m WHERE m.audience_import_id=$1::uuid AND NOT m.was_inserted AND c.id=m.contact_id`, importID, now)
	if err != nil {
		return RollbackResult{}, err
	}
	restored, err := res.RowsAffected()
	if err != nil {
		return RollbackResult{}, err
	}

	res, err = tx.ExecContext(ctx, `DELETE FROM contacts c USING audience_import_contact_mutations m
 WHERE m.audience_import_id=$1::uuid AND m.was_inserted AND c.id=m.contact_id
 AND NOT EXISTS(SELECT 1 FROM contact_sources s WHERE s.contact_id=c.id)
 AND NOT EXISTS(SELECT 1 FROM consent_grants g WHERE g.contact_id=c.id AND g.status IN ('ACTIVE','PENDING_VERIFICATION'))
 AND NOT EXISTS(SELECT 1 FROM suppressions s WHERE s.contact_id=c.id AND s.active)`, importID)
	if err != nil {
		return RollbackResult{}, err
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return RollbackResult{}, err
	}
	var remainingInserted int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM audience_import_contact_mutations m JOIN contacts c ON c.id=m.contact_id WHERE m.audience_import_id=$1::uuid AND m.was_inserted`, importID).Scan(&remainingInserted); err != nil {
		return RollbackResult{}, err
	}
	if remainingInserted != 0 {
		return RollbackResult{}, ErrRollbackSuperseded
	}

	result := RollbackResult{ImportID: importID, RestoredContacts: restored, DeletedContacts: deleted, RevokedConsents: revoked, RolledBackAt: now, RolledBackBy: actor, Reason: reason}
	eventID, err := id.New()
	if err != nil {
		return RollbackResult{}, err
	}
	res, err = tx.ExecContext(ctx, `UPDATE audience_imports SET status='ROLLED_BACK',rolled_back_at=$2,rolled_back_by=$3::uuid,rollback_reason=$4,updated_at=$2,version=version+1 WHERE id=$1::uuid AND version=$5`, importID, now, actor, reason, expectedVersion)
	if err != nil {
		return RollbackResult{}, err
	}
	updated, err := res.RowsAffected()
	if err != nil || updated != 1 {
		if err != nil {
			return RollbackResult{}, err
		}
		return RollbackResult{}, ErrImportConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audience_import_rollback_events(id,audience_import_id,actor_id,reason,restored_contacts,deleted_contacts,revoked_consents,occurred_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8)`, eventID, importID, actor, reason, restored, deleted, revoked, now); err != nil {
		return RollbackResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return RollbackResult{}, err
	}
	return result, nil
}

type MemoryRollbackRepository struct {
	Imports *MemoryImportRepository
}

func (r *MemoryRollbackRepository) Rollback(_ context.Context, importID string, expectedVersion int64, actor, reason string, now time.Time) (RollbackResult, error) {
	if r == nil || r.Imports == nil {
		return RollbackResult{}, errors.New("memory import repository is required")
	}
	r.Imports.mu.Lock()
	defer r.Imports.mu.Unlock()
	batch, ok := r.Imports.items[importID]
	if !ok {
		return RollbackResult{}, ErrImportNotFound
	}
	if batch.Version != expectedVersion {
		return RollbackResult{}, ErrImportConflict
	}
	if batch.Status == ImportRolledBack {
		return RollbackResult{ImportID: batch.ID, RolledBackAt: valueOrZeroTime(batch.RolledBackAt), RolledBackBy: batch.RolledBackBy, Reason: batch.RollbackReason}, nil
	}
	if batch.Status != ImportCompleted && batch.Status != ImportCompletedWithExceptions {
		return RollbackResult{}, ErrRollbackNotAllowed
	}
	rolledBackAt := now.UTC()
	batch.Status = ImportRolledBack
	batch.RolledBackAt = &rolledBackAt
	batch.RolledBackBy = actor
	batch.RollbackReason = reason
	batch.Version++
	batch.UpdatedAt = rolledBackAt
	r.Imports.items[batch.ID] = cloneImport(batch)
	return RollbackResult{ImportID: batch.ID, RolledBackAt: rolledBackAt, RolledBackBy: actor, Reason: reason}, nil
}

func valueOrZeroTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}
