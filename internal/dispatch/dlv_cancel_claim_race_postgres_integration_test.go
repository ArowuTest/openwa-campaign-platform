package dispatch

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
	_ "campaign-platform/internal/persistence/database"
)

func TestDLV018LiveClaimCannotCrossSubmissionBoundaryAfterCancel(t *testing.T) {
	dsn := os.Getenv("POSTGRES_FINAL_ELIGIBILITY_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_FINAL_ELIGIBILITY_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := seedFinalEligibilityFixture(t, ctx, db)
	now := f.AsOf.Add(time.Minute)
	if _, err := db.ExecContext(ctx, `UPDATE campaign_recipients SET status='CLAIMED' WHERE id=$1::uuid`, f.Recipient.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET status='CANCELLED' WHERE id=$1::uuid`, f.CampaignID); err != nil {
		t.Fatal(err)
	}
	repo := &delivery.PostgreSQLRepository{DB: db}
	_, _, err = repo.ApplyEvent(ctx, f.Recipient.ID, delivery.Event{DeduplicationKey: "dlv018-live-claim-submit:" + f.Recipient.ID, Type: delivery.EventSubmitting, OccurredAt: now})
	if !errors.Is(err, delivery.ErrCampaignNotDispatchable) {
		t.Fatalf("submit after cancel err=%v want ErrCampaignNotDispatchable", err)
	}
	var status string
	var attempts, events int
	if err := db.QueryRowContext(ctx, `SELECT status,attempt_count FROM campaign_recipients WHERE id=$1::uuid`, f.Recipient.ID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM delivery_events WHERE event_deduplication_key=$1`, "dlv018-live-claim-submit:"+f.Recipient.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if status != "CLAIMED" || attempts != 0 || events != 0 {
		t.Fatalf("cancel race mutated status=%s attempts=%d events=%d", status, attempts, events)
	}
}
