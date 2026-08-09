package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/consent"
)

type ConsentRepository struct{ DB *sql.DB }

func (r *ConsentRepository) Create(ctx context.Context, review consent.Review) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err = insertConsentReview(ctx, tx, review); err != nil {
		return err
	}
	if err = insertConsentEvidence(ctx, tx, review); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO consent_review_events(
  consent_review_id,event_type,actor_id,reason,review_version,evidence
) VALUES(
  $1::uuid,'CREATED',$2::uuid,'consent review created',$3,
  jsonb_build_object('scope',$4::text,'campaignId',nullif($5::text,''),'sourceSystem',nullif($6::text,''))
)`, review.ID, review.CreatedBy, review.Version, review.Scope, review.CampaignID, review.SourceSystem)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func insertConsentReview(ctx context.Context, tx *sql.Tx, review consent.Review) error {
	countries, err := json.Marshal(review.PermittedCountries)
	if err != nil {
		return err
	}
	partners, err := json.Marshal(review.PermittedPartnerOrganisations)
	if err != nil {
		return err
	}
	externalEvidence, err := json.Marshal(review.ExternalEvidenceReferences)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO consent_reviews(
  id,organisation_id,name,purpose_description,channel,consent_source,wording_version,
  privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,
  permitted_country_iso2,permitted_message_category,restrictions,status,
  created_at,updated_at,version,decision_reason,
  review_scope,campaign_id,source_system,collection_method,collection_period_from,
  collection_period_to,controller_role,permitted_purpose_code,
  permitted_partner_organisations,exact_consent_wording,privacy_notice_version,
  external_evidence_references,sample_review_notes,outcome,
  parent_review_id,created_by,submitted_by,next_review_at
) VALUES(
  $1::uuid,$2::uuid,$3,NULLIF($4::text,''),$5,$6,$7,
  $8,$9,$10,
  ARRAY(SELECT value::char(2) FROM jsonb_array_elements_text($11::jsonb) value),
  NULLIF($12::text,''),NULLIF($13::text,''),$14,
  $15,$15,$16,NULLIF($17,''),
  $18,NULLIF($19::text,'')::uuid,NULLIF($20::text,''),NULLIF($21::text,''),$22,$23,
  NULLIF($24::text,''),NULLIF($25::text,''),$26::jsonb,NULLIF($27::text,''),NULLIF($28::text,''),
  $29::jsonb,NULLIF($30::text,''),$31,
  NULLIF($32::text,'')::uuid,$33::uuid,NULLIF($34::text,'')::uuid,$35
)`,
		review.ID, review.OrganisationID, review.Name, review.PurposeDescription, review.Channel,
		review.ConsentSource, review.WordingVersion, review.PrivacyNoticeReviewed,
		review.OptOutProcessReviewed, review.SampleRecordsReviewed, string(countries),
		review.PermittedMessageCategory, review.Restrictions, review.Status, review.CreatedAt,
		review.Version, review.LastTransitionReason, review.Scope, review.CampaignID,
		review.SourceSystem, review.CollectionMethod, dateValue(review.CollectionPeriodFrom),
		dateValue(review.CollectionPeriodTo), review.ControllerRole, review.PurposeCode,
		string(partners), review.ExactConsentWording, review.PrivacyNoticeVersion,
		string(externalEvidence), review.SampleReviewNotes, review.Outcome,
		review.ParentReviewID, review.CreatedBy, review.SubmittedBy, review.NextReviewAt,
	)
	return err
}

func insertConsentEvidence(ctx context.Context, tx *sql.Tx, review consent.Review) error {
	for _, assetID := range review.EvidenceAssetIDs {
		result, err := tx.ExecContext(ctx, `
INSERT INTO consent_review_evidence(
  consent_review_id,object_key,original_filename,media_type,byte_size,sha256_checksum,
  malware_scan_status,uploaded_by,trusted_asset_id
)
SELECT $1::uuid,a.object_key,a.original_filename,a.media_type,a.byte_size,a.sha256_checksum,
       'CLEAN',$3::uuid,a.id
FROM trusted_assets a
WHERE a.id=$2::uuid AND a.purpose='CONSENT_EVIDENCE' AND a.status='CLEAN'
ON CONFLICT(consent_review_id,sha256_checksum) DO NOTHING`, review.ID, assetID, review.CreatedBy)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("trusted consent evidence %s is missing, unclean or duplicate", assetID)
		}
	}
	return nil
}

