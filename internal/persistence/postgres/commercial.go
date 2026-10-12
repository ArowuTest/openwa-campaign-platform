package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

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

type commercialExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertCommercialEvent(ctx context.Context, exec commercialExecer, event commercial.Event) error {
	raw, err := json.Marshal(event.Evidence)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `INSERT INTO campaign_commercial_events(id,commercial_record_id,event_type,actor_id,reason,record_version,evidence,occurred_at) VALUES($1::uuid,$2::uuid,$3,$4::uuid,$5,$6,$7::jsonb,$8)`,
		event.ID, event.CommercialRecordID, event.EventType, event.ActorID, event.Reason, event.RecordVersion, string(raw), event.OccurredAt)
	return err
}
func (r *CommercialRepository) Create(ctx context.Context, v commercial.Record) (commercial.Record, error) {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO campaign_commercial_approvals(id,campaign_id,organisation_id,quotation_reference,invoice_reference,currency,approved_recipients,unit_price_minor,management_fee_minor,total_amount_minor,payment_reference,payment_received_at,status,version,created_by,submitted_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),$12,$13,$14,NULLIF($15,'')::uuid,NULLIF($16,'')::uuid,NULLIF($17,'')::uuid,$18,$19,$20)`, v.ID, v.CampaignID, v.OrganisationID, v.QuotationReference, v.InvoiceReference, v.Currency, v.ApprovedRecipients, v.UnitPriceMinor, v.ManagementFeeMinor, v.TotalAmountMinor, v.PaymentReference, v.PaymentReceivedAt, v.Status, v.Version, v.CreatedBy, v.SubmittedBy, v.ApprovedBy, v.Reason, v.CreatedAt, v.UpdatedAt)
	return v, err
}

func (r *CommercialRepository) CreateWithEvent(ctx context.Context, v commercial.Record, event commercial.Event) (commercial.Record, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return commercial.Record{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO campaign_commercial_approvals(id,campaign_id,organisation_id,quotation_reference,invoice_reference,currency,approved_recipients,unit_price_minor,management_fee_minor,total_amount_minor,payment_reference,payment_received_at,status,version,created_by,submitted_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),$12,$13,$14,NULLIF($15,'')::uuid,NULLIF($16,'')::uuid,NULLIF($17,'')::uuid,$18,$19,$20)`, v.ID, v.CampaignID, v.OrganisationID, v.QuotationReference, v.InvoiceReference, v.Currency, v.ApprovedRecipients, v.UnitPriceMinor, v.ManagementFeeMinor, v.TotalAmountMinor, v.PaymentReference, v.PaymentReceivedAt, v.Status, v.Version, v.CreatedBy, v.SubmittedBy, v.ApprovedBy, v.Reason, v.CreatedAt, v.UpdatedAt); err != nil {
		return commercial.Record{}, err
	}
	if err = insertCommercialEvent(ctx, tx, event); err != nil {
		return commercial.Record{}, err
	}
	if err = tx.Commit(); err != nil {
		return commercial.Record{}, err
	}
	return v, nil
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
func (r *CommercialRepository) ListPage(ctx context.Context, org string, limit int, before *time.Time, beforeID string) ([]commercial.Record, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, commercialSelect+` WHERE ($1::text='' OR organisation_id=NULLIF($1::text,'')::uuid) AND ($3::timestamptz IS NULL OR created_at<$3 OR (created_at=$3 AND id<NULLIF($4::text,'')::uuid)) ORDER BY created_at DESC,id DESC LIMIT $2`, org, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]commercial.Record, 0, limit)
	for rows.Next() {
		v, scanErr := scanCommercial(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *CommercialRepository) CompareAndSwap(ctx context.Context, v commercial.Record, expected int64) (commercial.Record, error) {
	res, err := r.DB.ExecContext(ctx, `UPDATE campaign_commercial_approvals SET quotation_reference=$2,invoice_reference=$3,currency=$4,approved_recipients=$5,unit_price_minor=$6,management_fee_minor=$7,total_amount_minor=$8,payment_reference=NULLIF($9,''),payment_received_at=$10,status=$11,version=$12,submitted_by=NULLIF($13,'')::uuid,approved_by=NULLIF($14,'')::uuid,reason=$15,updated_at=$16 WHERE id=$1::uuid AND version=$17`, v.ID, v.QuotationReference, v.InvoiceReference, v.Currency, v.ApprovedRecipients, v.UnitPriceMinor, v.ManagementFeeMinor, v.TotalAmountMinor, v.PaymentReference, v.PaymentReceivedAt, v.Status, v.Version, v.SubmittedBy, v.ApprovedBy, v.Reason, v.UpdatedAt, expected)
	if err != nil {
		return commercial.Record{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return commercial.Record{}, err
	}
	if n != 1 {
		return commercial.Record{}, commercial.ErrConflict
	}
	return v, nil
}

func (r *CommercialRepository) CompareAndSwapWithEvent(ctx context.Context, v commercial.Record, expected int64, event commercial.Event) (commercial.Record, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return commercial.Record{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE campaign_commercial_approvals SET quotation_reference=$2,invoice_reference=$3,currency=$4,approved_recipients=$5,unit_price_minor=$6,management_fee_minor=$7,total_amount_minor=$8,payment_reference=NULLIF($9,''),payment_received_at=$10,status=$11,version=$12,submitted_by=NULLIF($13,'')::uuid,approved_by=NULLIF($14,'')::uuid,reason=$15,updated_at=$16 WHERE id=$1::uuid AND version=$17`, v.ID, v.QuotationReference, v.InvoiceReference, v.Currency, v.ApprovedRecipients, v.UnitPriceMinor, v.ManagementFeeMinor, v.TotalAmountMinor, v.PaymentReference, v.PaymentReceivedAt, v.Status, v.Version, v.SubmittedBy, v.ApprovedBy, v.Reason, v.UpdatedAt, expected)
	if err != nil {
		return commercial.Record{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return commercial.Record{}, err
	}
	if n != 1 {
		return commercial.Record{}, commercial.ErrConflict
	}
	if err = insertCommercialEvent(ctx, tx, event); err != nil {
		return commercial.Record{}, err
	}
	if err = tx.Commit(); err != nil {
		return commercial.Record{}, err
	}
	return v, nil
}

func (r *CommercialRepository) ListEvents(ctx context.Context, identifier string) ([]commercial.Event, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,commercial_record_id::text,event_type,actor_id::text,reason,record_version,evidence,occurred_at FROM campaign_commercial_events WHERE commercial_record_id=$1::uuid ORDER BY record_version,id`, identifier)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []commercial.Event{}
	for rows.Next() {
		var event commercial.Event
		var raw []byte
		if err := rows.Scan(&event.ID, &event.CommercialRecordID, &event.EventType, &event.ActorID, &event.Reason, &event.RecordVersion, &raw, &event.OccurredAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			if err := decoder.Decode(&event.Evidence); err != nil {
				return nil, err
			}
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		if _, err := r.Get(ctx, identifier); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *CommercialRepository) EventPage(ctx context.Context, identifier string, limit int, afterVersion int64, afterID string) ([]commercial.Event, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,commercial_record_id::text,event_type,actor_id::text,reason,record_version,evidence,occurred_at
FROM campaign_commercial_events
WHERE commercial_record_id=$1::uuid
  AND ($3::bigint=0 OR record_version>$3 OR (record_version=$3 AND id>NULLIF($4,'')::uuid))
ORDER BY record_version,id
LIMIT $2`, identifier, limit, afterVersion, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]commercial.Event, 0, limit)
	for rows.Next() {
		var event commercial.Event
		var raw []byte
		if err := rows.Scan(&event.ID, &event.CommercialRecordID, &event.EventType, &event.ActorID, &event.Reason, &event.RecordVersion, &raw, &event.OccurredAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			if err := decoder.Decode(&event.Evidence); err != nil {
				return nil, err
			}
		}
		out = append(out, event)
	}
	return out, rows.Err()
}
