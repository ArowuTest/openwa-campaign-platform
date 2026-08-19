package execution

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	_ "campaign-platform/internal/persistence/database"
	postgresrepo "campaign-platform/internal/persistence/postgres"
)

func TestPostgreSQLExecutionLeaseHasOneWinnerAndCannotBeReclaimedBeforeExpiry(t *testing.T) {
	dsn := os.Getenv("POSTGRES_EXECUTION_LEASE_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_EXECUTION_LEASE_DATABASE_URL is not set")
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

	var organisationID, purposeID, campaignID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(
		&organisationID, &purposeID, &campaignID,
	); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, organisationID)
	}()

	now := time.Now().UTC().Truncate(time.Second)
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`,
		organisationID, "Execution Lease "+organisationID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Lease','WHATSAPP','v1')`,
		purposeID, organisationID, "LEASE_"+purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,requested_start_at,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Lease',$3::uuid,'SCHEDULED',$4,1)`,
		campaignID, organisationID, purposeID, now); err != nil {
		t.Fatal(err)
	}

	repository := &PostgreSQLStore{DB: db}
	type outcome struct {
		owner     string
		campaigns []CampaignLease
		err       error
	}
	results := make(chan outcome, 2)
	var wait sync.WaitGroup
	for _, owner := range []string{"execution-worker-a", "execution-worker-b"} {
		owner := owner
		wait.Add(1)
		go func() {
			defer wait.Done()
			claimed, claimErr := repository.ClaimDue(ctx, owner, time.Minute, now, 1)
			results <- outcome{owner: owner, campaigns: claimed, err: claimErr}
		}()
	}
	wait.Wait()
	close(results)

	total := 0
	winnerOwner := ""
	var winner CampaignLease
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		total += len(result.campaigns)
		if len(result.campaigns) == 1 {
			winnerOwner = result.owner
			winner = result.campaigns[0]
		}
	}
	if total != 1 || winnerOwner == "" {
		t.Fatalf("concurrent claims=%d winner=%q", total, winnerOwner)
	}

	duplicate, err := repository.ClaimDue(ctx, winnerOwner, time.Minute, now.Add(time.Second), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(duplicate) != 0 {
		t.Fatalf("same owner reclaimed unexpired execution lease: %+v", duplicate)
	}

	expiredNow := now.Add(2 * time.Minute)
	reclaimed, err := repository.ClaimDue(ctx, winnerOwner, time.Minute, expiredNow, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(reclaimed) != 1 {
		t.Fatalf("expired lease claims=%d", len(reclaimed))
	}
	if winner.Owner != winnerOwner || winner.FenceToken <= 0 {
		t.Fatalf("winner lease is incomplete: %+v", winner)
	}
	if reclaimed[0].FenceToken <= winner.FenceToken {
		t.Fatalf("fence did not advance old=%d new=%d",
			winner.FenceToken, reclaimed[0].FenceToken)
	}
	if err = repository.Release(
		ctx, campaignID, winnerOwner, winner.FenceToken, expiredNow,
	); !errors.Is(err, ErrExecutionLeaseConflict) {
		t.Fatalf("stale release error=%v", err)
	}
	var currentExpiry time.Time
	if err = db.QueryRowContext(ctx, `
SELECT expires_at FROM campaign_execution_leases WHERE campaign_id=$1::uuid
`, campaignID).Scan(&currentExpiry); err != nil {
		t.Fatal(err)
	}
	if !currentExpiry.Equal(reclaimed[0].ExpiresAt) {
		t.Fatalf("stale release changed expiry got=%s want=%s",
			currentExpiry, reclaimed[0].ExpiresAt)
	}
	campaigns := &postgresrepo.CampaignRepository{DB: db}
	current, err := campaigns.Get(ctx, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := current.Transition(campaign.TransitionInput{
		Action: campaign.ActionCancel, ActorID: winnerOwner,
		Reason: "stale execution worker", ExpectedVersion: current.Version,
	}, expiredNow)
	if err != nil {
		t.Fatal(err)
	}
	committer := &PostgreSQLLifecycleCommitter{DB: db}
	_, err = committer.Commit(ctx, LifecycleCommit{
		Campaign: candidate, ExpectedVersion: current.Version,
		Action: campaign.ActionCancel, EventType: "CAMPAIGN_CANCELLED",
		ActorID: winnerOwner, Reason: "stale execution worker",
		ReservationOperation: ReservationNone, OccurredAt: candidate.UpdatedAt,
		ExecutionLease: &ExecutionLeaseFence{
			Owner: winnerOwner, FenceToken: winner.FenceToken,
		},
	})
	if !errors.Is(err, ErrExecutionLeaseConflict) {
		t.Fatalf("stale lifecycle commit error=%v", err)
	}
	var status string
	var version, lifecycleEvents int64
	if err = db.QueryRowContext(ctx, `
SELECT c.status,c.version,
       (SELECT count(*) FROM campaign_execution_events e
        WHERE e.campaign_id=c.id AND e.campaign_version IS NOT NULL)
FROM campaigns c WHERE c.id=$1::uuid
`, campaignID).Scan(&status, &version, &lifecycleEvents); err != nil {
		t.Fatal(err)
	}
	if status != "SCHEDULED" || version != current.Version || lifecycleEvents != 0 {
		t.Fatalf("stale worker committed status=%s version=%d events=%d",
			status, version, lifecycleEvents)
	}
}
