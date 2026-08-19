package privacy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type PostgreSQLRepository struct{ DB *sql.DB }

const legalHoldColumns = `id::text,subject_lookup_hmac,coalesce(contact_id::text,''),coalesce(organisation_id::text,''),scope,status,reason,created_by::text,coalesce(submitted_by::text,''),coalesce(decided_by::text,''),coalesce(decision_reason,''),created_at,activated_at,rejected_at,expires_at,released_at,coalesce(released_by::text,''),coalesce(release_reason,''),version`

const legalHoldSelect = `SELECT ` + legalHoldColumns + ` FROM privacy_legal_holds`

const privacyCaseSelect = `SELECT id::text,case_type,status,subject_lookup_hmac,subject_masked,coalesce(contact_id::text,''),coalesce(organisation_id::text,''),requested_at,due_at,coalesce(assigned_to::text,''),created_by::text,coalesce(submitted_by::text,''),coalesce(decided_by::text,''),coalesce(executed_by::text,''),request_reason,coalesce(decision_reason,''),coalesce(execution_reason,''),requested_changes,result_ciphertext,coalesce(result_key_version,''),coalesce(result_sha256,''),completed_at,rejected_at,cancelled_at,version,created_at,updated_at FROM privacy_cases`

type sqlScanner interface{ Scan(...any) error }

func privacySubjectLockKey(lookup []byte) int64 {
	sum := sha256.Sum256(lookup)
	return int64(binary.BigEndian.Uint64(sum[:8]))
}

func lockPrivacySubject(ctx context.Context, tx *sql.Tx, lookup []byte) error {
	var ignored any
	return tx.QueryRowContext(ctx, `SELECT pg_advisory_xact_lock($1)`, privacySubjectLockKey(lookup)).Scan(&ignored)
}

func scanLegalHold(scanner sqlScanner) (LegalHold, error) {
	var hold LegalHold
	err := scanner.Scan(&hold.ID, &hold.SubjectLookupHMAC, &hold.ContactID, &hold.OrganisationID, &hold.Scope, &hold.Status, &hold.Reason, &hold.CreatedBy, &hold.SubmittedBy, &hold.DecidedBy, &hold.DecisionReason, &hold.CreatedAt, &hold.ActivatedAt, &hold.RejectedAt, &hold.ExpiresAt, &hold.ReleasedAt, &hold.ReleasedBy, &hold.ReleaseReason, &hold.Version)
	return hold, err
}

func scanPrivacyCase(scanner sqlScanner) (Case, error) {
	var item Case
	var changes []byte
	var cipher []byte
	err := scanner.Scan(&item.ID, &item.Type, &item.Status, &item.SubjectLookupHMAC, &item.SubjectMasked, &item.ContactID, &item.OrganisationID, &item.RequestedAt, &item.DueAt, &item.AssignedTo, &item.CreatedBy, &item.SubmittedBy, &item.DecidedBy, &item.ExecutedBy, &item.RequestReason, &item.DecisionReason, &item.ExecutionReason, &changes, &cipher, &item.ResultKeyVersion, &item.ResultSHA256, &item.CompletedAt, &item.RejectedAt, &item.CancelledAt, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return Case{}, err
	}
	item.RequestedChanges = append([]byte(nil), changes...)
	item.ResultCiphertext = append([]byte(nil), cipher...)
	return item, nil
}

func (r *PostgreSQLRepository) Create(ctx context.Context, item Case, event Event) (Case, error) {
	if r == nil || r.DB == nil {
		return Case{}, errors.New("privacy PostgreSQL repository is not configured")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Case{}, err
	}
	defer tx.Rollback()
	var contactID sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM contacts WHERE msisdn_lookup_hmac=$1 LIMIT 1`, item.SubjectLookupHMAC).Scan(&contactID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Case{}, err
	}
	if contactID.Valid {
		item.ContactID = contactID.String
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO privacy_cases(id,case_type,status,subject_lookup_hmac,subject_masked,contact_id,organisation_id,requested_at,due_at,created_by,request_reason,requested_changes,version,created_at,updated_at) VALUES($1::uuid,$2,$3,$4,$5,NULLIF($6,'')::uuid,NULLIF($7,'')::uuid,$8,$9,$10::uuid,$11,$12,$13,$14,$15)`, item.ID, item.Type, item.Status, item.SubjectLookupHMAC, item.SubjectMasked, item.ContactID, item.OrganisationID, item.RequestedAt, item.DueAt, item.CreatedBy, item.RequestReason, item.RequestedChanges, item.Version, item.CreatedAt, item.UpdatedAt)
	if err != nil {
		return Case{}, err
	}
	if err := insertPrivacyEvent(ctx, tx, event); err != nil {
		return Case{}, err
	}
	if err := tx.Commit(); err != nil {
		return Case{}, err
	}
	return item, nil
}

