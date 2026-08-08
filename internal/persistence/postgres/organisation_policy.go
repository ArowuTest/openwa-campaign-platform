package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"campaign-platform/internal/organisation"
)

type OrganisationPolicyRepository struct{ DB *sql.DB }

func (r *OrganisationPolicyRepository) List(ctx context.Context, organisationID string) ([]organisation.Policy, error) {
	rows, err := r.DB.QueryContext(ctx, policySelect+` WHERE organisation_id=$1::uuid ORDER BY created_at DESC LIMIT 1000`, organisationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []organisation.Policy{}
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r *OrganisationPolicyRepository) ListPolicyPage(ctx context.Context, organisationID string, limit int, before *time.Time, beforeID string) ([]organisation.Policy, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, policySelect+` WHERE organisation_id=$1::uuid AND ($3::timestamptz IS NULL OR created_at<$3 OR (created_at=$3 AND id<NULLIF($4::text,'')::uuid)) ORDER BY created_at DESC,id DESC LIMIT $2`, organisationID, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]organisation.Policy, 0, limit)
	for rows.Next() {
		p, scanErr := scanPolicy(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *OrganisationPolicyRepository) Get(ctx context.Context, id string) (organisation.Policy, error) {
	p, err := scanPolicy(r.DB.QueryRowContext(ctx, policySelect+` WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return organisation.Policy{}, organisation.ErrPolicyNotFound
	}
	return p, err
}
func (r *OrganisationPolicyRepository) Active(ctx context.Context, org string, at time.Time) (organisation.Policy, error) {
	p, err := scanPolicy(r.DB.QueryRowContext(ctx, policySelect+` WHERE organisation_id=$1::uuid AND status='ACTIVE' AND effective_from<=$2 AND (effective_to IS NULL OR effective_to>$2) ORDER BY effective_from DESC LIMIT 1`, org, at))
	if errors.Is(err, sql.ErrNoRows) {
		return organisation.Policy{}, organisation.ErrPolicyNotFound
	}
	return p, err
}
func (r *OrganisationPolicyRepository) Create(ctx context.Context, p organisation.Policy) (organisation.Policy, error) {
	a, err := json.Marshal(p.AllowedPurposeIDs)
	if err != nil {
		return organisation.Policy{}, err
	}
	b, err := json.Marshal(p.ProhibitedPurposeIDs)
	if err != nil {
		return organisation.Policy{}, err
	}
	c, err := json.Marshal(p.FrequencyCaps)
	if err != nil {
		return organisation.Policy{}, err
	}
	_, err = r.DB.ExecContext(ctx, `INSERT INTO organisation_policy_versions(id,organisation_id,allowed_purpose_ids,prohibited_purpose_ids,frequency_caps,contact_retention_days,campaign_retention_days,report_brand_name,report_footer,status,effective_from,effective_to,version,created_by,submitted_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::jsonb,$4::jsonb,$5::jsonb,$6,$7,NULLIF($8,''),NULLIF($9,''),$10,$11,$12,$13,NULLIF($14,'')::uuid,NULLIF($15,'')::uuid,NULLIF($16,'')::uuid,$17,$18,$19)`, p.ID, p.OrganisationID, string(a), string(b), string(c), p.ContactRetentionDays, p.CampaignRetentionDays, p.ReportBrandName, p.ReportFooter, p.Status, p.EffectiveFrom, p.EffectiveTo, p.Version, p.CreatedBy, p.SubmittedBy, p.ApprovedBy, p.Reason, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return organisation.Policy{}, err
	}
	return p, nil
}
func (r *OrganisationPolicyRepository) CompareAndSwap(ctx context.Context, p organisation.Policy, expected int64) (organisation.Policy, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return organisation.Policy{}, err
	}
	defer tx.Rollback()
	if p.Status == organisation.PolicyActive {
		_, err = tx.ExecContext(ctx, `UPDATE organisation_policy_versions SET status='RETIRED',effective_to=$2,version=version+1,updated_at=$3 WHERE organisation_id=$1::uuid AND status='ACTIVE' AND id<>$4::uuid`, p.OrganisationID, p.EffectiveFrom, p.UpdatedAt, p.ID)
		if err != nil {
			return organisation.Policy{}, err
		}
	}
	a, err := json.Marshal(p.AllowedPurposeIDs)
	if err != nil {
		return organisation.Policy{}, err
	}
	b, err := json.Marshal(p.ProhibitedPurposeIDs)
	if err != nil {
		return organisation.Policy{}, err
	}
	c, err := json.Marshal(p.FrequencyCaps)
	if err != nil {
		return organisation.Policy{}, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE organisation_policy_versions SET allowed_purpose_ids=$2::jsonb,prohibited_purpose_ids=$3::jsonb,frequency_caps=$4::jsonb,contact_retention_days=$5,campaign_retention_days=$6,report_brand_name=NULLIF($7,''),report_footer=NULLIF($8,''),status=$9,effective_from=$10,effective_to=$11,version=$12,submitted_by=NULLIF($13,'')::uuid,approved_by=NULLIF($14,'')::uuid,reason=$15,updated_at=$16 WHERE id=$1::uuid AND version=$17`, p.ID, string(a), string(b), string(c), p.ContactRetentionDays, p.CampaignRetentionDays, p.ReportBrandName, p.ReportFooter, p.Status, p.EffectiveFrom, p.EffectiveTo, p.Version, p.SubmittedBy, p.ApprovedBy, p.Reason, p.UpdatedAt, expected)
	if err != nil {
		return organisation.Policy{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return organisation.Policy{}, err
	}
	if n != 1 {
		return organisation.Policy{}, organisation.ErrPolicyConflict
	}
	if err = tx.Commit(); err != nil {
		return organisation.Policy{}, err
	}
	return p, nil
}

const policySelect = `SELECT id::text,organisation_id::text,allowed_purpose_ids::text,prohibited_purpose_ids::text,frequency_caps::text,contact_retention_days,campaign_retention_days,coalesce(report_brand_name,''),coalesce(report_footer,''),status,effective_from,effective_to,version,coalesce(created_by::text,''),coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,created_at,updated_at FROM organisation_policy_versions`

type policyScanner interface{ Scan(...any) error }

func scanPolicy(row policyScanner) (organisation.Policy, error) {
	var p organisation.Policy
	var allowed, prohibited, caps, status string
	err := row.Scan(&p.ID, &p.OrganisationID, &allowed, &prohibited, &caps, &p.ContactRetentionDays, &p.CampaignRetentionDays, &p.ReportBrandName, &p.ReportFooter, &status, &p.EffectiveFrom, &p.EffectiveTo, &p.Version, &p.CreatedBy, &p.SubmittedBy, &p.ApprovedBy, &p.Reason, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return p, err
	}
	p.Status = organisation.PolicyStatus(status)
	if err := json.Unmarshal([]byte(allowed), &p.AllowedPurposeIDs); err != nil {
		return p, err
	}
	if err := json.Unmarshal([]byte(prohibited), &p.ProhibitedPurposeIDs); err != nil {
		return p, err
	}
	if err := json.Unmarshal([]byte(caps), &p.FrequencyCaps); err != nil {
		return p, err
	}
	return p, nil
}
