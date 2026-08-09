package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/campaignworkspace"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLCampaignWorkspacePersistsTagsNotesAndArchiveAudit(t *testing.T) {
	dsn := os.Getenv("POSTGRES_CAMPAIGN_WORKSPACE_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_CAMPAIGN_WORKSPACE_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var orgID, purposeID, campaignID, actorID, noteID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&orgID, &purposeID, &campaignID, &actorID, &noteID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_workspace_events WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_internal_notes WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_workspaces WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Workspace Evidence Actor','DISABLED',false)`, actorID, "workspace-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Workspace Evidence "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Workspace Evidence','WHATSAPP','v1')`, purposeID, orgID, "WS_"+purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Workspace Evidence',$3::uuid,'COMPLETED',1)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	repository := &CampaignWorkspaceRepository{DB: db}
	if err := repository.Ensure(ctx, campaignID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 13, 0, 0, 0, time.UTC)
	workspace, err := repository.SetTags(ctx, campaignID, []string{"events", "priority"}, actorID, "classify campaign", 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.Tags) != 2 || workspace.Archive.Version != 2 {
		t.Fatalf("tags not persisted: %+v", workspace)
	}
	note := campaignworkspace.Note{ID: noteID, CampaignID: campaignID, Category: campaignworkspace.NoteCompliance, Body: "Internal compliance evidence only.", CreatedBy: actorID, CreatedAt: now.Add(time.Minute)}
	if err := repository.AddNote(ctx, note); err != nil {
		t.Fatal(err)
	}
	archived, err := repository.Archive(ctx, campaignID, true, actorID, "retention lifecycle", 2, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !archived.Archive.Archived || archived.Archive.ArchivedBy != actorID || len(archived.Notes) != 1 {
		t.Fatalf("workspace archive/note persistence failed: %+v", archived)
	}
	rows, err := db.QueryContext(ctx, `SELECT event_type,actor_id::text,reason FROM campaign_workspace_events WHERE campaign_id=$1::uuid ORDER BY created_at`, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var events []string
	for rows.Next() {
		var eventType, eventActor, reason string
		if err := rows.Scan(&eventType, &eventActor, &reason); err != nil {
			t.Fatal(err)
		}
		if eventActor != actorID || reason == "" {
			t.Fatalf("workspace event lacks actor/reason: type=%s actor=%s reason=%s", eventType, eventActor, reason)
		}
		events = append(events, eventType)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0] != "TAGS_UPDATED" || events[1] != "ARCHIVED" {
		t.Fatalf("unexpected workspace audit events: %v", events)
	}
}
