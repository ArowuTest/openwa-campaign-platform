package segment

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLFrozenSnapshotMembershipRejectsMutation(t *testing.T) {
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
	var contactA, contactB, contactC string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(
		&actorID, &orgID, &purposeID, &campaignID, &contactA, &contactB, &contactC,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'SEG snapshot actor','DISABLED',false)`, actorID, "seg-snapshot-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "SEG snapshot org "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'SEG snapshot purpose','WHATSAPP','v1')`, purposeID, orgID, "SEG_SNAPSHOT_"+strings.ReplaceAll(purposeID, "-", "")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'SEG snapshot immutability',$3::uuid,'DRAFT',2)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	for index, contactID := range []string{contactA, contactB, contactC} {
		lookup := []byte("seg-snapshot-" + contactID)
		if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,$2,$3,$4,'ACTIVE',$5)`, contactID, []byte("cipher-"+contactID), lookup, "***000"+string(rune('1'+index)), time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorEquals, Values: []any{"NG"}}}}
	builder, err := NewBuilder(campaignID, "", definition, 1, "consent-v1", "config-v1", actorID)
	if err != nil {
		t.Fatal(err)
	}
	members := []Member{{ContactID: contactA, EligibilityEvidenceHash: strings.Repeat("a", 64)}, {ContactID: contactB, EligibilityEvidenceHash: strings.Repeat("b", 64)}}
	if members[0].ContactID > members[1].ContactID {
		members[0], members[1] = members[1], members[0]
	}
	for _, member := range members {
		if err := builder.Add(member.ContactID, member.EligibilityEvidenceHash); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := builder.Finalise(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	store := &PostgreSQLStore{DB: db}
	if _, created, err := store.EnsureWithMembers(ctx, snapshot, members); err != nil || !created {
		t.Fatalf("create frozen snapshot: created=%v err=%v", created, err)
	}
	loaded, err := store.Get(ctx, snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DefinitionVersion != 1 || loaded.ConsentPolicyVersion != "consent-v1" || loaded.ConfigurationVersion != "config-v1" || loaded.CreatedBy != actorID || loaded.CreatedAt.IsZero() {
		t.Fatalf("snapshot provenance incomplete after PostgreSQL reload: %+v", loaded)
	}
	allowed := make([]string, 0, 3)
	if _, err := db.ExecContext(ctx, `UPDATE audience_snapshot_members SET eligibility_evidence=jsonb_build_object('hash',$3::text) WHERE snapshot_id=$1::uuid AND contact_id=$2::uuid`, snapshot.ID, members[0].ContactID, strings.Repeat("f", 64)); err == nil {
		allowed = append(allowed, "UPDATE")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM audience_snapshot_members WHERE snapshot_id=$1::uuid AND contact_id=$2::uuid`, snapshot.ID, members[1].ContactID); err == nil {
		allowed = append(allowed, "DELETE")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_snapshot_members(snapshot_id,contact_id,eligibility_evidence) VALUES($1::uuid,$2::uuid,jsonb_build_object('hash',$3::text))`, snapshot.ID, contactC, strings.Repeat("c", 64)); err == nil {
		allowed = append(allowed, "INSERT")
	}
	if len(allowed) != 0 {
		t.Fatalf("frozen snapshot membership accepted post-freeze mutation(s): %v", allowed)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audience_snapshot_members WHERE snapshot_id=$1::uuid`, snapshot.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(members) {
		t.Fatalf("frozen snapshot member count=%d want=%d", count, len(members))
	}
}
