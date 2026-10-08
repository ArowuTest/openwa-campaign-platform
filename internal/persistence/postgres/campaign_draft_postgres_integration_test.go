package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	_ "campaign-platform/internal/persistence/database"
	"campaign-platform/internal/sender"
	"campaign-platform/internal/shared/id"
)

type draftSQLFixture struct {
	owner, db *sql.DB
	ctx       context.Context
	repo      *CampaignRepository
	c         campaign.Campaign
}

func draftGuardDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	u, err := url.Parse(dsn)
	expected := os.Getenv("POSTGRES_CAMPAIGN_DRAFT_EXPECTED_DATABASE")
	marker := os.Getenv("POSTGRES_CAMPAIGN_DRAFT_EXPECTED_MARKER")
	port := os.Getenv("POSTGRES_CAMPAIGN_DRAFT_EXPECTED_PORT")
	if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() != "127.0.0.1" || u.Port() != port || port != "55496" || (expected != "openwa_campaign_draft_20261008" && expected != "openwa_campaign_draft_u2_normal_20261008" && expected != "openwa_campaign_draft_u2_race_20261008") || strings.TrimPrefix(u.Path, "/") != expected || marker == "" || os.Getenv("POSTGRES_CAMPAIGN_DRAFT_DISPOSABLE") != "1" {
		t.Fatal("draft fixture target guard rejected connection")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal("draft fixture driver unavailable")
	}
	t.Cleanup(func() { db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var name, host, liveMarker string
	var livePort int
	if err := db.QueryRowContext(ctx, `SELECT current_database(),host(inet_server_addr()),inet_server_port(),(SELECT marker FROM public.draft_test_harness_marker WHERE marker=$1)`, marker).Scan(&name, &host, &livePort, &liveMarker); err != nil {
		t.Fatal("draft fixture identity/marker probe failed")
	}
	if name != expected || host != "127.0.0.1" || livePort != 55496 || liveMarker != marker {
		t.Fatal("draft fixture live target mismatch")
	}
	return db
}
func draftSQLRoles(t *testing.T, run func(*testing.T, *draftSQLFixture)) {
	t.Helper()
	appDSN, ownerDSN := os.Getenv("POSTGRES_CAMPAIGN_DRAFT_DATABASE_URL"), os.Getenv("POSTGRES_CAMPAIGN_DRAFT_OWNER_DATABASE_URL")
	if appDSN == "" && ownerDSN == "" {
		t.Skip("dedicated PostgreSQL draft fixture not opted in")
	}
	if appDSN == "" || ownerDSN == "" {
		t.Fatal("both draft fixture role lanes are required")
	}
	for _, lane := range []struct{ name, dsn string }{{"owner", ownerDSN}, {"application", appDSN}} {
		t.Run(lane.name, func(t *testing.T) {
			owner := draftGuardDB(t, ownerDSN)
			db := draftGuardDB(t, lane.dsn)
			if lane.name == "application" {
				probeCtx, cancelProbe := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancelProbe()
				var member, super, createdb, createrole, bypass, owns, createSchema bool
				if err := db.QueryRowContext(probeCtx, `SELECT pg_has_role(current_user,'campaign_control_api','MEMBER'),rolsuper,rolcreatedb,rolcreaterole,rolbypassrls,EXISTS(SELECT 1 FROM pg_class WHERE oid IN ('campaigns'::regclass,'campaign_material_change_events'::regclass) AND relowner=(SELECT oid FROM pg_roles WHERE rolname=current_user)),has_schema_privilege(current_user,'public','CREATE') FROM pg_roles WHERE rolname=current_user`).Scan(&member, &super, &createdb, &createrole, &bypass, &owns, &createSchema); err != nil {
					t.Fatal("draft app privilege probe failed")
				}
				if !member || super || createdb || createrole || bypass || owns || createSchema {
					t.Fatal("draft app identity is privileged or lacks required role")
				}
			}
			f := newDraftSQLFixture(t, owner, db)
			run(t, f)
		})
	}
}
func newDraftSQLFixture(t *testing.T, owner, db *sql.DB) *draftSQLFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	ids := make([]string, 7)
	for i := range ids {
		v, err := id.New()
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = v
	}
	actor, org, review, purpose, pool, definition, campaignID := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6]
	queries := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Draft fixture','DISABLED',false)`, []any{actor, "draft-" + actor + "@internal.invalid"}},
		{`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, []any{org, "Draft fixture " + org}},
		{`INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,status) VALUES($1::uuid,$2::uuid,'Draft fixture','WHATSAPP','DIRECT','w1','DRAFT')`, []any{review, org}},
		{`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Draft fixture','WHATSAPP','w1',$4::uuid)`, []any{purpose, org, "DRAFT_" + purpose, review}},
		{`INSERT INTO sender_pools(id,name,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,'ACTIVE',60,1000)`, []any{pool, "Draft pool " + pool}},
		{`INSERT INTO provider_capability_definitions(id,provider,channel,engine,adapter_version,capabilities,status,effective_from,version,created_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,'OPENWA','WHATSAPP','BAILEYS','v1','{SEND_TEXT}'::text[],'DRAFT',$2,1,$3::uuid,$3::uuid,'Draft fixture provider',now(),now())`, []any{definition, time.Now().UTC().Add(-time.Hour), actor}},
	}
	for _, q := range queries {
		if _, err := owner.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("draft fixture insert failed: %v", err)
		}
	}
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	c := campaign.Campaign{ID: campaignID, OrganisationID: org, PurposeID: purpose, ConsentReviewID: review, Name: "SQL saved draft", Status: campaign.StatusDraft, Timezone: "UTC",
		MaximumUniqueRecipients: 50, MaximumMessagesPerRecipient: 1, SenderPool: pool, CreatedBy: actor, CreatedAt: now, UpdatedAt: now, Version: 1,
		Transport: campaign.TransportSelection{Channel: "WHATSAPP", Provider: campaign.ProviderOpenWA, Engine: campaign.EngineBaileys, RoutingMode: campaign.RoutingSenderPool,
			GatewayPoolID: "50000000-0000-4000-8000-000000000001", GatewayPoolVersion: 1, SenderPoolID: pool, AdapterVersion: "v1", ProviderDefinitionID: definition, ProviderDefinitionVersion: 1,
			RequiredCapabilities: []string{"SEND_TEXT"}, FallbackMode: campaign.FallbackNone, RoutingPolicyVersion: "r1", CapacityEvidenceVersion: "c1"}}
	repo := &CampaignRepository{DB: db}
	if err := repo.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	// Canonical append-only events forbid per-row cleanup. The harness tears down
	// this entire independently marked database after all role lanes complete.
	return &draftSQLFixture{owner: owner, db: db, ctx: ctx, repo: repo, c: c}
}
func draftSQLInput(c campaign.Campaign) campaign.DraftSaveInput {
	return campaign.DraftSaveInput{ExpectedVersion: c.Version, ActorID: c.CreatedBy, Reason: "Correct SQL draft fields", Name: c.Name, OrganisationID: c.OrganisationID, PurposeID: c.PurposeID, ConsentReviewID: c.ConsentReviewID, RequestedStartAt: c.RequestedStartAt, CompletionDeadlineAt: c.CompletionDeadlineAt, Timezone: c.Timezone, QuietHoursStart: c.QuietHoursStart, QuietHoursEnd: c.QuietHoursEnd, MaximumUniqueRecipients: c.MaximumUniqueRecipients, MaximumMessagesPerRecipient: 1, Transport: c.Transport}
}
func draftSQLAssertUnchanged(t *testing.T, f *draftSQLFixture, c campaign.Campaign) {
	t.Helper()
	got, err := f.repo.Get(f.ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := f.repo.ListMaterialChanges(f.ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, c) || len(events) != 0 {
		t.Fatalf("rejected save changed row/evidence: version %d events %d", got.Version, len(events))
	}
}
func TestPostgreSQLDraftSaveReload(t *testing.T) {
	draftSQLRoles(t, func(t *testing.T, f *draftSQLFixture) {
		input := draftSQLInput(f.c)
		input.Name = "SQL edited draft"
		input.MaximumUniqueRecipients = 123
		input.Timezone = "Africa/Lagos"
		input.QuietHoursStart = "22:00"
		input.QuietHoursEnd = "06:00"
		selectedPool, err := id.New()
		if err != nil {
			t.Fatal(err)
		}
		selectedGateway, err := id.New()
		if err != nil {
			t.Fatal(err)
		}
		selectedDefinition, err := id.New()
		if err != nil {
			t.Fatal(err)
		}
		fixtures := []struct {
			q string
			a []any
		}{
			{`INSERT INTO sender_pools(id,name,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,'ACTIVE',70,5000)`, []any{selectedPool, "Selected draft pool " + selectedPool}},
			{`INSERT INTO gateway_pools(id,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,version) VALUES($1::uuid,$2,'OPENWA','WHATSAPP_WEB_JS','v2','ACTIVE','["SEND_IMAGE","DELIVERY_EVENTS"]'::jsonb,1,2)`, []any{selectedGateway, "Selected draft gateway " + selectedGateway}},
			{`INSERT INTO provider_capability_definitions(id,provider,channel,engine,adapter_version,capabilities,status,effective_from,version,created_by,reason,created_at,updated_at) VALUES($1::uuid,'OPENWA','WHATSAPP','WHATSAPP_WEB_JS','v2','{SEND_IMAGE,DELIVERY_EVENTS}'::text[],'DRAFT',now()-interval '1 hour',2,$2::uuid,'Selected fixture provider',now(),now())`, []any{selectedDefinition, f.c.CreatedBy}},
		}
		for _, q := range fixtures {
			if _, err := f.owner.ExecContext(f.ctx, q.q, q.a...); err != nil {
				t.Fatal(err)
			}
		}
		input.Transport.Engine = campaign.EngineWhatsAppWebJS
		input.Transport.AdapterVersion = "v2"
		input.Transport.SenderPoolID = selectedPool
		input.Transport.GatewayPoolID = selectedGateway
		input.Transport.GatewayPoolVersion = 2
		input.Transport.ProviderDefinitionID = selectedDefinition
		input.Transport.ProviderDefinitionVersion = 2
		input.Transport.RequiredCapabilities = []string{"SEND_IMAGE", "DELIVERY_EVENTS"}
		input.Transport.RoutingPolicyVersion = "r2"
		input.Transport.CapacityEvidenceVersion = "c2"
		start := time.Date(2026, 10, 9, 12, 0, 0, 0, time.FixedZone("offset", 3600))
		end := start.Add(time.Hour)
		input.RequestedStartAt = &start
		input.CompletionDeadlineAt = &end
		next, event, err := f.c.SaveDraft(input, time.Date(2026, 10, 8, 1, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		persisted, err := f.repo.SaveDraft(f.ctx, next, event, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !persisted.UpdatedAt.After(f.c.UpdatedAt) || persisted.UpdatedAt.Location() != time.UTC {
			t.Fatal("trigger-authoritative updated timestamp was not advanced UTC")
		}
		// Every editable/identity/version field must match the domain result.
		next.UpdatedAt = persisted.UpdatedAt
		if !reflect.DeepEqual(persisted, next) {
			t.Fatalf("atomic snapshot lost fields: %v", draftSQLDifferentFields(persisted, next))
		}
		got, err := (&CampaignRepository{DB: f.db}).Get(f.ctx, f.c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, next) {
			t.Fatalf("new repository did not reload every saved field: differing fields=%v", draftSQLDifferentFields(got, next))
		}
		events, err := f.repo.ListMaterialChanges(f.ctx, f.c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 || events[0].Sequence != 1 || !reflect.DeepEqual(events[0].ChangedFields, []string{"NAME", "SCHEDULE", "TRANSPORT", "ENTITLEMENT"}) {
			t.Fatalf("audit=%+v", events)
		}
		input = draftSQLInput(got)
		input.RequestedStartAt = nil
		input.CompletionDeadlineAt = nil
		input.QuietHoursStart = ""
		input.QuietHoursEnd = ""
		cleared, ev, err := got.SaveDraft(input, time.Date(2026, 10, 8, 2, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.SaveDraft(f.ctx, cleared, ev, 2); err != nil {
			t.Fatal(err)
		}
		got, err = f.repo.Get(f.ctx, got.ID)
		if err != nil || got.RequestedStartAt != nil || got.CompletionDeadlineAt != nil || got.QuietHoursStart != "" || got.QuietHoursEnd != "" {
			t.Fatalf("clear persisted incorrectly: %+v %v", got, err)
		}
		input = draftSQLInput(got)
		input.CompletionDeadlineAt = &end
		incomplete, ev, err := got.SaveDraft(input, time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.SaveDraft(f.ctx, incomplete, ev, 3); err != nil {
			t.Fatal(err)
		}
		got, err = f.repo.Get(f.ctx, got.ID)
		if err != nil || got.RequestedStartAt != nil || got.CompletionDeadlineAt == nil {
			t.Fatal("incomplete window did not persist")
		}
	})
}
func TestPostgreSQLDraftSaveConcurrentVersion(t *testing.T) {
	draftSQLRoles(t, func(t *testing.T, f *draftSQLFixture) {
		ready := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, name := range []string{"First editor", "Second editor"} {
			input := draftSQLInput(f.c)
			input.Name = name
			next, event, err := f.c.SaveDraft(input, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-ready
				_, err := f.repo.SaveDraft(f.ctx, next, event, 1)
				errs <- err
			}()
		}
		close(ready)
		wg.Wait()
		close(errs)
		winners, conflicts := 0, 0
		for err := range errs {
			if err == nil {
				winners++
			} else if errors.Is(err, campaign.ErrConflict) {
				conflicts++
			} else {
				t.Fatalf("unexpected concurrent save error: %v", err)
			}
		}
		got, err := f.repo.Get(f.ctx, f.c.ID)
		if err != nil {
			t.Fatal(err)
		}
		events, err := f.repo.ListMaterialChanges(f.ctx, f.c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if winners != 1 || conflicts != 1 || got.Version != 2 || len(events) != 1 {
			t.Fatalf("winners=%d conflicts=%d version=%d events=%d", winners, conflicts, got.Version, len(events))
		}
	})
}
func TestPostgreSQLDraftSaveTransitionRace(t *testing.T) {
	draftSQLRoles(t, func(t *testing.T, f *draftSQLFixture) {
		for _, noop := range []bool{false, true} {
			t.Run(fmt.Sprintf("no-op-%t", noop), func(t *testing.T) {
				if noop {
					f = newDraftSQLFixture(t, f.owner, f.db)
				}
				input := draftSQLInput(f.c)
				if !noop {
					input.Name = "Late draft editor"
				}
				next, event, err := f.c.SaveDraft(input, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				transitioned := f.c
				transitioned.Status = campaign.StatusConsentReviewPending
				transitioned.Version = 2
				transitioned.UpdatedAt = time.Now().UTC()
				if err := f.repo.CompareAndSwap(f.ctx, transitioned, 1); err != nil {
					t.Fatal(err)
				}
				if _, err := f.repo.SaveDraft(f.ctx, next, event, 1); !errors.Is(err, campaign.ErrConflict) {
					t.Fatalf("transition race=%v", err)
				}
				got, err := f.repo.Get(f.ctx, f.c.ID)
				if err != nil {
					t.Fatal(err)
				}
				events, err := f.repo.ListMaterialChanges(f.ctx, f.c.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Status != campaign.StatusConsentReviewPending || got.Version != 2 || got.Name != f.c.Name || len(events) != 0 {
					t.Fatal("late save overwrote transition")
				}
			})
		}
	})
}
func TestPostgreSQLDraftSaveNoOp(t *testing.T) {
	draftSQLRoles(t, func(t *testing.T, f *draftSQLFixture) {
		before, err := f.repo.Get(f.ctx, f.c.ID)
		if err != nil {
			t.Fatal(err)
		}
		next, event, err := before.SaveDraft(draftSQLInput(before), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.SaveDraft(f.ctx, next, event, 1); err != nil {
			t.Fatal(err)
		}
		draftSQLAssertUnchanged(t, f, before)
		if _, err := f.repo.SaveDraft(f.ctx, next, event, 2); !errors.Is(err, campaign.ErrConflict) {
			t.Fatalf("stale no-op=%v", err)
		}
	})
}
func TestPostgreSQLDraftSaveAuditRollback(t *testing.T) {
	draftSQLRoles(t, func(t *testing.T, f *draftSQLFixture) {
		before, err := f.repo.Get(f.ctx, f.c.ID)
		if err != nil {
			t.Fatal(err)
		}
		warm := draftSQLInput(before)
		warm.Name = "Proven save before forced failure"
		saved, proof, err := before.SaveDraft(warm, time.Now().UTC().Truncate(time.Microsecond))
		if err != nil {
			t.Fatal(err)
		}
		atomicSaved, err := f.repo.SaveDraft(f.ctx, saved, proof, 1)
		if err != nil {
			t.Fatalf("working save required before audit failure injection: %v", err)
		}
		saved.UpdatedAt = atomicSaved.UpdatedAt
		if !reflect.DeepEqual(saved, atomicSaved) || !atomicSaved.UpdatedAt.After(f.c.UpdatedAt) || atomicSaved.UpdatedAt.Location() != time.UTC {
			t.Fatal("warm atomic snapshot differs outside canonical updated timestamp")
		}
		before, err = f.repo.Get(f.ctx, f.c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, saved) || before.Name != warm.Name || before.Version != 2 {
			t.Fatalf("working warm save did not persist exact row/version: differing fields=%v", draftSQLDifferentFields(before, saved))
		}
		beforeEvents, err := f.repo.ListMaterialChanges(f.ctx, f.c.ID)
		if err != nil || len(beforeEvents) != 1 {
			t.Fatal("working audit insert required before failure injection")
		}
		suffix := strings.ReplaceAll(f.c.ID, "-", "")
		function := "draft_fail_" + suffix
		trigger := "draft_trigger_" + suffix
		ddl := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN IF NEW.campaign_id=''%s''::uuid THEN RAISE EXCEPTION ''draft fixture forced insert failure'';END IF;RETURN NEW;END'`, function, f.c.ID)
		if _, err := f.owner.ExecContext(f.ctx, ddl); err != nil {
			t.Fatal(err)
		}
		triggerDDL := fmt.Sprintf("CREATE TRIGGER %s BEFORE INSERT ON campaign_material_change_events FOR EACH ROW EXECUTE FUNCTION %s()", trigger, function)
		t.Cleanup(func() {
			draftGuardDB(t, os.Getenv("POSTGRES_CAMPAIGN_DRAFT_OWNER_DATABASE_URL"))
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := f.owner.ExecContext(ctx, fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON campaign_material_change_events", trigger)); err != nil {
				t.Error("scoped draft trigger cleanup failed")
			}
			if _, err := f.owner.ExecContext(ctx, fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", function)); err != nil {
				t.Error("scoped draft function cleanup failed")
			}
		})
		if _, err := f.owner.ExecContext(f.ctx, triggerDDL); err != nil {
			t.Fatal(err)
		}
		input := draftSQLInput(before)
		input.Name = "Must roll back"
		next, event, err := before.SaveDraft(input, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.SaveDraft(f.ctx, next, event, before.Version); err == nil {
			t.Fatal("audit failure save succeeded")
		} else if !strings.Contains(err.Error(), "draft fixture forced insert failure") || !strings.Contains(err.Error(), "SQLSTATE P0001") {
			t.Fatal("save failed before the injected append-only audit insert")
		}
		after, err := f.repo.Get(f.ctx, before.ID)
		if err != nil {
			t.Fatal(err)
		}
		afterEvents, err := f.repo.ListMaterialChanges(f.ctx, before.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeEvents, afterEvents) {
			t.Fatal("audit insert failure did not roll back row and event together")
		}
	})
}
func TestPostgreSQLDraftSaveLockedReferences(t *testing.T) {
	draftSQLRoles(t, func(t *testing.T, f *draftSQLFixture) {
		before, err := f.repo.Get(f.ctx, f.c.ID)
		if err != nil {
			t.Fatal(err)
		}
		input := draftSQLInput(before)
		input.Name = "Reparent attempt"
		next, event, err := before.SaveDraft(input, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"organisation", "purpose", "review"} {
			candidate := next
			switch field {
			case "organisation":
				candidate.OrganisationID = before.Transport.SenderPoolID
			case "purpose":
				candidate.PurposeID = before.Transport.SenderPoolID
			case "review":
				candidate.ConsentReviewID = before.Transport.SenderPoolID
			}
			if _, err := f.repo.SaveDraft(f.ctx, candidate, event, 1); !errors.Is(err, campaign.ErrDraftReferencesLocked) {
				t.Fatalf("locked %s=%v", field, err)
			}
			draftSQLAssertUnchanged(t, f, before)
		}
		var review, purpose int
		if err := f.owner.QueryRowContext(f.ctx, `SELECT (SELECT count(*) FROM consent_reviews WHERE id=$1::uuid),(SELECT count(*) FROM consent_purposes WHERE id=$2::uuid AND consent_review_id=$1::uuid)`, before.ConsentReviewID, before.PurposeID).Scan(&review, &purpose); err != nil || review != 1 || purpose != 1 {
			t.Fatal("locked downstream governance changed")
		}
	})
}
func TestPostgreSQLDraftPoolDirectLookup(t *testing.T) {
	draftSQLRoles(t, func(t *testing.T, f *draftSQLFixture) {
		store := &sender.PostgreSQLGovernanceStore{DB: f.db}
		reader, ok := any(store).(interface {
			GetPool(context.Context, string) (sender.Pool, error)
		})
		if !ok {
			t.Fatal("PostgreSQL direct sender-pool lookup is unavailable")
		}
		got, err := reader.GetPool(f.ctx, f.c.Transport.SenderPoolID)
		if err != nil || got.ID != f.c.Transport.SenderPoolID || got.Status != "ACTIVE" {
			t.Fatalf("direct pool lookup=%+v %v", got, err)
		}
		missing, _ := id.New()
		if _, err := reader.GetPool(f.ctx, missing); !errors.Is(err, sender.ErrSenderNotFound) {
			t.Fatalf("missing pool=%v", err)
		}
	})
}

func draftSQLDifferentFields(a, b campaign.Campaign) []string {
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	typ := av.Type()
	fields := []string{}
	for i := 0; i < av.NumField(); i++ {
		if !reflect.DeepEqual(av.Field(i).Interface(), bv.Field(i).Interface()) {
			fields = append(fields, typ.Field(i).Name)
		}
	}
	return fields
}
