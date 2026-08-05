package postgres

import (
	"campaign-platform/internal/consent"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

type ConsentRepository struct{ DB *sql.DB }

func (r *ConsentRepository) Create(ctx context.Context, v consent.Review) error {
	if r.DB == nil {
		return errors.New("database is required")
	}
	countries, err := json.Marshal(v.PermittedCountries)
	if err != nil {
		return err
	}
	evidence, err := json.Marshal(v.EvidenceObjectKeys)
	if err != nil {
		return err
	}
	const q = `INSERT INTO consent_reviews(id,organisation_id,name,purpose_description,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,permitted_country_iso2,permitted_message_category,restrictions,status,created_at,updated_at,version,decision_reason) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9,$10,ARRAY(SELECT jsonb_array_elements_text($11::jsonb)),NULLIF($12,''),NULLIF($13,''),$14,$15,$15,$16,NULL) RETURNING id`
	var id string
	err = r.DB.QueryRowContext(ctx, q, v.ID, v.OrganisationID, v.Name, v.PurposeDescription, v.Channel, v.ConsentSource, v.WordingVersion, v.PrivacyNoticeReviewed, v.OptOutProcessReviewed, v.SampleRecordsReviewed, countries, v.PermittedMessageCategory, v.Restrictions, v.Status, v.CreatedAt, v.Version).Scan(&id)
	if err != nil {
		return err
	}
	if len(v.EvidenceObjectKeys) > 0 {
		_, err = r.DB.ExecContext(ctx, `INSERT INTO consent_review_evidence(consent_review_id,object_key,sha256_checksum,malware_scan_status) SELECT $1,value,encode(digest(value,'sha256'),'hex'),'PENDING' FROM jsonb_array_elements_text($2::jsonb) value`, v.ID, evidence)
	}
	return err
}
func (r *ConsentRepository) CompareAndSwap(ctx context.Context, v consent.Review, expected int64) error {
	if r.DB == nil {
		return errors.New("database is required")
	}
	countries, err := json.Marshal(v.PermittedCountries)
	if err != nil {
		return err
	}
	const q = `UPDATE consent_reviews SET status=$2,reviewed_by=NULLIF($3,'')::uuid,reviewed_at=$4,expires_at=$5,restrictions=NULLIF($6,''),permitted_country_iso2=ARRAY(SELECT jsonb_array_elements_text($7::jsonb)),version=$8,updated_at=$9 WHERE id=$1 AND version=$10`
	res, err := r.DB.ExecContext(ctx, q, v.ID, v.Status, v.ReviewedBy, v.ReviewedAt, v.ExpiresAt, v.Restrictions, countries, v.Version, v.UpdatedAt, expected)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return consent.ErrConflict
	}
	return nil
}
func (r *ConsentRepository) List(ctx context.Context, orgID string) ([]consent.Review, error) {
	query := consentSelect
	args := []any{}
	if strings.TrimSpace(orgID) != "" {
		query += ` WHERE cr.organisation_id=$1`
		args = append(args, orgID)
	}
	query += ` ORDER BY cr.created_at DESC`
	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []consent.Review{}
	for rows.Next() {
		v, err := scanConsent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *ConsentRepository) Get(ctx context.Context, id string) (consent.Review, error) {
	v, err := scanConsent(r.DB.QueryRowContext(ctx, consentSelect+` WHERE cr.id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return consent.Review{}, consent.ErrNotFound
	}
	return v, err
}

const consentSelect = `SELECT cr.id,cr.organisation_id,cr.name,coalesce(cr.purpose_description,''),cr.channel,cr.consent_source,cr.wording_version,coalesce((SELECT jsonb_agg(e.object_key ORDER BY e.uploaded_at) FROM consent_review_evidence e WHERE e.consent_review_id=cr.id),'[]'::jsonb)::text,cr.privacy_notice_reviewed,cr.opt_out_process_reviewed,cr.sample_records_reviewed,to_jsonb(cr.permitted_country_iso2)::text,coalesce(cr.permitted_message_category,''),cr.status,coalesce(cr.restrictions,''),coalesce(cr.reviewed_by::text,''),cr.reviewed_at,cr.expires_at,cr.created_at,cr.updated_at,cr.version FROM consent_reviews cr`

func scanConsent(row scanner) (consent.Review, error) {
	var v consent.Review
	var status, evidence, countries string
	var reviewed, expires sql.NullTime
	err := row.Scan(&v.ID, &v.OrganisationID, &v.Name, &v.PurposeDescription, &v.Channel, &v.ConsentSource, &v.WordingVersion, &evidence, &v.PrivacyNoticeReviewed, &v.OptOutProcessReviewed, &v.SampleRecordsReviewed, &countries, &v.PermittedMessageCategory, &status, &v.Restrictions, &v.ReviewedBy, &reviewed, &expires, &v.CreatedAt, &v.UpdatedAt, &v.Version)
	if err != nil {
		return consent.Review{}, err
	}
	v.Status = consent.ReviewStatus(status)
	if err := json.Unmarshal([]byte(evidence), &v.EvidenceObjectKeys); err != nil {
		return consent.Review{}, err
	}
	if err := json.Unmarshal([]byte(countries), &v.PermittedCountries); err != nil {
		return consent.Review{}, err
	}
	if reviewed.Valid {
		t := reviewed.Time
		v.ReviewedAt = &t
	}
	if expires.Valid {
		t := expires.Time
		v.ExpiresAt = &t
	}
	return v, nil
}
