package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"campaign-platform/internal/organisation"
)

type OrganisationRepository struct{ DB *sql.DB }

func (r *OrganisationRepository) Create(ctx context.Context, v organisation.Organisation) error {
	if r.DB == nil {
		return errors.New("database is required")
	}
	const q = `INSERT INTO organisations(id,legal_name,trading_name,country_id,status,primary_contact_name,primary_contact_email,internal_notes,created_at,updated_at,version) VALUES($1,$2,NULLIF($3,''),(SELECT id FROM countries WHERE iso2=NULLIF($4,'')),$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),$9,$9,$10)`
	_, err := r.DB.ExecContext(ctx, q, v.ID, v.LegalName, v.TradingName, v.CountryISO2, v.Status, v.PrimaryContactName, v.PrimaryContactEmail, v.InternalNotes, v.CreatedAt, v.Version)
	if isUniqueViolation(err) {
		return organisation.ErrDuplicate
	}
	return err
}
func (r *OrganisationRepository) List(ctx context.Context) ([]organisation.Organisation, error) {
	if r.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := r.DB.QueryContext(ctx, organisationSelect+` ORDER BY o.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []organisation.Organisation{}
	for rows.Next() {
		v, err := scanOrganisation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *OrganisationRepository) ListPage(ctx context.Context, limit int, before *time.Time, beforeID string) ([]organisation.Organisation, error) {
	if r.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := r.DB.QueryContext(ctx, organisationSelect+` WHERE ($2::timestamptz IS NULL OR o.created_at<$2 OR (o.created_at=$2 AND o.id<NULLIF($3,'')::uuid)) ORDER BY o.created_at DESC,o.id DESC LIMIT $1`, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []organisation.Organisation{}
	for rows.Next() {
		v, err := scanOrganisation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *OrganisationRepository) Get(ctx context.Context, id string) (organisation.Organisation, error) {
	if r.DB == nil {
		return organisation.Organisation{}, errors.New("database is required")
	}
	v, err := scanOrganisation(r.DB.QueryRowContext(ctx, organisationSelect+` WHERE o.id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return organisation.Organisation{}, organisation.ErrNotFound
	}
	return v, err
}

const organisationSelect = `SELECT o.id,o.legal_name,coalesce(o.trading_name,''),coalesce(c.iso2,''),o.status,coalesce(o.primary_contact_name,''),coalesce(o.primary_contact_email::text,''),coalesce(o.internal_notes,''),o.created_at,o.updated_at,o.version FROM organisations o LEFT JOIN countries c ON c.id=o.country_id`

type scanner interface{ Scan(...any) error }

func scanOrganisation(row scanner) (organisation.Organisation, error) {
	var v organisation.Organisation
	var status string
	err := row.Scan(&v.ID, &v.LegalName, &v.TradingName, &v.CountryISO2, &status, &v.PrimaryContactName, &v.PrimaryContactEmail, &v.InternalNotes, &v.CreatedAt, &v.UpdatedAt, &v.Version)
	v.Status = organisation.Status(status)
	return v, err
}
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	value := strings.ToLower(err.Error())
	return strings.Contains(value, "unique") || strings.Contains(value, "duplicate key")
}

func (r *OrganisationRepository) Update(ctx context.Context, v organisation.Organisation, expectedVersion int64, event organisation.Event) (organisation.Organisation, error) {
	if r.DB == nil {
		return organisation.Organisation{}, errors.New("database is required")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return organisation.Organisation{}, err
	}
	defer tx.Rollback()
	const q = `UPDATE organisations SET legal_name=$2,trading_name=NULLIF($3,''),country_id=(SELECT id FROM countries WHERE iso2=NULLIF($4,'')),status=$5,primary_contact_name=NULLIF($6,''),primary_contact_email=NULLIF($7,''),internal_notes=NULLIF($8,''),updated_at=$9,version=$10 WHERE id=$1::uuid AND version=$11`
	res, err := tx.ExecContext(ctx, q, v.ID, v.LegalName, v.TradingName, v.CountryISO2, v.Status, v.PrimaryContactName, v.PrimaryContactEmail, v.InternalNotes, v.UpdatedAt, v.Version, expectedVersion)
	if isUniqueViolation(err) {
		return organisation.Organisation{}, organisation.ErrDuplicate
	}
	if err != nil {
		return organisation.Organisation{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return organisation.Organisation{}, err
	}
	if n != 1 {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM organisations WHERE id=$1::uuid)`, v.ID).Scan(&exists); err != nil {
			return organisation.Organisation{}, err
		}
		if !exists {
			return organisation.Organisation{}, organisation.ErrNotFound
		}
		return organisation.Organisation{}, organisation.ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO organisation_events(id,organisation_id,version,event_type,actor_id,reason,before_status,after_status,occurred_at) VALUES($1::uuid,$2::uuid,$3,$4,NULLIF($5,'')::uuid,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),$9)`, event.ID, event.OrganisationID, event.Version, event.EventType, event.ActorID, event.Reason, event.BeforeStatus, event.AfterStatus, event.OccurredAt)
	if err != nil {
		return organisation.Organisation{}, err
	}
	if err = tx.Commit(); err != nil {
		return organisation.Organisation{}, err
	}
	return v, nil
}

func (r *OrganisationRepository) ListEvents(ctx context.Context, organisationID string) ([]organisation.Event, error) {
	if r.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,organisation_id::text,version,event_type,coalesce(actor_id::text,''),coalesce(reason,''),coalesce(before_status,''),coalesce(after_status,''),occurred_at FROM organisation_events WHERE organisation_id=$1::uuid ORDER BY version,occurred_at`, organisationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []organisation.Event{}
	for rows.Next() {
		var e organisation.Event
		var before, after string
		if err := rows.Scan(&e.ID, &e.OrganisationID, &e.Version, &e.EventType, &e.ActorID, &e.Reason, &before, &after, &e.OccurredAt); err != nil {
			return nil, err
		}
		e.BeforeStatus = organisation.Status(before)
		e.AfterStatus = organisation.Status(after)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		var exists bool
		if err := r.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM organisations WHERE id=$1::uuid)`, organisationID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, organisation.ErrNotFound
		}
	}
	return out, nil
}
func (r *OrganisationRepository) ListEventPage(ctx context.Context, organisationID string, limit int, afterVersion int64) ([]organisation.Event, error) {
	if r.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,organisation_id::text,version,event_type,coalesce(actor_id::text,''),coalesce(reason,''),coalesce(before_status,''),coalesce(after_status,''),occurred_at FROM organisation_events WHERE organisation_id=$1::uuid AND version>$2 ORDER BY version ASC LIMIT $3`, organisationID, afterVersion, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []organisation.Event{}
	for rows.Next() {
		var event organisation.Event
		var before, after string
		if err := rows.Scan(&event.ID, &event.OrganisationID, &event.Version, &event.EventType, &event.ActorID, &event.Reason, &before, &after, &event.OccurredAt); err != nil {
			return nil, err
		}
		event.BeforeStatus = organisation.Status(before)
		event.AfterStatus = organisation.Status(after)
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		var exists bool
		if err := r.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM organisations WHERE id=$1::uuid)`, organisationID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, organisation.ErrNotFound
		}
	}
	return out, nil
}
