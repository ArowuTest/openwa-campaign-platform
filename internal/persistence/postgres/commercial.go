package postgres

import (
	"context"
	"database/sql"
	"errors"

	"campaign-platform/internal/commercial"
)

type CommercialRepository struct{ DB *sql.DB }

const commercialSelect = `SELECT id::text,campaign_id::text,organisation_id::text,quotation_reference,invoice_reference,currency,approved_recipients,unit_price_minor,management_fee_minor,total_amount_minor,coalesce(payment_reference,''),payment_received_at,status,version,coalesce(created_by::text,''),coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,created_at,updated_at FROM campaign_commercial_approvals`

type commercialScanner interface{ Scan(...any) error }

func scanCommercial(row commercialScanner) (commercial.Record, error) {
	var r commercial.Record
	var status string
	err := row.Scan(&r.ID, &r.CampaignID, &r.OrganisationID, &r.QuotationReference, &r.InvoiceReference, &r.Currency, &r.ApprovedRecipients, &r.UnitPriceMinor, &r.ManagementFeeMinor, &r.TotalAmountMinor, &r.PaymentReference, &r.PaymentReceivedAt, &status, &r.Version, &r.CreatedBy, &r.SubmittedBy, &r.ApprovedBy, &r.Reason, &r.CreatedAt, &r.UpdatedAt)
	r.Status = commercial.Status(status)
	return r, err
}
func (r *CommercialRepository) Create(ctx context.Context, v commercial.Record) (commercial.Record, error) {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO campaign_commercial_approvals(id,campaign_id,organisation_id,quotation_reference,invoice_reference,currency,approved_recipients,unit_price_minor,management_fee_minor,total_amount_minor,payment_reference,payment_received_at,status,version,created_by,submitted_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),$12,$13,$14,NULLIF($15,'')::uuid,NULLIF($16,'')::uuid,NULLIF($17,'')::uuid,$18,$19,$20)`, v.ID, v.CampaignID, v.OrganisationID, v.QuotationReference, v.InvoiceReference, v.Currency, v.ApprovedRecipients, v.UnitPriceMinor, v.ManagementFeeMinor, v.TotalAmountMinor, v.PaymentReference, v.PaymentReceivedAt, v.Status, v.Version, v.CreatedBy, v.SubmittedBy, v.ApprovedBy, v.Reason, v.CreatedAt, v.UpdatedAt)
	return v, err
}
func (r *CommercialRepository) Get(ctx context.Context, id string) (commercial.Record, error) {
	v, err := scanCommercial(r.DB.QueryRowContext(ctx, commercialSelect+` WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return commercial.Record{}, commercial.ErrNotFound
	}
	return v, err
}
func (r *CommercialRepository) GetByCampaign(ctx context.Context, id string) (commercial.Record, error) {
	v, err := scanCommercial(r.DB.QueryRowContext(ctx, commercialSelect+` WHERE campaign_id=$1::uuid ORDER BY created_at DESC LIMIT 1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return commercial.Record{}, commercial.ErrNotFound
	}
	return v, err
}
func (r *CommercialRepository) List(ctx context.Context, org string) ([]commercial.Record, error) {
	q := commercialSelect
	args := []any{}
	if org != "" {
		q += ` WHERE organisation_id=$1::uuid`
		args = append(args, org)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := r.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []commercial.Record{}
	for rows.Next() {
		v, e := scanCommercial(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *CommercialRepository) CompareAndSwap(ctx context.Context, v commercial.Record, expected int64) (commercial.Record, error) {
	res, err := r.DB.ExecContext(ctx, `UPDATE campaign_commercial_approvals SET payment_reference=NULLIF($2,''),payment_received_at=$3,status=$4,version=$5,submitted_by=NULLIF($6,'')::uuid,approved_by=NULLIF($7,'')::uuid,reason=$8,updated_at=$9 WHERE id=$1::uuid AND version=$10`, v.ID, v.PaymentReference, v.PaymentReceivedAt, v.Status, v.Version, v.SubmittedBy, v.ApprovedBy, v.Reason, v.UpdatedAt, expected)
	if err != nil {
		return commercial.Record{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return commercial.Record{}, commercial.ErrConflict
	}
	return v, nil
}
