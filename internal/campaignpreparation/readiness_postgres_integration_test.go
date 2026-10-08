package campaignpreparation_test

import (
	"campaign-platform/internal/campaign"
	prep "campaign-platform/internal/campaignpreparation"
	"campaign-platform/internal/commercial"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/execution"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/message"
	"campaign-platform/internal/organisation"
	_ "campaign-platform/internal/persistence/database"
	pg "campaign-platform/internal/persistence/postgres"
	"campaign-platform/internal/platform/httpserver"
	"campaign-platform/internal/platformpolicy"
	"campaign-platform/internal/provider"
	"campaign-platform/internal/segment"
	"campaign-platform/internal/sender"
	"campaign-platform/internal/shared/id"
	"campaign-platform/internal/storage"
	"campaign-platform/internal/testmessage"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

var readinessDatabases = map[string]string{
	"openwa_campaign_readiness_u3_normal_20261008": "ee75e8c0-72c4-4022-80e2-42625ac8a39e",
	"openwa_campaign_readiness_u3_race_20261008":   "bbfbdfab-a3a4-46c9-84c2-2cf934e4b581",
}

func readinessGuardDB(t *testing.T, dsn, role string) *sql.DB {
	t.Helper()
	expected := os.Getenv("POSTGRES_CAMPAIGN_PREPARATION_EXPECTED_DATABASE")
	marker := os.Getenv("POSTGRES_CAMPAIGN_PREPARATION_EXPECTED_MARKER")
	u, e := url.Parse(dsn)
	if e != nil || u == nil || u.User == nil || u.Scheme != "postgresql" || u.Hostname() != "127.0.0.1" || u.Port() != "55496" || strings.TrimPrefix(u.Path, "/") != expected || readinessDatabases[expected] == "" || readinessDatabases[expected] != marker || os.Getenv("POSTGRES_CAMPAIGN_PREPARATION_EXPECTED_PORT") != "55496" || os.Getenv("POSTGRES_CAMPAIGN_PREPARATION_DISPOSABLE") != "1" || u.User.Username() != role || u.RawQuery != "sslmode=disable" {
		t.Fatal("readiness fixture exact target guard rejected")
	}
	db, e := sql.Open("postgres", dsn)
	if e != nil {
		t.Fatal("readiness driver unavailable")
	}
	t.Cleanup(func() { db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var name, host, user, sessionUser, liveMarker string
	var port int
	e = db.QueryRowContext(ctx, `SELECT current_database(),host(inet_server_addr()),inet_server_port(),current_user,session_user,(SELECT marker FROM public.draft_test_harness_marker WHERE marker=$1)`, marker).Scan(&name, &host, &port, &user, &sessionUser, &liveMarker)
	if e != nil || name != expected || host != "127.0.0.1" || port != 55496 || liveMarker != marker || user != role || sessionUser != role {
		t.Fatal("readiness live target/role proof failed")
	}
	if role == "draft_test_app_20261008" {
		var member, super, createDB, createRole, bypass, owns, createSchema bool
		e = db.QueryRowContext(ctx, `SELECT (pg_has_role(current_user,'campaign_control_api','MEMBER') AND ARRAY(SELECT r.rolname::text FROM pg_auth_members m JOIN pg_roles r ON r.oid=m.roleid WHERE m.member=(SELECT oid FROM pg_roles WHERE rolname=current_user) ORDER BY r.rolname)=ARRAY['campaign_control_api']::text[]),rolsuper,rolcreatedb,rolcreaterole,rolbypassrls,(EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relowner=(SELECT oid FROM pg_roles WHERE rolname=current_user)) OR EXISTS(SELECT 1 FROM pg_database WHERE datname=current_database() AND datdba=(SELECT oid FROM pg_roles WHERE rolname=current_user))),has_schema_privilege(current_user,'public','CREATE') FROM pg_roles WHERE rolname=current_user`).Scan(&member, &super, &createDB, &createRole, &bypass, &owns, &createSchema)
		if e != nil || !member || super || createDB || createRole || bypass || owns || createSchema {
			t.Fatal("readiness application role privilege proof failed")
		}
	}
	t.Logf("guarded database=%s port=%d marker=%s role=%s", name, port, liveMarker, user)
	return db
}
func readinessSQLRoles(t *testing.T, run func(*testing.T, *sql.DB, *sql.DB)) {
	t.Helper()
	app, owner := os.Getenv("POSTGRES_CAMPAIGN_PREPARATION_DATABASE_URL"), os.Getenv("POSTGRES_CAMPAIGN_PREPARATION_OWNER_DATABASE_URL")
	if app == "" && owner == "" {
		t.Skip("dedicated readiness database not opted in")
	}
	if app == "" || owner == "" {
		t.Fatal("both readiness roles required")
	}
	for _, role := range []string{"owner", "application"} {
		t.Run(role, func(t *testing.T) {
			ownerDB := readinessGuardDB(t, owner, "draft_fixture_owner_20261008")
			dsn, user := owner, "draft_fixture_owner_20261008"
			if role == "application" {
				dsn, user = app, "draft_test_app_20261008"
			}
			db := readinessGuardDB(t, dsn, user)
			run(t, ownerDB, db)
		})
	}
}
func sqlID(t *testing.T) string {
	t.Helper()
	v, e := id.New()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func sqlMust(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, e := db.ExecContext(ctx, q, args...); e != nil {
		t.Fatalf("synthetic fixture statement failed: %v", e)
	}
}

type sqlPreparationFixture struct {
	svc                         *prep.Service
	campaign                    campaign.Campaign
	draft                       campaign.Campaign
	now                         time.Time
	pool, gateway, review, plan string
	objects                     *fixtureObjects
}
type fixtureObjects struct {
	sha    string
	outage bool
	writes int
}

func (f *fixtureObjects) Put(context.Context, string, io.Reader, int64) (storage.Metadata, error) {
	f.writes++
	return storage.Metadata{}, errors.New("fixture write forbidden")
}
func (f *fixtureObjects) Open(context.Context, string) (storage.ReadSeekCloser, storage.Metadata, error) {
	return nil, storage.Metadata{}, errors.New("fixture content read forbidden")
}
func (f *fixtureObjects) Stat(context.Context, string) (storage.Metadata, error) {
	if f.outage {
		return storage.Metadata{}, errors.New("private object outage")
	}
	return storage.Metadata{Size: 3, SHA256: f.sha}, nil
}
func (f *fixtureObjects) Delete(context.Context, string) error {
	f.writes++
	return errors.New("fixture write forbidden")
}

func newSQLPreparationFixture(t *testing.T, owner, db *sql.DB, media bool) *sqlPreparationFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	start, end := now.Add(time.Hour), now.Add(3*time.Hour)
	actor, checker, org, review, purpose, pool, gateway, policy := sqlID(t), sqlID(t), sqlID(t), sqlID(t), sqlID(t), sqlID(t), sqlID(t), sqlID(t)
	for _, a := range []string{actor, checker} {
		sqlMust(t, owner, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'U3 disposable actor','DISABLED',false)`, a, "u3-"+a+"@internal.invalid")
	}
	// Reuse the canonical active source. Only the absent fresh fixture case seeds
	// the exact canonical BAILEYS bootstrap record; no authority row is replaced.
	providers := &provider.Service{Store: &provider.PostgreSQLStore{DB: db}}
	definition, e := providers.Require(ctx, "OPENWA", provider.ChannelWhatsApp, "BAILEYS", now, []provider.Capability{provider.CapabilitySendText})
	if errors.Is(e, provider.ErrNotFound) {
		_, e = (&provider.PostgreSQLStore{DB: owner}).Create(ctx, provider.Definition{ID: "00000000-0000-4000-8000-000000000102", Provider: "OPENWA", Channel: provider.ChannelWhatsApp, Engine: "BAILEYS", AdapterVersion: "0.13.0", Capabilities: []provider.Capability{provider.CapabilitySendText, provider.CapabilitySendImage, provider.CapabilitySendVideo, provider.CapabilitySendDocument, provider.CapabilityDeliveryEvents, provider.CapabilityReadEvents, provider.CapabilityInbound, provider.CapabilityPairingQR, provider.CapabilityPairingCode}, MaximumAttachmentBytes: 64 << 20, Status: provider.StatusActive, EffectiveFrom: now.Add(-time.Second), Version: 1, CreatedBy: actor, ApprovedBy: checker, Reason: "Disposable canonical Baileys readiness fixture", CreatedAt: now, UpdatedAt: now})
		if e != nil {
			t.Fatalf("canonical fixture provider seed failed: %v", e)
		}
		definition, e = providers.Require(ctx, "OPENWA", provider.ChannelWhatsApp, "BAILEYS", now, []provider.Capability{provider.CapabilitySendText})
	}
	if e != nil {
		t.Fatalf("canonical provider lookup failed: %v", e)
	}
	t.Logf("canonical provider binding id=%s version=%d engine=%s adapter=%s", definition.ID, definition.Version, definition.Engine, definition.AdapterVersion)
	sqlMust(t, owner, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, org, "U3 "+org)
	sqlMust(t, owner, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,status,review_scope,expires_at,reviewed_at,reviewed_by,version,outcome,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed) VALUES($1::uuid,$2::uuid,'Synthetic approved review','WHATSAPP','DIRECT','u3-w1','APPROVED','ORGANISATION',$3,$4,$5::uuid,1,'APPROVED',true,true,true)`, review, org, now.Add(24*time.Hour), now, checker)
	sqlMust(t, owner, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'U3 purpose','WHATSAPP','u3-w1',$4::uuid)`, purpose, org, "U3_"+purpose, review)
	sqlMust(t, owner, `INSERT INTO organisation_policy_versions(id,organisation_id,allowed_purpose_ids,contact_retention_days,campaign_retention_days,status,effective_from,version,created_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::jsonb,30,90,'ACTIVE',$4,1,$5::uuid,$6::uuid,'Disposable U3 purpose policy',$4,$4)`, policy, org, `["`+purpose+`"]`, now.Add(-time.Hour), actor, checker)
	sqlMust(t, owner, `INSERT INTO sender_pools(id,name,status,max_messages_per_minute,daily_capacity,reserved_capacity) VALUES($1::uuid,$2,'ACTIVE',30,3000,100)`, pool, "U3 "+pool)
	sqlMust(t, owner, `INSERT INTO gateway_pools(id,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,version,effective_from) VALUES($1::uuid,$2,'OPENWA','BAILEYS',$3,'ACTIVE','["SEND_TEXT","SEND_IMAGE"]'::jsonb,1,1,$4)`, gateway, "U3 "+gateway, definition.AdapterVersion, now.Add(-time.Hour))
	var node, session string
	for i := 0; i < 2; i++ {
		node, session = sqlID(t), sqlID(t)
		sqlMust(t, owner, `INSERT INTO sender_nodes(id,name,status,gateway_pool_id,provider,engine,adapter_version,last_heartbeat_at,draining,internal_url) VALUES($1::uuid,$2,'READY',$3::uuid,'OPENWA','BAILEYS',$4,$5,false,'http://127.0.0.1:1')`, node, "U3 "+node, gateway, definition.AdapterVersion, now)
		sqlMust(t, owner, `INSERT INTO sender_sessions(id,node_id,sender_pool_id,gateway_pool_id,encrypted_msisdn,masked_msisdn,engine_type,status,safe_messages_per_minute,safe_daily_capacity,sent_today,last_heartbeat_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,decode('00','hex'),'synthetic','BAILEYS','READY',10,1000,100,$5)`, session, node, pool, gateway, now)
	}
	c := campaign.Campaign{ID: sqlID(t), OrganisationID: org, PurposeID: purpose, ConsentReviewID: review, Name: "U3 readonly fixture", Status: campaign.StatusDraft, Version: 1, RequestedStartAt: &start, CompletionDeadlineAt: &end, Timezone: "UTC", MaximumUniqueRecipients: 10, MaximumMessagesPerRecipient: 1, CreatedBy: actor, CreatedAt: now, UpdatedAt: now, Transport: campaign.TransportSelection{Channel: "WHATSAPP", Provider: campaign.ProviderOpenWA, Engine: campaign.EngineBaileys, RoutingMode: campaign.RoutingSenderPool, GatewayPoolID: gateway, GatewayPoolVersion: 1, SenderPoolID: pool, AdapterVersion: definition.AdapterVersion, ProviderDefinitionID: definition.ID, ProviderDefinitionVersion: definition.Version, RequiredCapabilities: []string{"SEND_TEXT"}, FallbackMode: campaign.FallbackNone, RoutingPolicyVersion: "u3-r1", CapacityEvidenceVersion: "u3-c1"}}
	repo := &pg.CampaignRepository{DB: owner}
	if e = repo.Create(ctx, c); e != nil {
		t.Fatal(e)
	}
	draft := c
	draft.ID = sqlID(t)
	draft.Name = "U3 empty draft"
	if e = repo.Create(ctx, draft); e != nil {
		t.Fatal(e)
	}
	snapshot, msg, commercialID, plan, hold, recipient, test := sqlID(t), sqlID(t), sqlID(t), sqlID(t), sqlID(t), sqlID(t), sqlID(t)
	sqlMust(t, owner, `INSERT INTO audience_snapshots(id,campaign_id,segment_definition,definition_version,consent_policy_version,configuration_version,snapshot_hash,eligible_count,created_by) VALUES($1::uuid,$2::uuid,'{"combinator":"AND","rules":[]}'::jsonb,1,'u3-consent','u3-config',$3,5,$4::uuid)`, snapshot, c.ID, strings.Repeat("a", 64), actor)
	objects := &fixtureObjects{}
	if media {
		asset := sqlID(t)
		objects.sha = fmt.Sprintf("%x", sha256.Sum256([]byte(asset)))
		sqlMust(t, owner, `INSERT INTO trusted_assets(id,purpose,object_key,original_filename,media_type,byte_size,sha256_checksum,status,created_by,version) VALUES($1::uuid,'MESSAGE_MEDIA',$2,'synthetic.png','image/png',3,$3,'CLEAN',$4::uuid,1)`, asset, "private/synthetic/"+asset, objects.sha, actor)
		sqlMust(t, owner, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,created_by,approved_by,approved_at,client_request_id,media_asset_id,media_object_key,media_sha256,media_type,media_size,media_scan_status) VALUES($1::uuid,$2::uuid,1,'IMAGE_CAPTION','Synthetic image',$3,'APPROVED',$4::uuid,$5::uuid,$6,'u3-message-'||$1::text,$7::uuid,$8,$9,'image/png',3,'CLEAN')`, msg, c.ID, strings.Repeat("b", 64), actor, checker, now, asset, "private/synthetic/"+asset, objects.sha)
	} else {
		sqlMust(t, owner, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,created_by,approved_by,approved_at,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','Synthetic text',$3,'APPROVED',$4::uuid,$5::uuid,$6,'u3-message-'||$1::text)`, msg, c.ID, strings.Repeat("b", 64), actor, checker, now)
	}
	sqlMust(t, owner, `INSERT INTO campaign_commercial_approvals(id,campaign_id,organisation_id,quotation_reference,invoice_reference,currency,approved_recipients,unit_price_minor,management_fee_minor,total_amount_minor,status,version,created_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,'PRIVATE_QUOTE','PRIVATE_INVOICE','NGN',10,0,0,0,'APPROVED',1,$4::uuid,$5::uuid,'Synthetic commercial approval',$6,$6)`, commercialID, c.ID, org, actor, checker, now)
	sqlMust(t, owner, `UPDATE campaigns SET status='COMMERCIAL_APPROVED',audience_snapshot_id=$2::uuid,approved_message_version_id=$3::uuid,commercial_approval_id=$4::uuid,version=2 WHERE id=$1::uuid`, c.ID, snapshot, msg, commercialID)
	sqlMust(t, owner, `INSERT INTO campaign_routing_plans(id,campaign_id,plan_version,routing_policy_version,capacity_evidence_version,pacing_policy_version,fallback_mode,approved_at,approved_by,idempotency_key,request_hash,distribution_mode) VALUES($1::uuid,$2::uuid,1,'u3-r1','u3-c1','u3-p1','NONE',$3,$4::uuid,$5,$6,'WEIGHTED')`, plan, c.ID, now, checker, "u3-plan-"+plan, strings.Repeat("d", 64))
	sqlMust(t, owner, `INSERT INTO campaign_routing_plan_pools(routing_plan_id,sender_pool_id,gateway_pool_id,provider,engine,allocation_weight,maximum_recipients,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,provider_adapter_version,provider_capability_definition_id,provider_capability_definition_version,gateway_pool_version) VALUES($1::uuid,$2::uuid,$3::uuid,'OPENWA','BAILEYS',100,10,5,300,500,$4,$5::uuid,$6,1)`, plan, pool, gateway, definition.AdapterVersion, definition.ID, definition.Version)
	sqlMust(t, owner, `INSERT INTO campaign_pool_capacity_reservations(id,campaign_id,routing_plan_id,sender_pool_id,reservation_start,reservation_end,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,status,fencing_version) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,5,300,500,'HELD',1)`, hold, c.ID, plan, pool, start, end)
	sqlMust(t, owner, `INSERT INTO approved_test_recipients(id,label,msisdn_encrypted,msisdn_lookup_hash,masked_msisdn,status,created_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,'Synthetic recipient',decode('00','hex'),decode(replace($1::text,'-',''),'hex'),'synthetic','ACTIVE',$2::uuid,$3::uuid,'Synthetic accepted evidence only',$4,$4)`, recipient, actor, checker, now)
	sqlMust(t, owner, `INSERT INTO test_message_sends(id,campaign_id,message_version_id,message_content_hash,test_recipient_id,gateway_pool_id,sender_pool_id,provider,engine,sender_session_id,status,created_by,reason,idempotency_key,created_at,updated_at,completed_at,gateway_pool_version,provider_adapter_version,provider_capability_definition_id,provider_capability_definition_version,gateway_node_id,gateway_node_version,session_lease_version,session_configuration_version,authority_expires_at,route_reference) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5::uuid,$6::uuid,$7::uuid,'OPENWA','BAILEYS',$8::uuid,'ACCEPTED',$9::uuid,'Synthetic evidence; no send',$10,$11,$11,$11,1,$12,$13::uuid,$14,$15::uuid,1,1,1,$16,'u3-synthetic-route')`, test, c.ID, msg, strings.Repeat("b", 64), recipient, gateway, pool, session, actor, "u3-test-"+test, now, definition.AdapterVersion, definition.ID, definition.Version, node, end)
	campaigns := campaign.NewService(&pg.CampaignRepository{DB: db})
	orgs := organisation.NewService(&pg.OrganisationRepository{DB: db})
	pools := &sender.PostgreSQLGovernanceStore{DB: db}
	gateways := &sender.GatewayPoolService{Store: pools}
	comm := &commercial.Service{Store: &pg.CommercialRepository{DB: db}}
	svc := &prep.Service{Campaigns: campaigns, Organisations: orgs, Purposes: &consent.PurposeService{Repository: &pg.ConsentPurposeRepository{DB: db}}, OrganisationPolicies: &organisation.PolicyAdministration{Store: &pg.OrganisationPolicyRepository{DB: db}, Organisations: orgs, Clock: func() time.Time { return now }}, Reviews: consent.NewService(&pg.ConsentRepository{DB: db}), Providers: providers, GatewayPools: gateways, SenderPools: pools, Snapshots: segment.NewService(&segment.PostgreSQLStore{DB: db}), Messages: message.NewService(&message.PostgreSQLRepository{DB: db}), Assets: &storage.TrustedAssetService{Repository: &storage.PostgreSQLAssetRepository{DB: db}, Objects: objects}, CommercialRecords: comm.Store, CommercialApprovals: comm, Routes: &execution.RoutingAdministration{Store: &execution.PostgreSQLRoutingPlanStore{DB: db}, Campaigns: campaigns, ProviderCapabilities: providers, GatewayPools: gateways}, TestMessages: &testmessage.Service{Repository: &testmessage.PostgreSQLRepository{DB: db}}, Capacity: &prep.PostgreSQLCapacityReader{DB: db}, Maintenance: &platformpolicy.MaintenanceAdministration{Store: &platformpolicy.PostgreSQLStore{DB: db}}, SafetyMarginPercent: 15, Clock: func() time.Time { return now }}
	c, e = campaigns.Get(ctx, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	return &sqlPreparationFixture{svc: svc, campaign: c, draft: draft, now: now, pool: pool, gateway: gateway, review: review, plan: plan, objects: objects}
}

func readinessFootprint(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rows, e := db.QueryContext(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`)
	if e != nil {
		t.Fatal("table inventory unavailable")
	}
	var queries []string
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			t.Fatal(e)
		}
		quoted := strings.ReplaceAll(name, "\"", "\"\"")
		queries = append(queries, fmt.Sprintf("SELECT '%s',count(*)::text||':'||md5(coalesce(string_agg(to_jsonb(t)::text,E'\\n' ORDER BY to_jsonb(t)::text),'')) FROM public.\"%s\" t", strings.ReplaceAll(name, "'", "''"), quoted))
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	rows.Close()
	rows, e = db.QueryContext(ctx, strings.Join(queries, " UNION ALL "))
	if e != nil {
		t.Fatal("read-only footprint query failed")
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, hash string
		if e = rows.Scan(&name, &hash); e != nil {
			t.Fatal(e)
		}
		out[name] = hash
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	return out
}
func sqlReadinessHandler(t *testing.T, svc *prep.Service) (http.Handler, string) {
	t.Helper()
	hash, e := identity.HashPassword("u3-disposable-session-password")
	if e != nil {
		t.Fatal(e)
	}
	user := identity.User{ID: "u3-readonly-operator", Email: "u3@internal.invalid", Status: identity.StatusActive, PasswordHash: hash, Permissions: map[string]struct{}{"campaign.read": {}}}
	auth := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, time.Hour)
	login, e := auth.Login(context.Background(), user.Email, "u3-disposable-session-password")
	if e != nil {
		t.Fatal(e)
	}
	return httpserver.New(slog.New(slog.NewTextHandler(io.Discard, nil)), httpserver.Dependencies{CampaignPreparation: svc, Identity: auth}).Handler(), login.SessionToken
}
func sqlReadinessGET(t *testing.T, h http.Handler, token, id string, want int) prep.Readiness {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/v1/campaigns/"+id+"/readiness", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("readiness GET status=%d want=%d body=%s", w.Code, want, w.Body)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("readiness cache not disabled")
	}
	for _, private := range []string{"Synthetic text", "PRIVATE_QUOTE", "PRIVATE_INVOICE", "private/synthetic", "private object outage", "msisdn", "variableValues"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("private source detail escaped response")
		}
	}
	var out prep.Readiness
	if want == 200 {
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		if len(out.Checks) != 12 {
			t.Fatal("missing required checks")
		}
		for _, c := range out.Checks {
			if c.Evidence == nil {
				t.Fatal("missing evidence array")
			}
		}
	}
	return out
}
func checkSQLReadiness(t *testing.T, r prep.Readiness, key, status, code string) {
	t.Helper()
	for _, c := range r.Checks {
		if c.Key == key {
			if c.Status != status || c.Code != code {
				t.Fatalf("%s=%s/%s want %s/%s", key, c.Status, c.Code, status, code)
			}
			return
		}
	}
	t.Fatalf("missing %s", key)
}
func TestPostgreSQLCampaignReadinessReadOnly(t *testing.T) {
	readinessSQLRoles(t, func(t *testing.T, owner, db *sql.DB) {
		f := newSQLPreparationFixture(t, owner, db, false)
		h, token := sqlReadinessHandler(t, f.svc)
		assertReadOnly := func(name string, run func()) {
			t.Helper()
			before := readinessFootprint(t, db)
			run()
			after := readinessFootprint(t, db)
			if !reflect.DeepEqual(before, after) {
				for table, b := range before {
					if after[table] != b {
						t.Errorf("%s changed table %s", name, table)
					}
				}
				t.Fatal("GET changed database footprint")
			}
			t.Logf("%s: %d public table counts and row hashes unchanged", name, len(before))
		}
		assertReadOnly("positive and draft repeated GET", func() {
			for i := 0; i < 3; i++ {
				r := sqlReadinessGET(t, h, token, f.campaign.ID, 200)
				if r.State != "READY_FOR_FINAL_REVIEW" || !r.ReadyForFinalReview {
					t.Fatalf("positive fixture blocked: %+v", r.Checks)
				}
				if r.Capacity == nil || r.Capacity.AvailableMessagesPerMinute != 20 || r.Capacity.AvailableDailyUnits != 1700 || r.Capacity.HealthySessions != 2 {
					t.Fatalf("gross measured values: %+v", r.Capacity)
				}
			}
			draft := sqlReadinessGET(t, h, token, f.draft.ID, 200)
			checkSQLReadiness(t, draft, "audience", "BLOCKED", "AUDIENCE_NOT_FROZEN")
		})
		// Dynamic reservations have their own evidence check. They do not change the
		// explicitly gross current aggregate, even when windows compete.
		competingPlan := sqlID(t)
		sqlMust(t, owner, `INSERT INTO campaign_routing_plans(id,campaign_id,plan_version,routing_policy_version,capacity_evidence_version,pacing_policy_version,fallback_mode,approved_at,approved_by,idempotency_key,request_hash,distribution_mode) SELECT $1::uuid,$2::uuid,1,routing_policy_version,capacity_evidence_version,pacing_policy_version,fallback_mode,approved_at,approved_by,'u3-plan-'||$1::text,request_hash,distribution_mode FROM campaign_routing_plans WHERE id=$3::uuid`, competingPlan, f.draft.ID, f.plan)
		for _, status := range []string{"HELD", "ACTIVE"} {
			sqlMust(t, owner, `INSERT INTO campaign_pool_capacity_reservations(id,campaign_id,routing_plan_id,sender_pool_id,reservation_start,reservation_end,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,status,fencing_version) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,2,120,200,$7,1)`, sqlID(t), f.draft.ID, competingPlan, f.pool, *f.campaign.RequestedStartAt, *f.campaign.CompletionDeadlineAt, status)
		}
		assertReadOnly("gross capacity with competing HELD/ACTIVE rows", func() {
			r := sqlReadinessGET(t, h, token, f.campaign.ID, 200)
			if r.Capacity == nil || r.Capacity.AvailableMessagesPerMinute != 20 || r.Capacity.AvailableDailyUnits != 1700 {
				t.Fatal("dynamic reservation incorrectly changed gross measurement")
			}
			checkSQLReadiness(t, r, "pilot", "PASS", "PILOT_EVIDENCE_VALID")
		})
		// Large, valid persisted counts must not wrap time.Duration into the past.
		sqlMust(t, owner, `UPDATE campaigns SET maximum_unique_recipients=130664438,version=version+1 WHERE id=$1::uuid`, f.draft.ID)
		sqlMust(t, owner, `UPDATE sender_pools SET max_messages_per_minute=1 WHERE id=$1::uuid`, f.pool)
		assertReadOnly("huge persisted count overflow", func() {
			r := sqlReadinessGET(t, h, token, f.draft.ID, 200)
			checkSQLReadiness(t, r, "capacity", "BLOCKED", "CAPACITY_PROJECTION_UNREPRESENTABLE")
			if r.Capacity == nil || r.Capacity.ProjectedCompletionAt != nil {
				t.Fatal("large count projected an invalid completion")
			}
		})
		sqlMust(t, owner, `UPDATE sender_pools SET max_messages_per_minute=30 WHERE id=$1::uuid`, f.pool)
		// Synthetic setup changes are outside each before/after GET boundary.
		sqlMust(t, owner, `UPDATE consent_reviews SET expires_at=$2 WHERE id=$1::uuid`, f.review, *f.campaign.CompletionDeadlineAt)
		assertReadOnly("expiry equality", func() {
			checkSQLReadiness(t, sqlReadinessGET(t, h, token, f.campaign.ID, 200), "consent", "BLOCKED", "CONSENT_EXPIRES_BEFORE_DEADLINE")
		})
		sqlMust(t, owner, `UPDATE gateway_pools SET status='RETIRED' WHERE id=$1::uuid`, f.gateway)
		assertReadOnly("retired selected gateway", func() {
			checkSQLReadiness(t, sqlReadinessGET(t, h, token, f.campaign.ID, 200), "transport", "BLOCKED", "TRANSPORT_SOURCE_RETIRED")
		})
		sqlMust(t, owner, `UPDATE campaigns SET status='SCHEDULED',version=version+1 WHERE id=$1::uuid`, f.campaign.ID)
		assertReadOnly("unsupported stage", func() { sqlReadinessGET(t, h, token, f.campaign.ID, 409) })
		if f.objects.writes != 0 {
			t.Fatal("readiness invoked object mutation")
		}
	})
}
func TestPostgreSQLCampaignReadinessMediaReadOnly(t *testing.T) {
	readinessSQLRoles(t, func(t *testing.T, owner, db *sql.DB) {
		f := newSQLPreparationFixture(t, owner, db, true)
		h, token := sqlReadinessHandler(t, f.svc)
		before := readinessFootprint(t, db)
		checkSQLReadiness(t, sqlReadinessGET(t, h, token, f.campaign.ID, 200), "message", "PASS", "MESSAGE_TRUSTED")
		f.objects.outage = true
		checkSQLReadiness(t, sqlReadinessGET(t, h, token, f.campaign.ID, 200), "message", "UNAVAILABLE", "MEDIA_UNAVAILABLE")
		if !reflect.DeepEqual(before, readinessFootprint(t, db)) {
			t.Fatal("media GET changed database footprint")
		}
		t.Logf("trusted media and metadata outage GET: %d public table counts and row hashes unchanged", len(before))
		if f.objects.writes != 0 {
			t.Fatal("object mutation during readiness")
		}
	})
}
