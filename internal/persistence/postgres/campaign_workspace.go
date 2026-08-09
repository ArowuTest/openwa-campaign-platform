package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"campaign-platform/internal/campaignworkspace"
)

type CampaignWorkspaceRepository struct{ DB *sql.DB }

func (r *CampaignWorkspaceRepository) Ensure(ctx context.Context, id string) error {
	if r.DB == nil {
		return errors.New("database is required")
	}
	_, err := r.DB.ExecContext(ctx, `INSERT INTO campaign_workspaces(campaign_id,tags,archived,version) VALUES($1,'[]'::jsonb,false,1) ON CONFLICT(campaign_id) DO NOTHING`, id)
	return err
}
func (r *CampaignWorkspaceRepository) Get(ctx context.Context, id string) (campaignworkspace.Workspace, error) {
	if r.DB == nil {
		return campaignworkspace.Workspace{}, errors.New("database is required")
	}
	var w campaignworkspace.Workspace
	var tags []byte
	var archivedAt sql.NullTime
	err := r.DB.QueryRowContext(ctx, `SELECT campaign_id,tags,archived,coalesce(archived_by::text,''),archived_at,coalesce(archive_reason,''),version FROM campaign_workspaces WHERE campaign_id=$1`, id).Scan(&w.CampaignID, &tags, &w.Archive.Archived, &w.Archive.ArchivedBy, &archivedAt, &w.Archive.Reason, &w.Archive.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return campaignworkspace.Workspace{}, campaignworkspace.ErrNotFound
	}
	if err != nil {
		return campaignworkspace.Workspace{}, err
	}
	w.Archive.CampaignID = id
	if err := json.Unmarshal(tags, &w.Tags); err != nil {
		return campaignworkspace.Workspace{}, err
	}
	if archivedAt.Valid {
		t := archivedAt.Time
		w.Archive.ArchivedAt = &t
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id,campaign_id,category,body,created_by,created_at FROM campaign_internal_notes WHERE campaign_id=$1 ORDER BY created_at DESC`, id)
	if err != nil {
		return campaignworkspace.Workspace{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var n campaignworkspace.Note
		if err := rows.Scan(&n.ID, &n.CampaignID, &n.Category, &n.Body, &n.CreatedBy, &n.CreatedAt); err != nil {
			return campaignworkspace.Workspace{}, err
		}
		w.Notes = append(w.Notes, n)
	}
	return w, rows.Err()
}
func (r *CampaignWorkspaceRepository) SetTags(ctx context.Context, id string, tags []string, actor, reason string, expected int64, now time.Time) (campaignworkspace.Workspace, error) {
	if r.DB == nil {
		return campaignworkspace.Workspace{}, errors.New("database is required")
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return campaignworkspace.Workspace{}, err
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return campaignworkspace.Workspace{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE campaign_workspaces SET tags=$2,version=version+1,updated_at=$3 WHERE campaign_id=$1 AND version=$4`, id, string(b), now, expected)
	if err != nil {
		return campaignworkspace.Workspace{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return campaignworkspace.Workspace{}, err
	}
	if n == 0 {
		return campaignworkspace.Workspace{}, campaignworkspace.ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO campaign_workspace_events(campaign_id,event_type,actor_id,reason,created_at) VALUES($1,'TAGS_UPDATED',$2,$3,$4)`, id, actor, reason, now); err != nil {
		return campaignworkspace.Workspace{}, err
	}
	if err = tx.Commit(); err != nil {
		return campaignworkspace.Workspace{}, err
	}
	return r.Get(ctx, id)
}
func (r *CampaignWorkspaceRepository) AddNote(ctx context.Context, n campaignworkspace.Note) error {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO campaign_internal_notes(id,campaign_id,category,body,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6)`, n.ID, n.CampaignID, n.Category, n.Body, n.CreatedBy, n.CreatedAt)
	return err
}
func (r *CampaignWorkspaceRepository) Archive(ctx context.Context, id string, archived bool, actor, reason string, expected int64, now time.Time) (campaignworkspace.Workspace, error) {
	if r.DB == nil {
		return campaignworkspace.Workspace{}, errors.New("database is required")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return campaignworkspace.Workspace{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE campaign_workspaces SET archived=$2,archived_by=CASE WHEN $2 THEN $3::uuid ELSE NULL END,archived_at=CASE WHEN $2 THEN $4::timestamptz ELSE NULL END,archive_reason=$5,version=version+1,updated_at=$4 WHERE campaign_id=$1 AND version=$6`, id, archived, actor, now, reason, expected)
	if err != nil {
		return campaignworkspace.Workspace{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return campaignworkspace.Workspace{}, err
	}
	if n == 0 {
		return campaignworkspace.Workspace{}, campaignworkspace.ErrConflict
	}
	event := "RESTORED"
	if archived {
		event = "ARCHIVED"
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO campaign_workspace_events(campaign_id,event_type,actor_id,reason,created_at) VALUES($1,$2,$3,$4,$5)`, id, event, actor, reason, now); err != nil {
		return campaignworkspace.Workspace{}, err
	}
	if err = tx.Commit(); err != nil {
		return campaignworkspace.Workspace{}, err
	}
	return r.Get(ctx, id)
}
