package message

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLMessageDraftPersistsJSONMetadataIdempotently(t *testing.T) {
	dsn := os.Getenv("POSTGRES_PAGINATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PAGINATION_DATABASE_URL is not set")
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
	var actorID, orgID, purposeID, campaignID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&actorID, &orgID, &purposeID, &campaignID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 message','DISABLED',false)`, actorID, "message-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Task 6 message "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Task 6 message','WHATSAPP','v1')`, purposeID, orgID, "MESSAGE_"+purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Task 6 message',$3::uuid,'DRAFT',1)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2099, 3, 2, 18, 0, 0, 0, time.UTC)
	repo := &PostgreSQLRepository{DB: db}
	input := Input{
		CampaignID: campaignID, Type: TypeText, Body: "Hello {{name}}",
		Variables: []Variable{{Name: "name", DataType: "TEXT", Fallback: "friend"}},
		CreatedBy: actorID, IdempotencyKey: "task6-message-create-000001",
	}
	created, err := repo.CreateDraft(ctx, input, now)
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || len(created.Variables) != 1 || created.Variables[0].Name != "name" {
		t.Fatalf("unexpected created draft: %+v", created)
	}
	loaded, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ContentHash != created.ContentHash || len(loaded.Variables) != 1 || loaded.Variables[0].Fallback != "friend" {
		t.Fatalf("unexpected loaded draft: %+v", loaded)
	}
	replayed, err := repo.CreateDraft(ctx, input, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != created.ID || replayed.Version != created.Version {
		t.Fatalf("unexpected idempotent replay: %+v", replayed)
	}
}
