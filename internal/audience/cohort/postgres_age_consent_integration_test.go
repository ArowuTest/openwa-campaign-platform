package cohort

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLCohortAgeBoundariesStillRequireConsent(t *testing.T) {
	dsn := os.Getenv("POSTGRES_COHORT_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_COHORT_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	var actorID, orgID, purposeID string
	if err = db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&actorID, &orgID, &purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'SEG cohort actor','DISABLED',false)`, actorID, "seg-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "SEG cohort org "+orgID); err != nil {
		t.Fatal(err)
	}
	ages := []int{17, 18, 35, 36, 25}
	contacts := make([]string, len(ages))
	for i, age := range ages {
		if err = db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&contacts[i]); err != nil {
			t.Fatal(err)
		}
		lookup := []byte(fmt.Sprintf("seg-age-%d-%s", age, contacts[i]))
		masked := fmt.Sprintf("***%04d", age)
		if _, err = db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,reported_age,age_recorded_at,age_source,age_verified,profile_recorded_at) VALUES($1::uuid,$2,$3,$4,'ACTIVE',$5,$6::date,'SELF_DECLARED_IMPORT',false,$6)`, contacts[i], []byte("cipher-"+contacts[i]), lookup, masked, age, now.Add(-24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		var grantID string
		if err = db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&grantID); err != nil {
			t.Fatal(err)
		}
		clientID := "seg-grant-" + grantID
		fingerprint := fmt.Sprintf("fp-%d-%s", i, grantID)
		if _, err = db.ExecContext(ctx, `INSERT INTO consent_grants(id,contact_id,organisation_id,purpose_id,channel,wording_version,source_type,evidence_checksum,granted_at,effective_from,status,created_by,client_request_id,request_fingerprint,version) VALUES($1::uuid,$2::uuid,$3::uuid,$4,'WHATSAPP','v1','IMPORT',$5,$6,$6,'ACTIVE',$7::uuid,$8,$9,1)`, grantID, contacts[i], orgID, purposeID, "checksum-"+grantID, now.Add(-time.Hour), actorID, clientID, fingerprint); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	group := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{
		DefinitionCode: "REPORTED_AGE", Operator: audiencefilter.OperatorBetween, Values: []any{18, 35},
	}}}
	service := NewExecutionService(NewCompiler(registry), &PostgreSQLQueryRepository{DB: db})
	estimate, err := service.Estimate(ctx, group, EligibilityContext{OrganisationID: orgID, PurposeID: purposeID, Channel: "WHATSAPP", AsOf: now}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if estimate.EligibleCount != 2 {
		t.Fatalf("eligible age 18..35 with consent=%d want=2", estimate.EligibleCount)
	}
}
