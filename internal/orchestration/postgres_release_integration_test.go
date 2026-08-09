package orchestration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

type postgresReleaseFixture struct {
	CampaignID, SnapshotID, MessageID, OrganisationID, PurposeID, ReviewID string
	Contacts                                                               []string
	AsOf                                                                   time.Time
}

func openPostgresReleaseDB(t *testing.T) (*sql.DB, context.Context, context.CancelFunc) {
	t.Helper()
	dsn := os.Getenv("POSTGRES_RELEASE_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_RELEASE_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := db.PingContext(ctx); err != nil {
		cancel()
		db.Close()
		t.Fatal(err)
	}
	return db, ctx, cancel
}
func seedPostgresReleaseFixture(t *testing.T, ctx context.Context, db *sql.DB, maximum int64, contactCount int) postgresReleaseFixture {
	t.Helper()
	ids := make([]string, 7+contactCount)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	query := "SELECT "
	for i := range ids {
		if i > 0 {
			query += ","
		}
		query += "gen_random_uuid()::text"
	}
	if err := db.QueryRowContext(ctx, query).Scan(args...); err != nil {
		t.Fatal(err)
	}
	orgID, purposeID, campaignID := ids[0], ids[1], ids[2]
	reviewID, messageID, snapshotID, actorID := ids[3], ids[4], ids[5], ids[6]
	contacts := ids[7:]
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Release Evidence Actor','DISABLED',false)`, actorID, "release-evidence-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Release Evidence "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,reviewed_by,reviewed_at,expires_at,outcome) VALUES($1::uuid,$2::uuid,'Release review','WHATSAPP','DIRECT','v1',true,true,true,'APPROVED',$3::uuid,$4,$5,'APPROVED')`, reviewID, orgID, actorID, now.Add(-time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Release purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "REL_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,maximum_messages_per_recipient,consent_review_id) VALUES($1::uuid,$2::uuid,'Release integration',$3::uuid,'SCHEDULED',$4,1,$5::uuid)`, campaignID, orgID, purposeID, maximum, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','release integration',repeat('c',64),'APPROVED',$3)`, messageID, campaignID, "release-"+messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count,configuration_version) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,$4,'release-test')`, snapshotID, campaignID, "snapshot-"+snapshotID, contactCount); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET audience_snapshot_id=$2::uuid,approved_message_version_id=$3::uuid WHERE id=$1::uuid`, campaignID, snapshotID, messageID); err != nil {
		t.Fatal(err)
	}
	for i, contactID := range contacts {
		lookup := fmt.Sprintf("%064x", i+1) + contactID[:8]
		if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),$3,'ACTIVE',$4)`, contactID, lookup, fmt.Sprintf("***%04d", i), now); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO consent_grants(contact_id,organisation_id,purpose_id,channel,wording_version,source_type,evidence_checksum,granted_at,status,effective_from,created_by,client_request_id,request_fingerprint,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'WHATSAPP','v1','DIRECT',$4,$5,'ACTIVE',$5,$6::uuid,$7,$8,$9::uuid)`, contactID, orgID, purposeID, "evidence-"+contactID, now.Add(-time.Hour), actorID, "grant-"+contactID, "fingerprint-"+contactID, reviewID); err != nil {
			t.Fatal(err)
		}
	}
	return postgresReleaseFixture{CampaignID: campaignID, SnapshotID: snapshotID, MessageID: messageID, OrganisationID: orgID, PurposeID: purposeID, ReviewID: reviewID, Contacts: contacts, AsOf: now}
}

func releaseCommand(f postgresReleaseFixture, maximum int64) Command {
	members := make([]Member, 0, len(f.Contacts))
	for _, contactID := range f.Contacts {
		members = append(members, Member{ContactID: contactID, EligibilityEvidenceHash: "eligibility-" + contactID})
	}
	return Command{CampaignID: f.CampaignID, SnapshotID: f.SnapshotID, MessageVersionID: f.MessageID, OrganisationID: f.OrganisationID, PurposeID: f.PurposeID, Channel: "WHATSAPP", MaximumUniqueRecipients: maximum, Members: members, ShardSize: 10_000, AsOf: f.AsOf}
}
func TestPostgreSQLReleaseReplayKeepsOneRecipientAndOneOutbox(t *testing.T) {
	db, ctx, cancel := openPostgresReleaseDB(t)
	defer cancel()
	defer db.Close()
	fixture := seedPostgresReleaseFixture(t, ctx, db, 1, 1)
	store := &PostgreSQLStore{DB: db}
	command := releaseCommand(fixture, 1)
	first, err := store.Authorise(ctx, command, SQLFinalEligibilityChecker{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Authorised != 1 || first.OutboxCreated != 1 {
		t.Fatalf("first release=%+v", first)
	}
	second, err := store.Authorise(ctx, command, SQLFinalEligibilityChecker{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Authorised != 0 || second.OutboxCreated != 0 || second.Existing != 1 {
		t.Fatalf("replay=%+v", second)
	}
	var recipients, outboxRows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_recipients WHERE campaign_id=$1::uuid AND contact_id=$2::uuid AND message_version_id=$3::uuid`, fixture.CampaignID, fixture.Contacts[0], fixture.MessageID).Scan(&recipients); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM transactional_outbox WHERE aggregate_type='CAMPAIGN_RECIPIENT' AND aggregate_id IN (SELECT id FROM campaign_recipients WHERE campaign_id=$1::uuid)`, fixture.CampaignID).Scan(&outboxRows); err != nil {
		t.Fatal(err)
	}
	if recipients != 1 || outboxRows != 1 {
		t.Fatalf("recipient rows=%d outbox rows=%d", recipients, outboxRows)
	}
}

func TestPostgreSQLReleaseEntitlementFailureRollsBackRecipientsAndOutbox(t *testing.T) {
	db, ctx, cancel := openPostgresReleaseDB(t)
	defer cancel()
	defer db.Close()
	fixture := seedPostgresReleaseFixture(t, ctx, db, 1, 2)
	store := &PostgreSQLStore{DB: db}
	_, err := store.Authorise(ctx, releaseCommand(fixture, 1), SQLFinalEligibilityChecker{})
	if !errors.Is(err, ErrEntitlementExceeded) {
		t.Fatalf("err=%v want=%v", err, ErrEntitlementExceeded)
	}
	var recipients, outboxRows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_recipients WHERE campaign_id=$1::uuid`, fixture.CampaignID).Scan(&recipients); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM transactional_outbox WHERE aggregate_type='CAMPAIGN_RECIPIENT' AND aggregate_id IN (SELECT id FROM campaign_recipients WHERE campaign_id=$1::uuid)`, fixture.CampaignID).Scan(&outboxRows); err != nil {
		t.Fatal(err)
	}
	if recipients != 0 || outboxRows != 0 {
		t.Fatalf("rollback leaked recipients=%d outbox=%d", recipients, outboxRows)
	}
}

func TestPostgreSQLReleaseBlocksInvalidOrganisationAndConsentReviewBasis(t *testing.T) {
	db, ctx, cancel := openPostgresReleaseDB(t)
	defer cancel()
	defer db.Close()
	fixture := seedPostgresReleaseFixture(t, ctx, db, 1, 1)
	store := &PostgreSQLStore{DB: db}
	command := releaseCommand(fixture, 1)

	if _, err := db.ExecContext(ctx, `UPDATE organisations SET status='SUSPENDED' WHERE id=$1::uuid`, fixture.OrganisationID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authorise(ctx, command, SQLFinalEligibilityChecker{}); err == nil {
		t.Fatal("release accepted a suspended campaign organisation")
	}
	if _, err := db.ExecContext(ctx, `UPDATE organisations SET status='ACTIVE' WHERE id=$1::uuid`, fixture.OrganisationID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE consent_reviews SET status='REJECTED',outcome='REJECTED' WHERE id=$1::uuid`, fixture.ReviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authorise(ctx, command, SQLFinalEligibilityChecker{}); err == nil {
		t.Fatal("release accepted a rejected consent-review basis")
	}
	var recipients int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_recipients WHERE campaign_id=$1::uuid`, fixture.CampaignID).Scan(&recipients); err != nil {
		t.Fatal(err)
	}
	if recipients != 0 {
		t.Fatalf("invalid release basis created %d recipient obligations", recipients)
	}
}

func TestPostgreSQLReleaseRejectsMessageVersionDifferentFromApprovedCampaignVersion(t *testing.T) {
	db, ctx, cancel := openPostgresReleaseDB(t)
	defer cancel()
	defer db.Close()
	fixture := seedPostgresReleaseFixture(t, ctx, db, 1, 1)
	var differentMessageID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&differentMessageID); err != nil {
		t.Fatal(err)
	}
	command := releaseCommand(fixture, 1)
	command.MessageVersionID = differentMessageID
	if _, err := (&PostgreSQLStore{DB: db}).Authorise(ctx, command, SQLFinalEligibilityChecker{}); !errors.Is(err, ErrReleaseConflict) {
		t.Fatalf("message-version mismatch err=%v want=%v", err, ErrReleaseConflict)
	}
	var recipients int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_recipients WHERE campaign_id=$1::uuid`, fixture.CampaignID).Scan(&recipients); err != nil {
		t.Fatal(err)
	}
	if recipients != 0 {
		t.Fatalf("message-version mismatch created %d recipient obligations", recipients)
	}
}

