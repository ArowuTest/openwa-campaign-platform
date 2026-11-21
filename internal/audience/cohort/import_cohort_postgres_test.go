package cohort

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	f "campaign-platform/internal/audience/filter"
	_ "campaign-platform/internal/persistence/database"
)

func TestUploadCohortRuleBounds(t *testing.T) {
	reg, err := f.NewRegistry(f.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	id := "aaaaaaaa-0000-4000-8000-000000000001"
	for _, tc := range []struct {
		value string
		valid bool
	}{{id + "/1/1", true}, {id + "/32/32", true}, {id + "/0/2", false}, {id + "/1/33", false}, {id + "/3/2", false}, {"not-an-import/1/2", false}} {
		group := f.Group{Join: f.JoinAnd, Rules: []f.Rule{{DefinitionCode: f.ImportCohortCode, Operator: f.OperatorEquals, Values: []any{tc.value}}}}
		if err := group.Validate(reg); (err == nil) != tc.valid {
			t.Errorf("%s: valid=%v err=%v", tc.value, tc.valid, err)
		}
	}
}

func TestPostgreSQLUploadCohortsPartitionOnlyEligibleImportMembers(t *testing.T) {
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
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// Shadow the migrated tables on this connection, retaining their real column
	// types. Nothing here can release messages or change shared fixture data.
	for _, table := range []string{"contacts", "organisations", "consent_grants", "suppressions", "organisation_policy_versions", "campaign_recipients", "campaigns", "audience_imports"} {
		if _, err := tx.ExecContext(ctx, "CREATE TEMP TABLE "+table+" AS SELECT * FROM public."+table+" WITH NO DATA"); err != nil {
			t.Fatal(err)
		}
	}
	org := "00000000-0000-4000-8000-000000000001"
	purpose := "00000000-0000-4000-8000-000000000002"
	upload := "00000000-0000-4000-8000-000000000003"
	other := "00000000-0000-4000-8000-000000000004"
	run := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	run("INSERT INTO organisations(id,status) VALUES ($1,'ACTIVE')", org)
	run("INSERT INTO audience_imports(id,organisation_id,purpose_id,channel,status) VALUES($1,$2,$3,'WHATSAPP','COMPLETED'),($4,$2,$3,'WHATSAPP','COMPLETED')", upload, org, purpose, other)
	run("INSERT INTO contacts(id,status) SELECT md5(i::text)::uuid,'ACTIVE' FROM generate_series(1,65) i")
	run(`INSERT INTO consent_grants(id,contact_id,organisation_id,purpose_id,channel,granted_at,status,source_import_id,source_type)
SELECT gen_random_uuid(),md5(i::text)::uuid,$1,$2,'WHATSAPP',now()-interval '1 day',CASE WHEN i=64 THEN 'REVOKED' ELSE 'ACTIVE' END,
CASE WHEN i=65 THEN $4::uuid ELSE $3::uuid END,'AUDIENCE_IMPORT' FROM generate_series(1,65) i`, org, purpose, upload, other)
	run("INSERT INTO suppressions(contact_id,active,effective_at,scope) VALUES(md5('63')::uuid,true,now()-interval '1 hour','GLOBAL')")
	reg, err := f.NewRegistry(f.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	compiler := NewCompiler(reg)
	eligibility := EligibilityContext{OrganisationID: org, PurposeID: purpose, Channel: "WHATSAPP", AsOf: time.Now().UTC()}
	read := func(part, total int) map[string]bool {
		t.Helper()
		group := f.Group{Join: f.JoinAnd, Rules: []f.Rule{{DefinitionCode: f.ImportCohortCode, Operator: f.OperatorEquals, Values: []any{fmt.Sprintf("%s/%d/%d", upload, part, total)}}}}
		query, err := compiler.Compile(group, eligibility)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := tx.QueryContext(ctx, query.SQL, query.Args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]bool{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			out[id] = true
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	all := read(1, 1)
	if len(all) != 62 {
		t.Fatalf("got %d eligible members, want 62", len(all))
	}
	union := map[string]bool{}
	for i := 1; i <= 3; i++ {
		part := read(i, 3)
		again := read(i, 3)
		if len(part) == 0 || len(part) != len(again) {
			t.Fatal("empty or unstable group")
		}
		for id := range part {
			if !all[id] || union[id] || !again[id] {
				t.Fatal("foreign, overlapping or unstable member")
			}
			union[id] = true
		}
	}
	if len(union) != len(all) {
		t.Fatal("split lost eligible contacts")
	}
	run("UPDATE audience_imports SET status='ROLLED_BACK' WHERE id=$1", upload)
	if len(read(1, 1)) != 0 {
		t.Fatal("rolled-back upload remained eligible")
	}
}
