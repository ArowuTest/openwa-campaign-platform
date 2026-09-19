package reconciliation

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLOutcomeRepositoryFinalUnknownWindowIsSticky(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DELIVERY_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_DELIVERY_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ids := make([]string, 7)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	orgID, purposeID, campaignID, contactID, snapshotID, messageID, recipientID := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6]
	now := time.Now().UTC()
	if _, err = db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "reconcile-"+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO consent_purposes(id,code,name,channel,wording_version,active) VALUES($1::uuid,$2,'Reconcile','WHATSAPP','v1',true)`, purposeID, "reconcile-"+purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status) VALUES($1::uuid,$2::uuid,'outcome-test',$3::uuid,'DISPATCHING')`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***9090','ACTIVE',$3)`, contactID, "outcome-"+contactID, now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,1)`, snapshotID, campaignID, "outcome-"+snapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','hello',$3,'APPROVED',$4)`, messageID, campaignID, "outcome-"+messageID, "outcome-"+messageID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,attempt_count,authorised_at,updated_at,reconciliation_required,last_error_code) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'SUBMITTING',1,$7,$8,true,NULL)`, recipientID, campaignID, snapshotID, contactID, messageID, "outcome-"+recipientID, now.Add(-26*time.Hour), now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = db.ExecContext(c, `DELETE FROM campaign_recipients WHERE id=$1::uuid`, recipientID)
		_, _ = db.ExecContext(c, `DELETE FROM message_versions WHERE id=$1::uuid`, messageID)
		_, _ = db.ExecContext(c, `DELETE FROM audience_snapshots WHERE id=$1::uuid`, snapshotID)
		_, _ = db.ExecContext(c, `DELETE FROM contacts WHERE id=$1::uuid`, contactID)
		_, _ = db.ExecContext(c, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(c, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(c, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
	})
	repo := &PostgreSQLOutcomeRepository{DB: db}
	worker := &OutcomeWorker{Repository: repo, ReconciliationWindow: time.Hour, FinalUnknownWindow: 24 * time.Hour, BatchSize: 100, Clock: func() time.Time { return now }}
	n, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("stale submitting processed=%d want 1", n)
	}
	var status, code string
	var required bool
	if err := db.QueryRowContext(ctx, `SELECT status,reconciliation_required,coalesce(last_error_code,'') FROM campaign_recipients WHERE id=$1::uuid`, recipientID).Scan(&status, &required, &code); err != nil {
		t.Fatal(err)
	}
	if status != "UNKNOWN" || !required || code != "OUTCOME_UNKNOWN" {
		t.Fatalf("stale submitting status=%s reconciliation=%v code=%s", status, required, code)
	}

	finalNow := now.Add(25 * time.Hour)
	worker.Clock = func() time.Time { return finalNow }
	n, err = worker.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("final UNKNOWN processed=%d want at least 1", n)
	}
	if err := db.QueryRowContext(ctx, `SELECT status,reconciliation_required,coalesce(last_error_code,'') FROM campaign_recipients WHERE id=$1::uuid`, recipientID).Scan(&status, &required, &code); err != nil {
		t.Fatal(err)
	}
	if status != "UNKNOWN" || !required || code != "FINAL_UNKNOWN" {
		t.Fatalf("final status=%s reconciliation=%v code=%s", status, required, code)
	}

	var blockerID, actionableID, blockerContactID, actionableContactID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&blockerID, &actionableID, &blockerContactID, &actionableContactID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{blockerContactID, actionableContactID} {
		if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***8080','ACTIVE',$3)`, id, "outcome-starve-"+id, now); err != nil {
			t.Fatal(err)
		}
	}
	starveNow := finalNow.Add(30 * time.Minute)
	for _, fixture := range []struct {
		id, contact, status string
		updated             time.Time
	}{
		{blockerID, blockerContactID, "GATEWAY_ACCEPTED", starveNow.Add(-4 * time.Hour)},
		{actionableID, actionableContactID, "SUBMITTING", starveNow.Add(-3 * time.Hour)},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,attempt_count,authorised_at,updated_at,reconciliation_required) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,$7,1,$8,$9,true)`, fixture.id, campaignID, snapshotID, fixture.contact, messageID, "outcome-"+fixture.id, fixture.status, starveNow.Add(-5*time.Hour), fixture.updated); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = db.ExecContext(c, `DELETE FROM campaign_recipients WHERE id IN ($1::uuid,$2::uuid)`, blockerID, actionableID)
		_, _ = db.ExecContext(c, `DELETE FROM contacts WHERE id IN ($1::uuid,$2::uuid)`, blockerContactID, actionableContactID)
	})
	worker.BatchSize = 1
	worker.Clock = func() time.Time { return starveNow }
	n, err = worker.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("actionable stale outcome was starved by no-op reconciliation row: processed=%d want 1", n)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM campaign_recipients WHERE id=$1::uuid`, actionableID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "UNKNOWN" {
		t.Fatalf("actionable stale submitting status=%s want UNKNOWN", status)
	}

	items, err := repo.ListStaleOutcomes(ctx, starveNow.Add(72*time.Hour), starveNow.Add(48*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.RecipientID == recipientID {
			t.Fatalf("FINAL_UNKNOWN recipient remained in automatic outcome scan: %+v", item)
		}
	}

	var fencedID, fencedContactID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&fencedID, &fencedContactID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***7070','ACTIVE',$3)`, fencedContactID, "outcome-fence-"+fencedContactID, now); err != nil {
		t.Fatal(err)
	}
	fencedUpdatedAt := starveNow.Add(-3 * time.Hour)
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,attempt_count,authorised_at,updated_at,reconciliation_required) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'SUBMITTING',1,$7,$8,true)`, fencedID, campaignID, snapshotID, fencedContactID, messageID, "outcome-"+fencedID, starveNow.Add(-4*time.Hour), fencedUpdatedAt); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = db.ExecContext(c, `DELETE FROM campaign_recipients WHERE id=$1::uuid`, fencedID)
		_, _ = db.ExecContext(c, `DELETE FROM contacts WHERE id=$1::uuid`, fencedContactID)
	})
	listed, err := repo.ListStaleOutcomes(ctx, starveNow.Add(-time.Hour), starveNow.Add(-24*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range listed {
		if item.RecipientID == fencedID {
			fencedUpdatedAt = item.UpdatedAt
			found = true
			break
		}
	}
	if !found {
		t.Fatal("fencing fixture was not selected")
	}
	if _, err := db.ExecContext(ctx, `UPDATE campaign_recipients SET updated_at=$2 WHERE id=$1::uuid`, fencedID, starveNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.QuarantineStaleSubmitting(ctx, fencedID, fencedUpdatedAt, starveNow); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale reconciliation snapshot overwrote a freshened row: err=%v listedUpdatedAt=%v", err, fencedUpdatedAt)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM campaign_recipients WHERE id=$1::uuid`, fencedID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "SUBMITTING" {
		t.Fatalf("freshened reconciliation candidate status=%s want SUBMITTING", status)
	}

	var ineligibleID, ineligibleContactID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&ineligibleID, &ineligibleContactID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***5050','ACTIVE',$3)`, ineligibleContactID, "outcome-ineligible-"+ineligibleContactID, now); err != nil {
		t.Fatal(err)
	}
	ineligibleUpdatedAt := starveNow.Add(-4 * time.Hour)
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,attempt_count,authorised_at,updated_at,reconciliation_required) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'SUBMITTING',1,$7,$8,false)`, ineligibleID, campaignID, snapshotID, ineligibleContactID, messageID, "outcome-"+ineligibleID, starveNow.Add(-5*time.Hour), ineligibleUpdatedAt); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = db.ExecContext(c, `DELETE FROM campaign_recipients WHERE id=$1::uuid`, ineligibleID)
		_, _ = db.ExecContext(c, `DELETE FROM contacts WHERE id=$1::uuid`, ineligibleContactID)
	})
	if err := repo.QuarantineStaleSubmitting(ctx, ineligibleID, ineligibleUpdatedAt, starveNow); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("non-reconciliation SUBMITTING row was quarantined: err=%v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status,reconciliation_required FROM campaign_recipients WHERE id=$1::uuid`, ineligibleID).Scan(&status, &required); err != nil {
		t.Fatal(err)
	}
	if status != "SUBMITTING" || required {
		t.Fatalf("ineligible reconciliation row mutated status=%s reconciliation=%v", status, required)
	}
}