func (r *ConsentRepository) CompareAndSwap(ctx context.Context, review consent.Review, expected int64) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	countries, err := json.Marshal(review.PermittedCountries)
	if err != nil {
		return err
	}
	partners, err := json.Marshal(review.PermittedPartnerOrganisations)
	if err != nil {
		return err
	}
	var updated int
	err = r.DB.QueryRowContext(ctx, `
WITH changed AS (
  UPDATE consent_reviews SET
    status=$2,
    outcome=$3,
    submitted_by=NULLIF($4,'')::uuid,
    reviewed_by=NULLIF($5,'')::uuid,
    reviewed_at=$6,
    expires_at=$7,
    next_review_at=$8,
    restrictions=NULLIF($9,''),
    permitted_country_iso2=ARRAY(SELECT value::char(2) FROM jsonb_array_elements_text($10::jsonb) value),
    permitted_partner_organisations=$11::jsonb,
    version=$12,
    updated_at=$13,
    revoked_by=NULLIF($14,'')::uuid,
    revoked_at=$15,
    revoke_reason=NULLIF($16,''),
    superseded_by_id=NULLIF($17,'')::uuid,
    decision_reason=NULLIF($18,'')
  WHERE id=$1::uuid AND version=$19
  RETURNING id,status,version,coalesce(revoked_by,reviewed_by,submitted_by,created_by) AS actor
), event AS (
  INSERT INTO consent_review_events(
    consent_review_id,event_type,actor_id,reason,review_version,evidence
  )
  SELECT id,
    CASE
      WHEN status='APPROVED' AND $3='APPROVED_WITH_RESTRICTIONS' THEN 'APPROVED_WITH_RESTRICTIONS'
      WHEN status='APPROVED' THEN 'APPROVED'
      WHEN status='REJECTED' THEN 'REJECTED'
      WHEN status='REVOKED' THEN 'REVOKED'
      WHEN status='SUPERSEDED' THEN 'SUPERSEDED'
      WHEN status='PENDING_REVIEW' THEN 'SUBMITTED'
      ELSE 'EXPIRED'
    END,
    actor,$18,version,jsonb_build_object('outcome',$3,'expiresAt',$7)
  FROM changed
)
SELECT count(*) FROM changed`,
		review.ID, review.Status, review.Outcome, review.SubmittedBy, review.ReviewedBy,
		review.ReviewedAt, review.ExpiresAt, review.NextReviewAt, review.Restrictions,
		string(countries), string(partners), review.Version, review.UpdatedAt,
		review.RevokedBy, review.RevokedAt, review.RevokeReason, review.SupersededByID,
		review.LastTransitionReason, expected,
	).Scan(&updated)
	if err != nil {
		return err
	}
	if updated != 1 {
		return consent.ErrConflict
	}
	return nil
}

func (r *ConsentRepository) CreateRevision(ctx context.Context, previous, replacement consent.Review, expected int64) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
UPDATE consent_reviews SET
  status='SUPERSEDED',superseded_by_id=$3::uuid,version=$4,updated_at=$5,
  decision_reason=$6
WHERE id=$1::uuid AND version=$2 AND status='APPROVED'`,
		previous.ID, expected, replacement.ID, previous.Version, previous.UpdatedAt,
		previous.LastTransitionReason,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return consent.ErrConflict
	}
	if err = insertConsentReview(ctx, tx, replacement); err != nil {
		return err
	}
	if err = insertConsentEvidence(ctx, tx, replacement); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO consent_review_events(
  consent_review_id,event_type,actor_id,reason,review_version,evidence
) VALUES
  ($1::uuid,'SUPERSEDED',$2::uuid,$3,$4,jsonb_build_object('replacementId',$5::text)),
  ($5::uuid,'CREATED',$2::uuid,$6,$7,jsonb_build_object('parentId',$1::text))`,
		previous.ID, replacement.CreatedBy, previous.LastTransitionReason, previous.Version,
		replacement.ID, replacement.LastTransitionReason, replacement.Version,
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *ConsentRepository) List(ctx context.Context, organisationID string) ([]consent.Review, error) {
	query := consentSelect
	args := []any{}
	if strings.TrimSpace(organisationID) != "" {
		query += ` WHERE cr.organisation_id=$1::uuid`
		args = append(args, organisationID)
	}
	query += ` ORDER BY cr.created_at DESC,cr.id DESC LIMIT 2000`
	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []consent.Review{}
	for rows.Next() {
		review, err := scanConsent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, review)
	}
	return items, rows.Err()
}

func (r *ConsentRepository) ListReviewPage(ctx context.Context, organisationID string, limit int, before *time.Time, beforeID string) ([]consent.Review, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, consentSelect+` WHERE ($1::text='' OR cr.organisation_id=NULLIF($1::text,'')::uuid) AND ($3::timestamptz IS NULL OR cr.created_at<$3 OR (cr.created_at=$3 AND cr.id<NULLIF($4::text,'')::uuid)) ORDER BY cr.created_at DESC,cr.id DESC LIMIT $2`, organisationID, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]consent.Review, 0, limit)
	for rows.Next() {
		review, scanErr := scanConsent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, review)
	}
	return out, rows.Err()
}

