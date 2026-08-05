package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	pgretry "campaign-platform/internal/persistence/postgres"
)

// PostgreSQLMergeRepository promotes protected staging rows into canonical
// contacts, immutable profile history, source lineage and purpose/channel-scoped
// consent grants in one transaction. The import row is locked and the committed
// result is persisted, making concurrent calls and replays idempotent.
type PostgreSQLMergeRepository struct{ DB *sql.DB }

type mergeBasis struct {
	OrganisationID, PurposeID, Channel, WordingVersion string
	UploadedBy, ApprovedBy                             string
	GrantedAt                                          time.Time
	ExpiresAt                                          sql.NullTime
	ImportStatus, ReviewStatus                         string
	ReviewExpiresAt                                    sql.NullTime
	SourceSystem                                       string
	MalwareStatus                                      string
	ContentSignatureValid                              bool
	UpdatePolicy                                       UpdatePolicy
}

func (r *PostgreSQLMergeRepository) Merge(ctx context.Context, importID string, now time.Time) (MergeResult, error) {
	return pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (MergeResult, error) {
		return r.mergeOnce(ctx, importID, now)
	})
}

func (r *PostgreSQLMergeRepository) mergeOnce(ctx context.Context, importID string, now time.Time) (MergeResult, error) {
	if r == nil || r.DB == nil {
		return MergeResult{}, errors.New("database is required")
	}
	if strings.TrimSpace(importID) == "" {
		return MergeResult{}, errors.New("import ID is required")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return MergeResult{}, err
	}
	defer tx.Rollback()

	basis, err := lockMergeBasis(ctx, tx, importID)
	if errors.Is(err, sql.ErrNoRows) {
		return MergeResult{}, ErrImportNotApproved
	}
	if err != nil {
		return MergeResult{}, fmt.Errorf("load import merge basis: %w", err)
	}
	if existing, ok, err := loadMergeResult(ctx, tx, importID); err != nil {
		return MergeResult{}, err
	} else if ok {
		if err := tx.Commit(); err != nil {
			return MergeResult{}, err
		}
		return existing, nil
	}
	if basis.ImportStatus != "APPROVED" && basis.ImportStatus != "IMPORTING" {
		return MergeResult{}, ErrImportNotApproved
	}
	if basis.UploadedBy == "" || basis.ApprovedBy == "" || basis.UploadedBy == basis.ApprovedBy {
		return MergeResult{}, ErrMakerChecker
	}
	if basis.ReviewStatus != "APPROVED" || !basis.ReviewExpiresAt.Valid || !basis.ReviewExpiresAt.Time.After(now) ||
		basis.OrganisationID == "" || basis.PurposeID == "" || strings.ToUpper(basis.Channel) != "WHATSAPP" || basis.WordingVersion == "" {
		return MergeResult{}, ErrConsentBasis
	}
	if basis.MalwareStatus != string(MalwareClean) || !basis.ContentSignatureValid {
		return MergeResult{}, ErrImportScanRequired
	}
	if basis.UpdatePolicy != UpdateInsertOnly && basis.UpdatePolicy != UpdateNewestSource && basis.UpdatePolicy != UpdateFillNull && basis.UpdatePolicy != UpdateTrustedSource && basis.UpdatePolicy != UpdateManualConflict {
		return MergeResult{}, ErrUnsupportedUpdatePolicy
	}
	var evidenceCount, unsafeEvidence int
	if err := tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE malware_scan_status<>'CLEAN') FROM consent_review_evidence WHERE consent_review_id=(SELECT consent_review_id FROM audience_imports WHERE id=$1::uuid)`, importID).Scan(&evidenceCount, &unsafeEvidence); err != nil {
		return MergeResult{}, fmt.Errorf("check consent evidence: %w", err)
	}
	if evidenceCount == 0 || unsafeEvidence != 0 {
		return MergeResult{}, ErrConsentBasis
	}
	var stagedCount, invalidGeography int
	if err := tx.QueryRowContext(ctx, resolvedStagingCountSQL, importID).Scan(&stagedCount, &invalidGeography); err != nil {
		return MergeResult{}, fmt.Errorf("validate staged geography: %w", err)
	}
	if stagedCount == 0 {
		return MergeResult{}, errors.New("approved import contains no staged contacts")
	}
	if invalidGeography != 0 {
		return MergeResult{}, fmt.Errorf("%d staged contacts have unresolved geography", invalidGeography)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE audience_imports SET status='IMPORTING',started_at=coalesce(started_at,$2),updated_at=$2,version=version+1 WHERE id=$1::uuid AND status IN ('APPROVED','IMPORTING')`, importID, now.UTC()); err != nil {
		return MergeResult{}, fmt.Errorf("mark import processing: %w", err)
	}

	result := MergeResult{}
	upsertSQL := upsertContactsNewestSQL
	if basis.UpdatePolicy == UpdateInsertOnly {
		upsertSQL = insertContactsOnlySQL
	}
	if basis.UpdatePolicy == UpdateFillNull || basis.UpdatePolicy == UpdateManualConflict {
		upsertSQL = upsertContactsFillNullSQL
	}
	if basis.UpdatePolicy == UpdateTrustedSource {
		upsertSQL = upsertContactsTrustedSQL
	}
	if basis.UpdatePolicy == UpdateManualConflict {
		if err := tx.QueryRowContext(ctx, insertProfileConflictsSQL, importID, now.UTC()).Scan(&result.Conflicts); err != nil {
			return MergeResult{}, fmt.Errorf("capture profile conflicts: %w", err)
		}
	}
	if err := tx.QueryRowContext(ctx, upsertSQL, importID, now.UTC()).Scan(&result.InsertedContacts, &result.UpdatedContacts); err != nil {
		return MergeResult{}, fmt.Errorf("merge canonical contacts: %w", err)
	}
	if err := tx.QueryRowContext(ctx, insertContactSourcesSQL, importID, now.UTC()).Scan(&result.SourceLinks); err != nil {
		return MergeResult{}, fmt.Errorf("merge contact sources: %w", err)
	}
	if err := tx.QueryRowContext(ctx, insertProfileHistorySQL, importID, now.UTC()).Scan(&result.ProfileHistory); err != nil {
		return MergeResult{}, fmt.Errorf("merge profile history: %w", err)
	}
	if err := tx.QueryRowContext(ctx, insertConsentGrantsSQL, importID, now.UTC()).Scan(&result.ConsentGrants); err != nil {
		return MergeResult{}, fmt.Errorf("merge consent grants: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audience_import_merge_results(audience_import_id,inserted_contacts,updated_contacts,consent_grants,source_links,profile_history,conflicts,completed_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(audience_import_id) DO NOTHING`, importID, result.InsertedContacts, result.UpdatedContacts, result.ConsentGrants, result.SourceLinks, result.ProfileHistory, result.Conflicts, now.UTC()); err != nil {
		return MergeResult{}, fmt.Errorf("store import merge result: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE audience_imports SET status=CASE WHEN invalid_rows>0 THEN 'COMPLETED_WITH_EXCEPTIONS' ELSE 'COMPLETED' END,inserted_contacts=$2,updated_contacts=$3,completed_at=$4,failure_reason=NULL,updated_at=$4,version=version+1 WHERE id=$1::uuid AND status='IMPORTING'`, importID, result.InsertedContacts, result.UpdatedContacts, now.UTC()); err != nil {
		return MergeResult{}, fmt.Errorf("complete audience import: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return MergeResult{}, fmt.Errorf("commit audience import merge: %w", err)
	}
	return result, nil
}

func lockMergeBasis(ctx context.Context, tx *sql.Tx, importID string) (mergeBasis, error) {
	const query = `
SELECT ai.organisation_id::text,ai.purpose_id::text,upper(coalesce(ai.channel,'')),coalesce(ai.wording_version,''),
       coalesce(ai.uploaded_by::text,''),coalesce(ai.approved_by::text,''),coalesce(ai.granted_at,ai.approved_at,ai.created_at),ai.expires_at,
       ai.status,cr.status,cr.expires_at,coalesce(ai.source_system,''),ai.malware_scan_status,ai.content_signature_valid,ai.update_policy
FROM audience_imports ai
JOIN consent_reviews cr ON cr.id=ai.consent_review_id AND cr.organisation_id=ai.organisation_id
JOIN consent_purposes cp ON cp.id=ai.purpose_id
  AND cp.organisation_id=ai.organisation_id
  AND cp.consent_review_id=ai.consent_review_id
  AND upper(cp.channel)=upper(ai.channel)
  AND cp.wording_version=ai.wording_version
  AND cp.active
WHERE ai.id=$1::uuid
FOR UPDATE OF ai`
	var b mergeBasis
	err := tx.QueryRowContext(ctx, query, importID).Scan(&b.OrganisationID, &b.PurposeID, &b.Channel, &b.WordingVersion, &b.UploadedBy, &b.ApprovedBy, &b.GrantedAt, &b.ExpiresAt, &b.ImportStatus, &b.ReviewStatus, &b.ReviewExpiresAt, &b.SourceSystem, &b.MalwareStatus, &b.ContentSignatureValid, &b.UpdatePolicy)
	return b, err
}

func loadMergeResult(ctx context.Context, tx *sql.Tx, importID string) (MergeResult, bool, error) {
	var v MergeResult
	err := tx.QueryRowContext(ctx, `SELECT inserted_contacts,updated_contacts,consent_grants,source_links,profile_history,conflicts FROM audience_import_merge_results WHERE audience_import_id=$1::uuid`, importID).Scan(&v.InsertedContacts, &v.UpdatedContacts, &v.ConsentGrants, &v.SourceLinks, &v.ProfileHistory, &v.Conflicts)
	if errors.Is(err, sql.ErrNoRows) {
		return MergeResult{}, false, nil
	}
	return v, err == nil, err
}

const resolvedStagingCTE = `
WITH resolved AS (
 SELECT s.*,c.id AS country_id,st.id AS state_id,lg.id AS lga_id
 FROM audience_import_staging s
 LEFT JOIN countries c ON c.iso2=s.country_iso2 AND c.active
 LEFT JOIN administrative_areas st ON st.country_id=c.id AND st.level=1 AND st.active
   AND lower(st.name)=lower(s.state_name) AND st.parent_id IS NULL
 LEFT JOIN administrative_areas lg ON lg.country_id=c.id AND lg.level=2 AND lg.active
   AND lg.parent_id=st.id AND lower(lg.name)=lower(s.lga_name)
 WHERE s.audience_import_id=$1::uuid
)`

const resolvedStagingCountSQL = resolvedStagingCTE + `
SELECT count(*),count(*) FILTER(WHERE country_id IS NULL OR (state_name IS NOT NULL AND state_id IS NULL) OR (lga_name IS NOT NULL AND lga_id IS NULL)) FROM resolved`

const upsertContactsNewestSQL = resolvedStagingCTE + `,
basis AS MATERIALIZED (
 SELECT coalesce(granted_at,approved_at,created_at) AS observed_at,
        coalesce(source_system,'AUDIENCE_IMPORT') AS source_system
 FROM audience_imports WHERE id=$1::uuid
), existing AS MATERIALIZED (
 SELECT ct.msisdn_lookup_hmac FROM contacts ct JOIN resolved r ON r.msisdn_lookup_hmac=ct.msisdn_lookup_hmac
), merged AS (
 INSERT INTO contacts(encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,country_id,state_id,lga_id,reported_age,age_recorded_at,age_source,age_verified,gender_code,status,source_system,source_record_id,profile_recorded_at,created_at,updated_at)
 SELECT encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,country_id,state_id,lga_id,reported_age,age_recorded_at,
        CASE WHEN reported_age IS NULL THEN NULL ELSE 'SELF_DECLARED_IMPORT' END,false,gender_code,'ACTIVE',
        b.source_system,$1::text,r.profile_recorded_at,$2,$2
 FROM resolved r CROSS JOIN basis b
 ON CONFLICT(msisdn_lookup_hmac) DO UPDATE SET
   encrypted_msisdn=excluded.encrypted_msisdn,
   masked_msisdn=excluded.masked_msisdn,
   country_id=CASE WHEN excluded.country_id IS NOT NULL AND excluded.profile_recorded_at>=contacts.profile_recorded_at THEN excluded.country_id ELSE contacts.country_id END,
   state_id=CASE WHEN excluded.state_id IS NOT NULL AND excluded.profile_recorded_at>=contacts.profile_recorded_at THEN excluded.state_id ELSE contacts.state_id END,
   lga_id=CASE WHEN excluded.lga_id IS NOT NULL AND excluded.profile_recorded_at>=contacts.profile_recorded_at THEN excluded.lga_id ELSE contacts.lga_id END,
   reported_age=CASE WHEN excluded.reported_age IS NOT NULL AND (contacts.age_recorded_at IS NULL OR excluded.age_recorded_at>=contacts.age_recorded_at) THEN excluded.reported_age ELSE contacts.reported_age END,
   age_recorded_at=CASE WHEN excluded.reported_age IS NOT NULL AND (contacts.age_recorded_at IS NULL OR excluded.age_recorded_at>=contacts.age_recorded_at) THEN excluded.age_recorded_at ELSE contacts.age_recorded_at END,
   age_source=CASE WHEN excluded.reported_age IS NOT NULL AND (contacts.age_recorded_at IS NULL OR excluded.age_recorded_at>=contacts.age_recorded_at) THEN excluded.age_source ELSE contacts.age_source END,
   gender_code=CASE WHEN excluded.gender_code IS NOT NULL AND excluded.profile_recorded_at>=contacts.profile_recorded_at THEN excluded.gender_code ELSE contacts.gender_code END,
   profile_recorded_at=GREATEST(contacts.profile_recorded_at,excluded.profile_recorded_at),
   source_system=excluded.source_system,source_record_id=excluded.source_record_id,updated_at=$2
 RETURNING msisdn_lookup_hmac
)
SELECT (SELECT count(*) FROM merged)-(SELECT count(*) FROM existing),(SELECT count(*) FROM existing)`

const upsertContactsTrustedSQL = resolvedStagingCTE + `,
basis AS MATERIALIZED (
 SELECT ai.organisation_id,coalesce(ai.source_system,'AUDIENCE_IMPORT') AS source_system,
        coalesce(p.trust_level,0) AS incoming_trust
 FROM audience_imports ai
 LEFT JOIN audience_source_trust_policies p ON p.organisation_id=ai.organisation_id
   AND p.source_system=upper(coalesce(ai.source_system,'AUDIENCE_IMPORT'))
 WHERE ai.id=$1::uuid
), existing AS MATERIALIZED (
 SELECT ct.msisdn_lookup_hmac FROM contacts ct JOIN resolved r ON r.msisdn_lookup_hmac=ct.msisdn_lookup_hmac
), merged AS (
 INSERT INTO contacts(encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,country_id,state_id,lga_id,reported_age,age_recorded_at,age_source,age_verified,gender_code,status,source_system,source_record_id,profile_recorded_at,created_at,updated_at)
 SELECT encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,country_id,state_id,lga_id,reported_age,age_recorded_at,
        CASE WHEN reported_age IS NULL THEN NULL ELSE 'SELF_DECLARED_IMPORT' END,false,gender_code,'ACTIVE',
        b.source_system,$1::text,r.profile_recorded_at,$2,$2
 FROM resolved r CROSS JOIN basis b
 ON CONFLICT(msisdn_lookup_hmac) DO UPDATE SET
   encrypted_msisdn=CASE WHEN (SELECT incoming_trust FROM basis)>=coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) THEN excluded.encrypted_msisdn ELSE contacts.encrypted_msisdn END,
   masked_msisdn=CASE WHEN (SELECT incoming_trust FROM basis)>=coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) THEN excluded.masked_msisdn ELSE contacts.masked_msisdn END,
   country_id=CASE WHEN excluded.country_id IS NOT NULL AND ((SELECT incoming_trust FROM basis)>coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) OR ((SELECT incoming_trust FROM basis)=coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) AND excluded.profile_recorded_at>=contacts.profile_recorded_at)) THEN excluded.country_id ELSE contacts.country_id END,
   state_id=CASE WHEN excluded.state_id IS NOT NULL AND ((SELECT incoming_trust FROM basis)>coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) OR ((SELECT incoming_trust FROM basis)=coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) AND excluded.profile_recorded_at>=contacts.profile_recorded_at)) THEN excluded.state_id ELSE contacts.state_id END,
   lga_id=CASE WHEN excluded.lga_id IS NOT NULL AND ((SELECT incoming_trust FROM basis)>coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) OR ((SELECT incoming_trust FROM basis)=coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) AND excluded.profile_recorded_at>=contacts.profile_recorded_at)) THEN excluded.lga_id ELSE contacts.lga_id END,
   reported_age=CASE WHEN excluded.reported_age IS NOT NULL AND ((SELECT incoming_trust FROM basis)>coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) OR ((SELECT incoming_trust FROM basis)=coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) AND (contacts.age_recorded_at IS NULL OR excluded.age_recorded_at>=contacts.age_recorded_at))) THEN excluded.reported_age ELSE contacts.reported_age END,
   age_recorded_at=CASE WHEN excluded.reported_age IS NOT NULL AND ((SELECT incoming_trust FROM basis)>coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) OR ((SELECT incoming_trust FROM basis)=coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) AND (contacts.age_recorded_at IS NULL OR excluded.age_recorded_at>=contacts.age_recorded_at))) THEN excluded.age_recorded_at ELSE contacts.age_recorded_at END,
   age_source=CASE WHEN excluded.reported_age IS NOT NULL AND ((SELECT incoming_trust FROM basis)>coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) OR ((SELECT incoming_trust FROM basis)=coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) AND (contacts.age_recorded_at IS NULL OR excluded.age_recorded_at>=contacts.age_recorded_at))) THEN excluded.age_source ELSE contacts.age_source END,
   gender_code=CASE WHEN excluded.gender_code IS NOT NULL AND ((SELECT incoming_trust FROM basis)>coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) OR ((SELECT incoming_trust FROM basis)=coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) AND excluded.profile_recorded_at>=contacts.profile_recorded_at)) THEN excluded.gender_code ELSE contacts.gender_code END,
   profile_recorded_at=CASE WHEN (SELECT incoming_trust FROM basis)>coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) OR ((SELECT incoming_trust FROM basis)=coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) AND excluded.profile_recorded_at>=contacts.profile_recorded_at) THEN excluded.profile_recorded_at ELSE contacts.profile_recorded_at END,
   source_system=CASE WHEN (SELECT incoming_trust FROM basis)>coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) OR ((SELECT incoming_trust FROM basis)=coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) AND excluded.profile_recorded_at>=contacts.profile_recorded_at) THEN excluded.source_system ELSE contacts.source_system END,
   source_record_id=CASE WHEN (SELECT incoming_trust FROM basis)>coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) OR ((SELECT incoming_trust FROM basis)=coalesce((SELECT trust_level FROM audience_source_trust_policies p WHERE p.organisation_id=(SELECT organisation_id FROM basis) AND p.source_system=upper(coalesce(contacts.source_system,'AUDIENCE_IMPORT'))),0) AND excluded.profile_recorded_at>=contacts.profile_recorded_at) THEN excluded.source_record_id ELSE contacts.source_record_id END,
   updated_at=$2
 RETURNING msisdn_lookup_hmac
)
SELECT (SELECT count(*) FROM merged)-(SELECT count(*) FROM existing),(SELECT count(*) FROM existing)`

const upsertContactsFillNullSQL = resolvedStagingCTE + `,
basis AS MATERIALIZED (
 SELECT coalesce(source_system,'AUDIENCE_IMPORT') AS source_system
 FROM audience_imports WHERE id=$1::uuid
), existing AS MATERIALIZED (
 SELECT ct.msisdn_lookup_hmac FROM contacts ct JOIN resolved r ON r.msisdn_lookup_hmac=ct.msisdn_lookup_hmac
), merged AS (
 INSERT INTO contacts(encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,country_id,state_id,lga_id,reported_age,age_recorded_at,age_source,age_verified,gender_code,status,source_system,source_record_id,profile_recorded_at,created_at,updated_at)
 SELECT encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,country_id,state_id,lga_id,reported_age,age_recorded_at,
        CASE WHEN reported_age IS NULL THEN NULL ELSE 'SELF_DECLARED_IMPORT' END,false,gender_code,'ACTIVE',
        b.source_system,$1::text,r.profile_recorded_at,$2,$2
 FROM resolved r CROSS JOIN basis b
 ON CONFLICT(msisdn_lookup_hmac) DO UPDATE SET
   encrypted_msisdn=excluded.encrypted_msisdn,
   masked_msisdn=excluded.masked_msisdn,
   country_id=coalesce(contacts.country_id,excluded.country_id),
   state_id=coalesce(contacts.state_id,excluded.state_id),
   lga_id=coalesce(contacts.lga_id,excluded.lga_id),
   reported_age=coalesce(contacts.reported_age,excluded.reported_age),
   age_recorded_at=CASE WHEN contacts.reported_age IS NULL THEN excluded.age_recorded_at ELSE contacts.age_recorded_at END,
   age_source=CASE WHEN contacts.reported_age IS NULL THEN excluded.age_source ELSE contacts.age_source END,
   gender_code=coalesce(contacts.gender_code,excluded.gender_code),
   profile_recorded_at=GREATEST(contacts.profile_recorded_at,excluded.profile_recorded_at),
   source_system=excluded.source_system,source_record_id=excluded.source_record_id,updated_at=$2
 RETURNING msisdn_lookup_hmac
)
SELECT (SELECT count(*) FROM merged)-(SELECT count(*) FROM existing),(SELECT count(*) FROM existing)`

const insertContactSourcesSQL = `
WITH candidates AS MATERIALIZED (
 SELECT ct.id AS contact_id,ai.organisation_id,ai.id AS audience_import_id,
        ai.source_system,s.row_number::text AS source_record_id,
        s.source_record_hash,$2::timestamptz AS observed_at
 FROM audience_import_staging s
 JOIN audience_imports ai ON ai.id=s.audience_import_id
 JOIN contacts ct ON ct.msisdn_lookup_hmac=s.msisdn_lookup_hmac
 WHERE s.audience_import_id=$1::uuid
), existing AS MATERIALIZED (
 SELECT cs.id
 FROM contact_sources cs
 JOIN candidates c ON c.contact_id=cs.contact_id
   AND c.organisation_id=cs.organisation_id
   AND c.source_record_hash=cs.source_record_hash
), inserted AS (
 INSERT INTO contact_sources(contact_id,organisation_id,audience_import_id,source_system,source_record_id,source_record_hash,first_seen_at,last_seen_at)
 SELECT contact_id,organisation_id,audience_import_id,source_system,source_record_id,source_record_hash,observed_at,observed_at
 FROM candidates
 ON CONFLICT(contact_id,organisation_id,source_record_hash) DO NOTHING
 RETURNING 1
), refreshed AS (
 UPDATE contact_sources cs
 SET audience_import_id=c.audience_import_id,
     source_system=c.source_system,
     source_record_id=c.source_record_id,
     last_seen_at=GREATEST(cs.last_seen_at,c.observed_at)
 FROM candidates c,existing e
 WHERE cs.id=e.id AND cs.contact_id=c.contact_id
   AND cs.organisation_id=c.organisation_id
   AND cs.source_record_hash=c.source_record_hash
 RETURNING 1
)
SELECT count(*) FROM inserted`

const insertProfileHistorySQL = resolvedStagingCTE + `,
inserted AS (
 INSERT INTO contact_profile_history(contact_id,audience_import_id,reported_age,age_recorded_at,profile_recorded_at,gender_code,country_id,state_id,lga_id,source_record_hash,recorded_at)
 SELECT ct.id,$1::uuid,r.reported_age,r.age_recorded_at,r.profile_recorded_at,r.gender_code,r.country_id,r.state_id,r.lga_id,r.source_record_hash,$2
 FROM resolved r JOIN contacts ct ON ct.msisdn_lookup_hmac=r.msisdn_lookup_hmac
 ON CONFLICT(contact_id,audience_import_id,source_record_hash) DO NOTHING
 RETURNING 1
)
SELECT count(*) FROM inserted`

const insertConsentGrantsSQL = `
WITH inserted AS (
 INSERT INTO consent_grants(contact_id,organisation_id,purpose_id,channel,wording_version,source_type,source_reference,granted_at,expires_at,status,reviewed_by,reviewed_at,source_import_id,created_at,updated_at)
 SELECT ct.id,ai.organisation_id,ai.purpose_id,upper(ai.channel),ai.wording_version,'AUDIENCE_IMPORT',ai.id::text,
        coalesce(ai.granted_at,ai.approved_at,ai.created_at),ai.expires_at,'ACTIVE',ai.approved_by,ai.approved_at,ai.id,$2,$2
 FROM audience_import_staging s
 JOIN audience_imports ai ON ai.id=s.audience_import_id
 JOIN contacts ct ON ct.msisdn_lookup_hmac=s.msisdn_lookup_hmac
 WHERE s.audience_import_id=$1::uuid
 ON CONFLICT(contact_id,purpose_id,channel,source_import_id) WHERE source_import_id IS NOT NULL DO NOTHING
 RETURNING 1
)
SELECT count(*) FROM inserted`

const insertProfileConflictsSQL = resolvedStagingCTE + `,
conflicts AS (
 INSERT INTO audience_profile_conflicts(audience_import_id,contact_id,masked_msisdn,field_name,existing_value,incoming_value,status,version,created_at)
 SELECT $1::uuid,ct.id,r.masked_msisdn,v.field_name,v.existing_value,v.incoming_value,'PENDING',1,$2
 FROM resolved r
 JOIN contacts ct ON ct.msisdn_lookup_hmac=r.msisdn_lookup_hmac
 CROSS JOIN LATERAL (VALUES
   ('country_id',ct.country_id::text,r.country_id::text),
   ('state_id',ct.state_id::text,r.state_id::text),
   ('lga_id',ct.lga_id::text,r.lga_id::text),
   ('reported_age',ct.reported_age::text,r.reported_age::text),
   ('gender_code',ct.gender_code,r.gender_code)
 ) AS v(field_name,existing_value,incoming_value)
 WHERE v.existing_value IS NOT NULL AND v.incoming_value IS NOT NULL AND v.existing_value<>v.incoming_value
 ON CONFLICT(audience_import_id,contact_id,field_name) DO NOTHING
 RETURNING 1
) SELECT count(*) FROM conflicts`

const insertContactsOnlySQL = resolvedStagingCTE + `,
basis AS MATERIALIZED (
 SELECT coalesce(source_system,'AUDIENCE_IMPORT') AS source_system
 FROM audience_imports WHERE id=$1::uuid
), inserted AS (
 INSERT INTO contacts(encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,country_id,state_id,lga_id,reported_age,age_recorded_at,age_source,age_verified,gender_code,status,source_system,source_record_id,profile_recorded_at,created_at,updated_at)
 SELECT encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,country_id,state_id,lga_id,reported_age,age_recorded_at,
        CASE WHEN reported_age IS NULL THEN NULL ELSE 'SELF_DECLARED_IMPORT' END,false,gender_code,'ACTIVE',
        b.source_system,$1::text,r.profile_recorded_at,$2,$2
 FROM resolved r CROSS JOIN basis b
 ON CONFLICT(msisdn_lookup_hmac) DO NOTHING
 RETURNING 1
) SELECT count(*),0 FROM inserted`