func (r *PostgreSQLRepository) Get(ctx context.Context, identifier string) (Case, error) {
	item, err := scanPrivacyCase(r.DB.QueryRowContext(ctx, privacyCaseSelect+` WHERE id=$1::uuid`, identifier))
	if errors.Is(err, sql.ErrNoRows) {
		return Case{}, ErrNotFound
	}
	return item, err
}

func (r *PostgreSQLRepository) List(ctx context.Context, query Query) (Page, error) {
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args := []any{}
	where := []string{"true"}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if query.Status != "" {
		add("status=$%d", query.Status)
	}
	if query.Type != "" {
		add("case_type=$%d", query.Type)
	}
	if strings.TrimSpace(query.AssignedTo) != "" {
		add("assigned_to=$%d::uuid", strings.TrimSpace(query.AssignedTo))
	}
	if query.AfterCreatedAt != nil {
		args = append(args, query.AfterCreatedAt.UTC(), query.AfterID)
		where = append(where, fmt.Sprintf("(created_at,id)<($%d,$%d::uuid)", len(args)-1, len(args)))
	}
	args = append(args, limit+1)
	rows, err := r.DB.QueryContext(ctx, privacyCaseSelect+` WHERE `+strings.Join(where, " AND ")+fmt.Sprintf(` ORDER BY created_at DESC,id DESC LIMIT $%d`, len(args)), args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	items := make([]Case, 0, limit+1)
	for rows.Next() {
		item, err := scanPrivacyCase(rows)
		if err != nil {
			return Page{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	page := Page{}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextAfter = last.CreatedAt.Format(time.RFC3339Nano) + "|" + last.ID
	} else {
		page.Items = items
	}
	return page, nil
}

func (r *PostgreSQLRepository) ListEvents(ctx context.Context, identifier string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return r.ListEventPage(ctx, identifier, limit, nil, "")
}
func (r *PostgreSQLRepository) ListEventPage(ctx context.Context, identifier string, limit int, before *time.Time, beforeID string) ([]Event, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,privacy_case_id::text,event_type,actor_id::text,coalesce(reason,''),case_version,evidence,occurred_at FROM privacy_case_events WHERE privacy_case_id=$1::uuid AND ($3::timestamptz IS NULL OR occurred_at<$3 OR (occurred_at=$3 AND id<NULLIF($4,'')::uuid)) ORDER BY occurred_at DESC,id DESC LIMIT $2`, identifier, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var event Event
		var evidence []byte
		if err := rows.Scan(&event.ID, &event.CaseID, &event.Type, &event.ActorID, &event.Reason, &event.CaseVersion, &evidence, &event.OccurredAt); err != nil {
			return nil, err
		}
		event.Evidence = append([]byte(nil), evidence...)
		out = append(out, event)
	}
	return out, rows.Err()
}

func (r *PostgreSQLRepository) AppendEvent(ctx context.Context, event Event) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	if strings.TrimSpace(event.ID) == "" || strings.TrimSpace(event.ActorID) == "" {
		return errors.New("privacy event identity and actor are required")
	}
	_, err := r.DB.ExecContext(ctx, `INSERT INTO privacy_case_events(id,privacy_case_id,event_type,actor_id,reason,case_version,evidence,occurred_at) VALUES($1::uuid,$2::uuid,$3,$4::uuid,NULLIF($5,''),$6,$7,$8)`, event.ID, event.CaseID, event.Type, event.ActorID, event.Reason, event.CaseVersion, event.Evidence, event.OccurredAt)
	return err
}

func (r *PostgreSQLRepository) Assign(ctx context.Context, identifier, assignee, reason string, expected int64, now time.Time, event Event) (Case, error) {
	return r.transition(ctx, identifier, expected, event, `UPDATE privacy_cases SET status='ASSIGNED',assigned_to=$3::uuid,version=version+1,updated_at=$4 WHERE id=$1::uuid AND version=$2 AND status IN ('OPEN','ASSIGNED') RETURNING `+selectPrivacyReturning, assignee, now)
}

func (r *PostgreSQLRepository) Submit(ctx context.Context, identifier, actor string, expected int64, now time.Time, event Event) (Case, error) {
	return r.transition(ctx, identifier, expected, event, `UPDATE privacy_cases SET status='PENDING_APPROVAL',submitted_by=$3::uuid,version=version+1,updated_at=$4 WHERE id=$1::uuid AND version=$2 AND status IN ('OPEN','ASSIGNED') RETURNING `+selectPrivacyReturning, actor, now)
}

func (r *PostgreSQLRepository) Decide(ctx context.Context, identifier string, approve bool, reason, actor string, expected int64, now time.Time, event Event) (Case, error) {
	if approve {
		return r.transition(ctx, identifier, expected, event, `UPDATE privacy_cases SET status='APPROVED',decided_by=$3::uuid,decision_reason=$4,version=version+1,updated_at=$5 WHERE id=$1::uuid AND version=$2 AND status='PENDING_APPROVAL' AND created_by<>$3::uuid AND coalesce(submitted_by,'00000000-0000-0000-0000-000000000000'::uuid)<>$3::uuid RETURNING `+selectPrivacyReturning, actor, reason, now)
	}
	return r.transition(ctx, identifier, expected, event, `UPDATE privacy_cases SET status='REJECTED',decided_by=$3::uuid,decision_reason=$4,rejected_at=$5,version=version+1,updated_at=$5 WHERE id=$1::uuid AND version=$2 AND status='PENDING_APPROVAL' AND created_by<>$3::uuid AND coalesce(submitted_by,'00000000-0000-0000-0000-000000000000'::uuid)<>$3::uuid RETURNING `+selectPrivacyReturning, actor, reason, now)
}

const selectPrivacyReturning = `id::text,case_type,status,subject_lookup_hmac,subject_masked,coalesce(contact_id::text,''),coalesce(organisation_id::text,''),requested_at,due_at,coalesce(assigned_to::text,''),created_by::text,coalesce(submitted_by::text,''),coalesce(decided_by::text,''),coalesce(executed_by::text,''),request_reason,coalesce(decision_reason,''),coalesce(execution_reason,''),requested_changes,result_ciphertext,coalesce(result_key_version,''),coalesce(result_sha256,''),completed_at,rejected_at,cancelled_at,version,created_at,updated_at`

func (r *PostgreSQLRepository) transition(ctx context.Context, identifier string, expected int64, event Event, statement string, args ...any) (Case, error) {
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Case{}, err
	}
	defer tx.Rollback()
	params := []any{identifier, expected}
	params = append(params, args...)
	item, err := scanPrivacyCase(tx.QueryRowContext(ctx, statement, params...))
	if errors.Is(err, sql.ErrNoRows) {
		return Case{}, ErrConflict
	}
	if err != nil {
		return Case{}, err
	}
	event.CaseVersion = item.Version
	if err := insertPrivacyEvent(ctx, tx, event); err != nil {
		return Case{}, err
	}
	if err := tx.Commit(); err != nil {
		return Case{}, err
	}
	return item, nil
}

func (r *PostgreSQLRepository) LoadSubjectPackage(ctx context.Context, lookup []byte, now time.Time) (SubjectPackage, error) {
	return r.loadSubjectPackageConcrete(ctx, lookup, now)
}

func (r *PostgreSQLRepository) loadSubjectPackageConcrete(ctx context.Context, lookup []byte, now time.Time) (SubjectPackage, error) {
	var pkg SubjectPackage
	var countryID, stateID, lgaID, gender, language, ageSource, sourceSystem, sourceRecord sql.NullString
	var age sql.NullInt64
	var ageRecorded sql.NullTime
	var ageVerified bool
	err := r.DB.QueryRowContext(ctx, `SELECT id::text,encrypted_msisdn,masked_msisdn,status,processing_restricted,country_id::text,state_id::text,lga_id::text,reported_age,age_recorded_at,gender_code,preferred_language_code,age_source,age_verified,source_system,source_record_id FROM contacts WHERE msisdn_lookup_hmac=$1`, lookup).Scan(&pkg.ContactID, &pkg.EncryptedMSISDN, &pkg.MaskedMSISDN, &pkg.Status, &pkg.ProcessingRestricted, &countryID, &stateID, &lgaID, &age, &ageRecorded, &gender, &language, &ageSource, &ageVerified, &sourceSystem, &sourceRecord)
	if errors.Is(err, sql.ErrNoRows) {
		return SubjectPackage{}, ErrNotFound
	}
	if err != nil {
		return SubjectPackage{}, err
	}
	profile := map[string]any{"ageVerified": ageVerified}
	putNullString(profile, "countryId", countryID)
	putNullString(profile, "stateId", stateID)
	putNullString(profile, "lgaId", lgaID)
	putNullString(profile, "genderCode", gender)
	putNullString(profile, "preferredLanguageCode", language)
	putNullString(profile, "ageSource", ageSource)
	putNullString(profile, "sourceSystem", sourceSystem)
	putNullString(profile, "sourceRecordId", sourceRecord)
	if age.Valid {
		profile["reportedAge"] = age.Int64
	}
	if ageRecorded.Valid {
		profile["ageRecordedAt"] = ageRecorded.Time.Format("2006-01-02")
	}
	pkg.Profile = profile
	var errLoad error
	if pkg.Attributes, errLoad = queryJSONRecords(ctx, r.DB, `SELECT jsonb_build_object('code',d.code,'displayName',d.display_name,'valueText',v.value_text,'valueInteger',v.value_integer,'valueDecimal',v.value_decimal,'valueBoolean',v.value_boolean,'valueDate',v.value_date,'valueJson',v.value_json,'sourceSystem',v.source_system,'recordedAt',v.recorded_at) FROM contact_attribute_values v JOIN attribute_definitions d ON d.id=v.attribute_definition_id WHERE v.contact_id=$1::uuid ORDER BY d.code,v.recorded_at DESC`, pkg.ContactID); errLoad != nil {
		return SubjectPackage{}, errLoad
	}
	if pkg.Consents, errLoad = queryJSONRecords(ctx, r.DB, `SELECT jsonb_build_object('id',id,'organisationId',organisation_id,'purposeId',purpose_id,'channel',channel,'wordingVersion',wording_version,'sourceType',source_type,'sourceReference',source_reference,'grantedAt',granted_at,'effectiveFrom',effective_from,'expiresAt',expires_at,'status',status) FROM consent_grants WHERE contact_id=$1::uuid ORDER BY created_at DESC,id DESC`, pkg.ContactID); errLoad != nil {
		return SubjectPackage{}, errLoad
	}
	if pkg.Suppressions, errLoad = queryJSONRecords(ctx, r.DB, `SELECT jsonb_build_object('id',id,'organisationId',organisation_id,'purposeId',purpose_id,'channel',channel,'scope',scope,'reason',reason,'effectiveAt',effective_at,'expiresAt',expires_at,'active',active) FROM suppressions WHERE contact_id=$1::uuid OR msisdn_lookup_hmac=$2 ORDER BY created_at DESC,id DESC`, pkg.ContactID, lookup); errLoad != nil {
		return SubjectPackage{}, errLoad
	}
	if pkg.CampaignHistory, errLoad = queryJSONRecords(ctx, r.DB, `SELECT jsonb_build_object('campaignId',c.id,'campaignName',c.name,'organisationId',c.organisation_id,'status',cr.status,'authorisedAt',cr.authorised_at,'submittedAt',cr.submitted_at,'completedAt',cr.completed_at) FROM campaign_recipients cr JOIN campaigns c ON c.id=cr.campaign_id WHERE cr.contact_id=$1::uuid ORDER BY cr.authorised_at DESC,cr.id DESC`, pkg.ContactID); errLoad != nil {
		return SubjectPackage{}, errLoad
	}
	pkg.GeneratedAt = now.UTC()
	return pkg, nil
}

func putNullString(target map[string]any, key string, value sql.NullString) {
	if value.Valid && value.String != "" {
		target[key] = value.String
	}
}

func queryJSONRecords(ctx context.Context, db *sql.DB, statement string, args ...any) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *PostgreSQLRepository) Execute(ctx context.Context, current Case, actor, reason string, cipher []byte, keyVersion, checksum string, now time.Time, event Event) (Case, error) {
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Case{}, err
	}
	defer tx.Rollback()
	locked, err := scanPrivacyCase(tx.QueryRowContext(ctx, privacyCaseSelect+` WHERE id=$1::uuid FOR UPDATE`, current.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return Case{}, ErrNotFound
	}
	if err != nil {
		return Case{}, err
	}
	if locked.Version != current.Version || locked.Status != StatusApproved || locked.CreatedBy == actor || locked.DecidedBy == actor || locked.ContactID == "" {
		return Case{}, ErrConflict
	}
	if err := insertProfileHistory(ctx, tx, locked, actor, reason, now); err != nil {
		return Case{}, err
	}
	switch locked.Type {
	case CaseRectification:
		if err := applyRectification(ctx, tx, locked, actor, reason, now); err != nil {
			return Case{}, err
		}
	case CaseErasure:
		if err := applyErasure(ctx, tx, locked, actor, reason, checksum, now); err != nil {
			return Case{}, err
		}
	case CaseObjection:
		if err := applyObjection(ctx, tx, locked, actor, reason, now); err != nil {
			return Case{}, err
		}
	case CaseRestriction:
		if _, err := tx.ExecContext(ctx, `UPDATE contacts SET processing_restricted=true,status_reason=$2,status_updated_by=$3::uuid,status_updated_at=$4,updated_at=$4 WHERE id=$1::uuid`, locked.ContactID, reason, actor, now); err != nil {
			return Case{}, err
		}
	case CaseAccess, CasePortability:
	default:
		return Case{}, ErrInvalid
	}
	updated, err := scanPrivacyCase(tx.QueryRowContext(ctx, `UPDATE privacy_cases SET status='COMPLETED',executed_by=$3::uuid,execution_reason=$4,result_ciphertext=$5,result_key_version=NULLIF($6,''),result_sha256=NULLIF($7,''),completed_at=$8,version=version+1,updated_at=$8 WHERE id=$1::uuid AND version=$2 AND status='APPROVED' RETURNING `+selectPrivacyReturning, locked.ID, locked.Version, actor, reason, cipher, keyVersion, checksum, now))
	if errors.Is(err, sql.ErrNoRows) {
		return Case{}, ErrConflict
	}
	if err != nil {
		return Case{}, err
	}
	event.CaseVersion = updated.Version
	if err := insertPrivacyEvent(ctx, tx, event); err != nil {
		return Case{}, err
	}
	if err := tx.Commit(); err != nil {
		return Case{}, err
	}
	return updated, nil
}

func insertProfileHistory(ctx context.Context, tx *sql.Tx, item Case, actor, reason string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO contact_profile_history(contact_id,audience_import_id,privacy_case_id,actor_id,change_reason,reported_age,age_recorded_at,gender_code,country_id,state_id,lga_id,source_record_hash,recorded_at,profile_recorded_at,source_system,source_record_id,age_source,age_verified,preferred_language_code,contact_status) SELECT id,NULL,$2::uuid,$3::uuid,$4,reported_age,age_recorded_at,gender_code,country_id,state_id,lga_id,$5,$6,profile_recorded_at,source_system,source_record_id,age_source,age_verified,preferred_language_code,status FROM contacts WHERE id=$1::uuid`, item.ContactID, item.ID, actor, reason, "privacy:"+item.ID, now)
	return err
}

func applyRectification(ctx context.Context, tx *sql.Tx, item Case, actor, reason string, now time.Time) error {
	if err := ValidateRequestedChanges(item.Type, item.RequestedChanges); err != nil {
		return err
	}
	var changes map[string]any
	if err := json.Unmarshal(item.RequestedChanges, &changes); err != nil {
		return err
	}
	sets := []string{"status_reason=$2", "status_updated_by=$3::uuid", "status_updated_at=$4", "profile_recorded_at=$4", "updated_at=$4"}
	args := []any{item.ContactID, reason, actor, now}
	columns := map[string]string{
		"reportedAge": "reported_age", "ageRecordedAt": "age_recorded_at", "ageSource": "age_source", "ageVerified": "age_verified", "genderCode": "gender_code", "preferredLanguageCode": "preferred_language_code", "countryId": "country_id", "stateId": "state_id", "lgaId": "lga_id", "status": "status",
	}
	for _, key := range []string{"reportedAge", "ageRecordedAt", "ageSource", "ageVerified", "genderCode", "preferredLanguageCode", "countryId", "stateId", "lgaId", "status"} {
		value, exists := changes[key]
		if !exists {
			continue
		}
		args = append(args, value)
		position := len(args)
		column := columns[key]
		switch key {
		case "reportedAge":
			sets = append(sets, fmt.Sprintf("%s=$%d::smallint", column, position))
		case "ageRecordedAt":
			sets = append(sets, fmt.Sprintf("%s=NULLIF($%d::text,'')::date", column, position))
		case "ageVerified":
			sets = append(sets, fmt.Sprintf("%s=$%d::boolean", column, position))
		case "countryId", "stateId", "lgaId":
			sets = append(sets, fmt.Sprintf("%s=NULLIF($%d::text,'')::uuid", column, position))
		default:
			sets = append(sets, fmt.Sprintf("%s=NULLIF($%d::text,'')", column, position))
		}
	}
	if len(sets) == 5 {
		return ErrInvalid
	}
	res, err := tx.ExecContext(ctx, `UPDATE contacts SET `+strings.Join(sets, ",")+` WHERE id=$1::uuid`, args...)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func applyErasure(ctx context.Context, tx *sql.Tx, item Case, actor, reason, checksum string, now time.Time) error {
	if err := lockPrivacySubject(ctx, tx, item.SubjectLookupHMAC); err != nil {
		return fmt.Errorf("lock privacy subject for erasure: %w", err)
	}
	var blocked bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM privacy_legal_holds WHERE subject_lookup_hmac=$1 AND status='ACTIVE' AND released_at IS NULL AND (expires_at IS NULL OR expires_at>$2) AND scope IN ('ALL','CONTACT'))`, item.SubjectLookupHMAC, now).Scan(&blocked); err != nil {
		return err
	}
	if blocked {
		return ErrLegalHold
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM contact_attribute_values WHERE contact_id=$1::uuid`, item.ContactID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE consent_grants SET status='WITHDRAWN',version=version+1,updated_at=$2 WHERE contact_id=$1::uuid AND status IN ('ACTIVE','PENDING_VERIFICATION')`, item.ContactID, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE contacts SET encrypted_msisdn=gen_random_bytes(44),masked_msisdn='ANONYMISED',country_id=NULL,state_id=NULL,lga_id=NULL,reported_age=NULL,age_recorded_at=NULL,age_source=NULL,age_verified=false,gender_code=NULL,preferred_language_code=NULL,status='ANONYMISED',status_reason=$2,status_updated_by=$3::uuid,status_updated_at=$4,processing_restricted=true,source_system=NULL,source_record_id=NULL,anonymised_at=$4,updated_at=$4 WHERE id=$1::uuid`, item.ContactID, reason, actor, now); err != nil {
		return err
	}
	return ensurePrivacySuppression(ctx, tx, item, actor, reason, "privacy-erasure:"+checksum, now)
}

func applyObjection(ctx context.Context, tx *sql.Tx, item Case, actor, reason string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `UPDATE contacts SET status='SUPPRESSED',status_reason=$2,status_updated_by=$3::uuid,status_updated_at=$4,updated_at=$4 WHERE id=$1::uuid`, item.ContactID, reason, actor, now); err != nil {
		return err
	}
	return ensurePrivacySuppression(ctx, tx, item, actor, reason, "privacy-objection:"+item.ID, now)
}

