package materialisation

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	_ "campaign-platform/internal/persistence/database"
	"campaign-platform/internal/segment"
)

func TestPostgreSQLMaterialisationCommitsFrozenSnapshot(t *testing.T) {
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
	now := time.Now().UTC().Truncate(time.Microsecond)
	var actorID, orgID, purposeID, campaignID, contactA, contactB string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(
		&actorID, &orgID, &purposeID, &campaignID, &contactA, &contactB,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'SEG materialisation actor','DISABLED',false)`, actorID, "seg-materialisation-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "SEG materialisation org "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'SEG materialisation purpose','WHATSAPP','v1')`, purposeID, orgID, "SEG_MAT_"+strings.ReplaceAll(purposeID, "-", "")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'SEG materialisation',$3::uuid,'DRAFT',2)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	for index, contactID := range []string{contactA, contactB} {
		lookup := []byte("seg-materialisation-" + contactID)
		if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,$2,$3,$4,'ACTIVE',$5)`, contactID, []byte("cipher-"+contactID), lookup, "***100"+string(rune('1'+index)), now); err != nil {
			t.Fatal(err)
		}
	}
	if contactA > contactB {
		contactA, contactB = contactB, contactA
	}
	definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorEquals, Values: []any{"NG"}}}}
	repo := &PostgreSQLRepository{DB: db}
	service := &MaterialisationService{Repository: repo, Clock: func() time.Time { return now }}
	job, err := service.Schedule(ctx, ScheduleMaterialisation{
		CampaignID: campaignID, Definition: definition, DefinitionVersion: 1,
		Eligibility:          cohort.EligibilityContext{OrganisationID: orgID, PurposeID: purposeID, Channel: "WHATSAPP", AsOf: now},
		ConsentPolicyVersion: "consent-v1", ConfigurationVersion: "config-v1", RequestedBy: actorID, ExpectedCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.Claim(ctx, "seg-worker", 1, time.Minute, now)
	if err != nil || len(claimed) != 1 || claimed[0].ID != job.ID {
		t.Fatalf("claim materialisation: jobs=%+v err=%v", claimed, err)
	}
	builder, err := segment.NewBuilder(campaignID, "", definition, 1, "consent-v1", "config-v1", actorID)
	if err != nil {
		t.Fatal(err)
	}
	members := []segment.Member{{ContactID: contactA, EligibilityEvidenceHash: strings.Repeat("a", 64)}, {ContactID: contactB, EligibilityEvidenceHash: strings.Repeat("b", 64)}}
	for _, member := range members {
		if err := builder.Add(member.ContactID, member.EligibilityEvidenceHash); err != nil {
			t.Fatal(err)
		}
	}
	lastContactID, rollingHash, processedCount := builder.Checkpoint()
	checkpointed, err := repo.AppendMembers(ctx, job.ID, claimed[0].LeaseToken, members, lastContactID, rollingHash, processedCount, now)
	if err != nil {
		t.Fatal(err)
	}
	if checkpointed.Status != MaterialisationPending || checkpointed.LeaseOwner != "" || checkpointed.LeaseExpiresAt != nil {
		t.Fatalf("checkpointed PostgreSQL job did not return to pending: %+v", checkpointed)
	}
	reclaimed, err := repo.Claim(ctx, "seg-worker-2", 1, time.Minute, now)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].ID != job.ID || reclaimed[0].LeaseToken <= claimed[0].LeaseToken {
		t.Fatalf("reclaim materialisation: jobs=%+v err=%v", reclaimed, err)
	}
	snapshot, err := builder.Finalise(now)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := repo.CommitSnapshot(ctx, job.ID, reclaimed[0].LeaseToken, snapshot, now)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != MaterialisationCompleted || completed.SnapshotID != snapshot.ID || completed.ProcessedCount != 2 {
		t.Fatalf("unexpected completed materialisation: %+v", completed)
	}
	var memberCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audience_snapshot_members WHERE snapshot_id=$1::uuid`, snapshot.ID).Scan(&memberCount); err != nil {
		t.Fatal(err)
	}
	if memberCount != 2 {
		t.Fatalf("committed snapshot members=%d want=2", memberCount)
	}
}
