package execution

import (
	"context"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	postgresrepo "campaign-platform/internal/persistence/postgres"
)

func TestDLV018CampaignCancellationMarksOnlyUndispatchedObligationsCancelled(t *testing.T) {
	db := openExecutionLifecycleDatabase(t)
	fixture := newExecutionLifecycleFixture(t, db, "PAUSED")
	t.Cleanup(func() { fixture.cleanup(t, db) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ids := make([]string, 8)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `
SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,
       gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,
       gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	contactQueued, contactUnknown, contactAccepted := ids[0], ids[1], ids[2]
	snapshotID, messageID := ids[3], ids[4]
	queuedID, unknownID, acceptedID := ids[5], ids[6], ids[7]
	now := time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)

	for i, contactID := range []string{contactQueued, contactUnknown, contactAccepted} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at)
VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),$3,'ACTIVE',$4)`,
			contactID, "dlv018-"+contactID, "***000"+string(rune('1'+i)), now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count,created_by)
VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,3,$4::uuid)`, snapshotID, fixture.campaignID, "dlv018-"+snapshotID, fixture.actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,created_by,client_request_id)
VALUES($1::uuid,$2::uuid,1,'TEXT','hello',$3,'APPROVED',$4::uuid,$5)`, messageID, fixture.campaignID, "dlv018-"+messageID, fixture.actorID, "dlv018-"+messageID); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		id        string
		contactID string
		status    string
		attempts  int
	}{
		{queuedID, contactQueued, "QUEUED", 3},
		{unknownID, contactUnknown, "UNKNOWN", 2},
		{acceptedID, contactAccepted, "GATEWAY_ACCEPTED", 1},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO campaign_recipients(
 id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,attempt_count,authorised_at,updated_at
) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,$7,$8,$9,$9)`,
			item.id, fixture.campaignID, snapshotID, item.contactID, messageID,
			"dlv018-"+item.id, item.status, item.attempts, now.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO campaign_metrics(campaign_id,queued_total,submitted_total,unknown_total,updated_at)
VALUES($1::uuid,1,1,1,$2)`, fixture.campaignID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO durable_jobs(
 job_type,deduplication_key,payload,status,priority,attempt_count,max_attempts,
 available_at,created_at,updated_at
) VALUES(
 'DISPATCH_CAMPAIGN_RECIPIENT',$1,jsonb_build_object('campaignRecipientId',$2::text),
 'PENDING',0,0,8,$3,$3,$3
)`, "dlv018-job-"+queuedID, queuedID, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = db.ExecContext(cleanup, `DELETE FROM durable_jobs WHERE deduplication_key=$1`, "dlv018-job-"+queuedID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM delivery_events WHERE campaign_recipient_id IN ($1::uuid,$2::uuid,$3::uuid)`, queuedID, unknownID, acceptedID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_metrics WHERE campaign_id=$1::uuid`, fixture.campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_recipients WHERE id IN ($1::uuid,$2::uuid,$3::uuid)`, queuedID, unknownID, acceptedID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM message_versions WHERE id=$1::uuid`, messageID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM audience_snapshots WHERE id=$1::uuid`, snapshotID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM contacts WHERE id IN ($1::uuid,$2::uuid,$3::uuid)`, contactQueued, contactUnknown, contactAccepted)
	})

	repository := &postgresrepo.CampaignRepository{DB: db}
	current, err := repository.Get(ctx, fixture.campaignID)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := current.Transition(campaign.TransitionInput{
		Action: campaign.ActionCancel, ActorID: fixture.actorID,
		Reason: "operator cancellation", ExpectedVersion: current.Version,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	committer := &PostgreSQLLifecycleCommitter{DB: db}
	if _, err := committer.Commit(ctx, LifecycleCommit{
		Campaign: candidate, ExpectedVersion: current.Version,
		Action: campaign.ActionCancel, EventType: "CAMPAIGN_CANCELLED",
		ActorID: fixture.actorID, Reason: "operator cancellation",
		RoutingPlanID: fixture.planID, ReservationOperation: ReservationRelease,
		OccurredAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	assertRecipient := func(id, wantStatus string, wantAttempts int) {
		t.Helper()
		var status string
		var attempts int
		if err := db.QueryRowContext(ctx, `SELECT status,attempt_count FROM campaign_recipients WHERE id=$1::uuid`, id).Scan(&status, &attempts); err != nil {
			t.Fatal(err)
		}
		if status != wantStatus || attempts != wantAttempts {
			t.Fatalf("recipient %s status=%s attempts=%d want status=%s attempts=%d", id, status, attempts, wantStatus, wantAttempts)
		}
	}
	assertRecipient(queuedID, "CANCELLED", 3)
	assertRecipient(unknownID, "UNKNOWN", 2)
	assertRecipient(acceptedID, "GATEWAY_ACCEPTED", 1)

	var jobStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM durable_jobs WHERE deduplication_key=$1`, "dlv018-job-"+queuedID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "CANCELLED" {
		t.Fatalf("pending dispatch job status=%s want CANCELLED", jobStatus)
	}
	var queuedTotal, submittedTotal, unknownTotal int64
	if err := db.QueryRowContext(ctx, `
SELECT queued_total,submitted_total,unknown_total
FROM campaign_metrics WHERE campaign_id=$1::uuid`, fixture.campaignID).Scan(&queuedTotal, &submittedTotal, &unknownTotal); err != nil {
		t.Fatal(err)
	}
	if queuedTotal != 0 || submittedTotal != 1 || unknownTotal != 1 {
		t.Fatalf("metrics queued=%d submitted=%d unknown=%d want 0/1/1", queuedTotal, submittedTotal, unknownTotal)
	}

	var cancellationEvents int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM delivery_events
WHERE campaign_recipient_id=$1::uuid AND event_type='message.cancelled'`, queuedID).Scan(&cancellationEvents); err != nil {
		t.Fatal(err)
	}
	if cancellationEvents != 1 {
		t.Fatalf("queued recipient cancellation events=%d want 1", cancellationEvents)
	}
}