func TestPostgreSQLReleaseDoesNotAuthoriseDemographicMatchWithoutConsent(t *testing.T) {
	db, ctx, cancel := openPostgresReleaseDB(t)
	defer cancel()
	defer db.Close()
	fixture := seedPostgresReleaseFixture(t, ctx, db, 1, 1)
	contactID := fixture.Contacts[0]
	if _, err := db.ExecContext(ctx, `UPDATE contacts SET reported_age=35,age_recorded_at=$2::date,gender_code='MALE' WHERE id=$1::uuid`, contactID, fixture.AsOf); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM consent_grants WHERE contact_id=$1::uuid`, contactID); err != nil {
		t.Fatal(err)
	}
	result, err := (&PostgreSQLStore{DB: db}).Authorise(ctx, releaseCommand(fixture, 1), SQLFinalEligibilityChecker{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Authorised != 0 || result.Excluded != 1 || result.ExclusionReasons["NO_ACTIVE_CONSENT"] != 1 {
		t.Fatalf("demographic-only release result=%+v", result)
	}
	var recipients int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_recipients WHERE campaign_id=$1::uuid`, fixture.CampaignID).Scan(&recipients); err != nil {
		t.Fatal(err)
	}
	if recipients != 0 {
		t.Fatalf("demographic-only release created %d obligations", recipients)
	}
}