func ensurePrivacySuppression(ctx context.Context, tx *sql.Tx, item Case, actor, reason, fingerprint string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO suppressions(id,contact_id,msisdn_lookup_hmac,organisation_id,purpose_id,channel,scope,reason,effective_at,active,source_reference,created_by,client_request_id,request_fingerprint,version,created_at) SELECT gen_random_uuid(),$1::uuid,$2,NULL,NULL,'WHATSAPP','GLOBAL',$3,$4,true,$5,$6::uuid,$5,$7,1,$4 WHERE NOT EXISTS(SELECT 1 FROM suppressions WHERE (contact_id=$1::uuid OR msisdn_lookup_hmac=$2) AND scope='GLOBAL' AND active=true AND effective_at<=$4 AND (expires_at IS NULL OR expires_at>$4))`, item.ContactID, item.SubjectLookupHMAC, reason, now, "privacy-case:"+item.ID, actor, fingerprint)
	return err
}

func (r *PostgreSQLRepository) CreateLegalHold(ctx context.Context, hold LegalHold, event LegalHoldEvent) (LegalHold, error) {
	if r == nil || r.DB == nil {
		return LegalHold{}, errors.New("privacy PostgreSQL repository is not configured")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return LegalHold{}, err
	}
	defer tx.Rollback()
	var contactID sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM contacts WHERE msisdn_lookup_hmac=$1`, hold.SubjectLookupHMAC).Scan(&contactID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return LegalHold{}, err
	}
	if contactID.Valid {
		hold.ContactID = contactID.String
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO privacy_legal_holds(id,subject_lookup_hmac,contact_id,organisation_id,scope,status,reason,created_by,created_at,expires_at,version) VALUES($1::uuid,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$6,$7,$8::uuid,$9,$10,$11)`, hold.ID, hold.SubjectLookupHMAC, hold.ContactID, hold.OrganisationID, hold.Scope, hold.Status, hold.Reason, hold.CreatedBy, hold.CreatedAt, hold.ExpiresAt, hold.Version)
	if err != nil {
		return LegalHold{}, err
	}
	if err := insertLegalHoldEvent(ctx, tx, event); err != nil {
		return LegalHold{}, err
	}
	if err := tx.Commit(); err != nil {
		return LegalHold{}, err
	}
	return hold, nil
}

func (r *PostgreSQLRepository) SubmitLegalHold(ctx context.Context, identifier, actor string, expected int64, now time.Time, event LegalHoldEvent) (LegalHold, error) {
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return LegalHold{}, err
	}
	defer tx.Rollback()
	hold, err := scanLegalHold(tx.QueryRowContext(ctx, legalHoldSelect+` WHERE id=$1::uuid FOR UPDATE`, identifier))
	if errors.Is(err, sql.ErrNoRows) {
		return LegalHold{}, ErrNotFound
	}
	if err != nil {
		return LegalHold{}, err
	}
	if hold.Version != expected || hold.Status != HoldDraft {
		return LegalHold{}, ErrConflict
	}
	hold, err = scanLegalHold(tx.QueryRowContext(ctx, `UPDATE privacy_legal_holds SET status='PENDING_APPROVAL',submitted_by=$3::uuid,version=version+1 WHERE id=$1::uuid AND version=$2 AND status='DRAFT' RETURNING `+legalHoldColumns, identifier, expected, actor))
	if errors.Is(err, sql.ErrNoRows) {
		return LegalHold{}, ErrConflict
	}
	if err != nil {
		return LegalHold{}, err
	}
	event.HoldVersion = hold.Version
	event.OccurredAt = now.UTC()
	if err := insertLegalHoldEvent(ctx, tx, event); err != nil {
		return LegalHold{}, err
	}
	if err := tx.Commit(); err != nil {
		return LegalHold{}, err
	}
	return hold, nil
}

func (r *PostgreSQLRepository) DecideLegalHold(ctx context.Context, identifier string, approve bool, reason, actor string, expected int64, now time.Time, event LegalHoldEvent) (LegalHold, error) {
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return LegalHold{}, err
	}
	defer tx.Rollback()
	hold, err := scanLegalHold(tx.QueryRowContext(ctx, legalHoldSelect+` WHERE id=$1::uuid FOR UPDATE`, identifier))
	if errors.Is(err, sql.ErrNoRows) {
		return LegalHold{}, ErrNotFound
	}
	if err != nil {
		return LegalHold{}, err
	}
	if hold.Version != expected || hold.Status != HoldPendingApproval || hold.CreatedBy == actor || hold.SubmittedBy == actor {
		return LegalHold{}, ErrConflict
	}
	if approve {
		if err := lockPrivacySubject(ctx, tx, hold.SubjectLookupHMAC); err != nil {
			return LegalHold{}, fmt.Errorf("lock privacy subject for legal hold activation: %w", err)
		}
	}
	status := HoldRejected
	activatedAt := (*time.Time)(nil)
	rejectedAt := &now
	if approve {
		status = HoldActive
		activatedAt = &now
		rejectedAt = nil
	}
	hold, err = scanLegalHold(tx.QueryRowContext(ctx, `UPDATE privacy_legal_holds SET status=$3,decided_by=$4::uuid,decision_reason=$5,activated_at=$6,rejected_at=$7,version=version+1 WHERE id=$1::uuid AND version=$2 AND status='PENDING_APPROVAL' RETURNING `+legalHoldColumns, identifier, expected, status, actor, reason, activatedAt, rejectedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return LegalHold{}, ErrConflict
	}
	if err != nil {
		return LegalHold{}, err
	}
	event.HoldVersion = hold.Version
	event.OccurredAt = now.UTC()
	if err := insertLegalHoldEvent(ctx, tx, event); err != nil {
		return LegalHold{}, err
	}
	if err := tx.Commit(); err != nil {
		return LegalHold{}, err
	}
	return hold, nil
}

func (r *PostgreSQLRepository) ListLegalHoldEvents(ctx context.Context, identifier string, limit int) ([]LegalHoldEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return r.ListLegalHoldEventPage(ctx, identifier, limit, nil, "")
}
func (r *PostgreSQLRepository) ListLegalHoldEventPage(ctx context.Context, identifier string, limit int, before *time.Time, beforeID string) ([]LegalHoldEvent, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,legal_hold_id::text,event_type,actor_id::text,coalesce(reason,''),hold_version,evidence,occurred_at FROM privacy_legal_hold_events WHERE legal_hold_id=$1::uuid AND ($3::timestamptz IS NULL OR occurred_at<$3 OR (occurred_at=$3 AND id<NULLIF($4,'')::uuid)) ORDER BY occurred_at DESC,id DESC LIMIT $2`, identifier, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LegalHoldEvent{}
	for rows.Next() {
		var event LegalHoldEvent
		if err := rows.Scan(&event.ID, &event.LegalHoldID, &event.Type, &event.ActorID, &event.Reason, &event.HoldVersion, &event.Evidence, &event.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (r *PostgreSQLRepository) ListLegalHolds(ctx context.Context, lookup []byte, activeOnly bool, limit int) ([]LegalHold, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	statement := legalHoldSelect + ` WHERE subject_lookup_hmac=$1`
	if activeOnly {
		statement += ` AND status='ACTIVE' AND (expires_at IS NULL OR expires_at>now())`
	}
	statement += ` ORDER BY created_at DESC,id DESC LIMIT $2`
	rows, err := r.DB.QueryContext(ctx, statement, lookup, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LegalHold{}
	for rows.Next() {
		hold, err := scanLegalHold(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, hold)
	}
	return out, rows.Err()
}

func (r *PostgreSQLRepository) ListLegalHoldPage(ctx context.Context, lookup []byte, activeOnly bool, limit int, before *time.Time, beforeID string) ([]LegalHold, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("privacy PostgreSQL repository is not configured")
	}
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, legalHoldSelect+` WHERE subject_lookup_hmac=$1 AND (NOT $2::boolean OR (status='ACTIVE' AND released_at IS NULL AND (expires_at IS NULL OR expires_at>now()))) AND ($4::timestamptz IS NULL OR created_at<$4 OR (created_at=$4 AND id<NULLIF($5,'')::uuid)) ORDER BY created_at DESC,id DESC LIMIT $3`, lookup, activeOnly, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]LegalHold, 0, limit)
	for rows.Next() {
		hold, scanErr := scanLegalHold(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, hold)
	}
	return out, rows.Err()
}

func (r *PostgreSQLRepository) ReleaseLegalHold(ctx context.Context, identifier, reason, actor string, expected int64, now time.Time, event LegalHoldEvent) (LegalHold, error) {
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return LegalHold{}, err
	}
	defer tx.Rollback()
	hold, err := scanLegalHold(tx.QueryRowContext(ctx, legalHoldSelect+` WHERE id=$1::uuid FOR UPDATE`, identifier))
	if errors.Is(err, sql.ErrNoRows) {
		return LegalHold{}, ErrNotFound
	}
	if err != nil {
		return LegalHold{}, err
	}
	if hold.Version != expected || hold.Status != HoldActive || hold.ReleasedAt != nil {
		return LegalHold{}, ErrConflict
	}
	if err := lockPrivacySubject(ctx, tx, hold.SubjectLookupHMAC); err != nil {
		return LegalHold{}, fmt.Errorf("lock privacy subject for legal hold release: %w", err)
	}
	hold, err = scanLegalHold(tx.QueryRowContext(ctx, `UPDATE privacy_legal_holds SET status='RELEASED',released_at=$4,released_by=$3::uuid,release_reason=$5,version=version+1 WHERE id=$1::uuid AND version=$2 AND status='ACTIVE' AND released_at IS NULL RETURNING `+legalHoldColumns, identifier, expected, actor, now, reason))
	if errors.Is(err, sql.ErrNoRows) {
		return LegalHold{}, ErrConflict
	}
	if err != nil {
		return LegalHold{}, err
	}
	event.HoldVersion = hold.Version
	event.OccurredAt = now.UTC()
	if err := insertLegalHoldEvent(ctx, tx, event); err != nil {
		return LegalHold{}, err
	}
	if err := tx.Commit(); err != nil {
		return LegalHold{}, err
	}
	return hold, nil
}

