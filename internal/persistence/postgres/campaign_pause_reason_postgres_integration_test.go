package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLCampaignRepositoryProjectsPauseReason(t *testing.T) {
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
	var orgID, purposeID, campaignID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&orgID, &purposeID, &campaignID); err != nil {
		t.Fatal(err)
	}
	must := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Pause projection "+orgID[:8])
	must(`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Pause projection','WHATSAPP','v1')`, purposeID, orgID, "PAUSE_"+purposeID[:8])
	must(`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,maximum_messages_per_recipient,pause_reason) VALUES($1::uuid,$2::uuid,'Pause projection',$3::uuid,'PAUSED',1,1,'operator capacity hold')`, campaignID, orgID, purposeID)
	defer func() {
		cleanup, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
	}()
	loaded, err := (&CampaignRepository{DB: db}).Get(ctx, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MaximumMessagesPerRecipient != 1 {
		t.Fatalf("maximum_messages_per_recipient projection=%d", loaded.MaximumMessagesPerRecipient)
	}
	if loaded.PauseReason != "operator capacity hold" {
		t.Fatalf("pause_reason projection=%q", loaded.PauseReason)
	}
	previousVersion := loaded.Version
	loaded.MaximumMessagesPerRecipient = 2
	loaded.Version = previousVersion + 1
	loaded.UpdatedAt = time.Now().UTC()
	if err := (&CampaignRepository{DB: db}).CompareAndSwap(ctx, loaded, previousVersion); err != nil {
		t.Fatal(err)
	}
	reloaded, err := (&CampaignRepository{DB: db}).Get(ctx, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.MaximumMessagesPerRecipient != 2 {
		t.Fatalf("maximum_messages_per_recipient CAS update was not persisted: %d", reloaded.MaximumMessagesPerRecipient)
	}
}