func TestPostgreSQLConcurrentReleaseCannotOverAllocateEntitlement(t *testing.T) {
	db, ctx, cancel := openPostgresReleaseDB(t)
	defer cancel()
	defer db.Close()
	fixture := seedPostgresReleaseFixture(t, ctx, db, 1, 2)
	store := &PostgreSQLStore{DB: db}
	base := releaseCommand(fixture, 1)
	commands := []Command{base, base}
	commands[0].Members = []Member{{ContactID: fixture.Contacts[0], EligibilityEvidenceHash: "eligibility-" + fixture.Contacts[0]}}
	commands[1].Members = []Member{{ContactID: fixture.Contacts[1], EligibilityEvidenceHash: "eligibility-" + fixture.Contacts[1]}}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, command := range commands {
		wg.Add(1)
		go func(command Command) {
			defer wg.Done()
			_, err := store.Authorise(ctx, command, SQLFinalEligibilityChecker{})
			errs <- err
		}(command)
	}
	wg.Wait()
	close(errs)
	successes, exceeded := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrEntitlementExceeded):
			exceeded++
		default:
			t.Fatalf("unexpected concurrent release error: %v", err)
		}
	}
	if successes != 1 || exceeded != 1 {
		t.Fatalf("successes=%d entitlementExceeded=%d want=1/1", successes, exceeded)
	}
	var recipients, outboxRows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_recipients WHERE campaign_id=$1::uuid`, fixture.CampaignID).Scan(&recipients); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM transactional_outbox WHERE aggregate_type='CAMPAIGN_RECIPIENT' AND aggregate_id IN (SELECT id FROM campaign_recipients WHERE campaign_id=$1::uuid)`, fixture.CampaignID).Scan(&outboxRows); err != nil {
		t.Fatal(err)
	}
	if recipients != 1 || outboxRows != 1 {
		t.Fatalf("concurrent entitlement leaked recipients=%d outbox=%d", recipients, outboxRows)
	}
}

func TestPostgreSQLReleaseEntitlementIsBoundToCampaignOrganisationSnapshotAndMessage(t *testing.T) {
	db, ctx, cancel := openPostgresReleaseDB(t)
	defer cancel()
	defer db.Close()
	fixture := seedPostgresReleaseFixture(t, ctx, db, 1, 1)
	store := &PostgreSQLStore{DB: db}
	var wrongOrganisation, wrongSnapshot, wrongMessage, wrongCampaign string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&wrongOrganisation, &wrongSnapshot, &wrongMessage, &wrongCampaign); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*Command)
	}{
		{"organisation", func(c *Command) { c.OrganisationID = wrongOrganisation }},
		{"snapshot", func(c *Command) { c.SnapshotID = wrongSnapshot }},
		{"message", func(c *Command) { c.MessageVersionID = wrongMessage }},
		{"campaign", func(c *Command) { c.CampaignID = wrongCampaign }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			command := releaseCommand(fixture, 1)
			tc.mutate(&command)
			if _, err := store.Authorise(ctx, command, SQLFinalEligibilityChecker{}); err == nil {
				t.Fatalf("release accepted entitlement reused with mismatched %s", tc.name)
			}
		})
	}
}