func (r *PostgreSQLRepository) HasActiveLegalHold(ctx context.Context, lookup []byte, scope string, now time.Time) (bool, error) {
	var exists bool
	err := r.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM privacy_legal_holds WHERE subject_lookup_hmac=$1 AND status='ACTIVE' AND released_at IS NULL AND (expires_at IS NULL OR expires_at>$2) AND scope IN ('ALL',$3))`, lookup, now, strings.ToUpper(strings.TrimSpace(scope))).Scan(&exists)
	return exists, err
}

func insertLegalHoldEvent(ctx context.Context, tx *sql.Tx, event LegalHoldEvent) error {
	if strings.TrimSpace(event.ID) == "" || strings.TrimSpace(event.ActorID) == "" {
		return errors.New("legal hold event identity and actor are required")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO privacy_legal_hold_events(id,legal_hold_id,event_type,actor_id,reason,hold_version,evidence,occurred_at) VALUES($1::uuid,$2::uuid,$3,$4::uuid,NULLIF($5,''),$6,$7,$8)`, event.ID, event.LegalHoldID, event.Type, event.ActorID, event.Reason, event.HoldVersion, event.Evidence, event.OccurredAt)
	return err
}

func insertPrivacyEvent(ctx context.Context, tx *sql.Tx, event Event) error {
	if strings.TrimSpace(event.ID) == "" || strings.TrimSpace(event.ActorID) == "" {
		return errors.New("privacy event identity and actor are required")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO privacy_case_events(id,privacy_case_id,event_type,actor_id,reason,case_version,evidence,occurred_at) VALUES($1::uuid,$2::uuid,$3,$4::uuid,NULLIF($5,''),$6,$7,$8)`, event.ID, event.CaseID, event.Type, event.ActorID, event.Reason, event.CaseVersion, event.Evidence, event.OccurredAt)
	return err
}