func (r *ConsentRepository) Get(ctx context.Context, id string) (consent.Review, error) {
	review, err := scanConsent(r.DB.QueryRowContext(ctx, consentSelect+` WHERE cr.id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return consent.Review{}, consent.ErrNotFound
	}
	return review, err
}

const consentSelect = `SELECT
  cr.id::text,cr.organisation_id::text,cr.review_scope,coalesce(cr.campaign_id::text,''),
  coalesce(cr.source_system,''),cr.name,coalesce(cr.purpose_description,''),
  coalesce(cr.permitted_purpose_code,''),cr.channel,cr.consent_source,
  coalesce(cr.collection_method,''),cr.collection_period_from,cr.collection_period_to,
  coalesce(cr.controller_role,''),cr.wording_version,coalesce(cr.exact_consent_wording,''),
  coalesce(cr.privacy_notice_version,''),cr.external_evidence_references::text,
  coalesce(cr.sample_review_notes,''),
  coalesce((SELECT jsonb_agg(e.trusted_asset_id::text ORDER BY e.uploaded_at)
            FILTER(WHERE e.trusted_asset_id IS NOT NULL)
            FROM consent_review_evidence e WHERE e.consent_review_id=cr.id),'[]'::jsonb)::text,
  coalesce((SELECT jsonb_agg(e.object_key ORDER BY e.uploaded_at)
            FROM consent_review_evidence e WHERE e.consent_review_id=cr.id),'[]'::jsonb)::text,
  cr.privacy_notice_reviewed,cr.opt_out_process_reviewed,cr.sample_records_reviewed,
  to_jsonb(cr.permitted_country_iso2)::text,coalesce(cr.permitted_message_category,''),
  cr.permitted_partner_organisations::text,cr.status,cr.outcome,coalesce(cr.restrictions,''),
  coalesce(cr.created_by::text,''),coalesce(cr.submitted_by::text,''),
  coalesce(cr.reviewed_by::text,''),cr.reviewed_at,cr.expires_at,cr.next_review_at,
  coalesce(cr.parent_review_id::text,''),coalesce(cr.superseded_by_id::text,''),
  coalesce(cr.revoked_by::text,''),cr.revoked_at,coalesce(cr.revoke_reason,''),
  coalesce(cr.decision_reason,''),cr.created_at,cr.updated_at,cr.version
FROM consent_reviews cr`

type consentScanner interface{ Scan(...any) error }

func scanConsent(row consentScanner) (consent.Review, error) {
	var review consent.Review
	var scope, status, outcome string
	var externalEvidence, assetIDs, evidenceKeys, countries, partners string
	var from, to, reviewedAt, expiresAt, nextReviewAt, revokedAt sql.NullTime
	err := row.Scan(
		&review.ID, &review.OrganisationID, &scope, &review.CampaignID, &review.SourceSystem,
		&review.Name, &review.PurposeDescription, &review.PurposeCode, &review.Channel,
		&review.ConsentSource, &review.CollectionMethod, &from, &to, &review.ControllerRole,
		&review.WordingVersion, &review.ExactConsentWording, &review.PrivacyNoticeVersion,
		&externalEvidence, &review.SampleReviewNotes, &assetIDs, &evidenceKeys, &review.PrivacyNoticeReviewed, &review.OptOutProcessReviewed,
		&review.SampleRecordsReviewed, &countries, &review.PermittedMessageCategory, &partners,
		&status, &outcome, &review.Restrictions, &review.CreatedBy, &review.SubmittedBy,
		&review.ReviewedBy, &reviewedAt, &expiresAt, &nextReviewAt, &review.ParentReviewID,
		&review.SupersededByID, &review.RevokedBy, &revokedAt, &review.RevokeReason,
		&review.LastTransitionReason, &review.CreatedAt, &review.UpdatedAt, &review.Version,
	)
	if err != nil {
		return consent.Review{}, err
	}
	review.Scope = consent.ReviewScope(scope)
	review.Status = consent.ReviewStatus(status)
	review.Outcome = consent.ReviewOutcome(outcome)
	if err = json.Unmarshal([]byte(externalEvidence), &review.ExternalEvidenceReferences); err != nil {
		return consent.Review{}, err
	}
	for raw, target := range map[string]*[]string{
		assetIDs:     &review.EvidenceAssetIDs,
		evidenceKeys: &review.EvidenceObjectKeys,
		countries:    &review.PermittedCountries,
		partners:     &review.PermittedPartnerOrganisations,
	} {
		if err = json.Unmarshal([]byte(raw), target); err != nil {
			return consent.Review{}, err
		}
	}
	if from.Valid {
		value := from.Time
		review.CollectionPeriodFrom = &value
	}
	if to.Valid {
		value := to.Time
		review.CollectionPeriodTo = &value
	}
	if reviewedAt.Valid {
		value := reviewedAt.Time
		review.ReviewedAt = &value
	}
	if expiresAt.Valid {
		value := expiresAt.Time
		review.ExpiresAt = &value
	}
	if nextReviewAt.Valid {
		value := nextReviewAt.Time
		review.NextReviewAt = &value
	}
	if revokedAt.Valid {
		value := revokedAt.Time
		review.RevokedAt = &value
	}
	return review, nil
}

func dateValue(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format("2006-01-02")
}
