package execution

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLMixedRoutingPersistsMetaEndpointAndUsesMetaCapacity(t *testing.T) {
	dsn := os.Getenv("POSTGRES_META_CLOUD_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_META_CLOUD_DATABASE_URL is not set")
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
	ids := make([]string, 10)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, purposeID, campaignID := ids[0], ids[1], ids[2], ids[3]
	openPoolID, metaPoolID, gatewayID := ids[4], ids[5], ids[6]
	metaSenderID, openDefID, metaDefID := ids[7], ids[8], ids[9]
	alienOrgID, alienPoolID := newDatabaseUUID(t, db, ctx), newDatabaseUUID(t, db, ctx)
	alienMetaSenderID := newDatabaseUUID(t, db, ctx)
	now := time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)
	start, end := now.Add(time.Hour), now.Add(3*time.Hour)

	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Meta routing actor','ACTIVE',false)`, actorID, "meta-route-"+actorID[:8]+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Meta routing "+orgID[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, alienOrgID, "Alien routing "+alienOrgID[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Meta routing','WHATSAPP','v1')`, purposeID, orgID, "MR_"+purposeID[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,requested_start_at,completion_deadline_at) VALUES($1::uuid,$2::uuid,'Meta routing',$3::uuid,'DRAFT',10,$4,$5)`, campaignID, orgID, purposeID, start, end); err != nil {
		t.Fatal(err)
	}
	for _, pool := range []struct {
		id, name string
		mpm      int
		daily    int64
	}{{openPoolID, "openwa-" + orgID[:8], 30, 3000}, {metaPoolID, "meta-" + orgID[:8], 70, 7000}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',$4,$5)`, pool.id, pool.name, orgID, pool.mpm, pool.daily); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',10,1000)`, alienPoolID, "alien-"+alienOrgID[:8], alienOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meta_cloud_senders(id,organisation_id,sender_pool_id,waba_id,phone_number_id,display_name,business_phone_display,credential_key,graph_api_version,status,health_status,health_observed_at,effective_from,version,created_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,'Alien Meta Sender','+234 ***','alien-meta-route-key','v23.0','ACTIVE','HEALTHY',$6,$7,2,$8::uuid,$8::uuid,'approved alien Meta sender',$7,$6)`, alienMetaSenderID, alienOrgID, alienPoolID, "waba-alien-"+alienOrgID[:8], "phone-alien-"+alienOrgID[:8], now, now.Add(-time.Hour), actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO gateway_pools(id,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,version) VALUES($1::uuid,$2,'OPENWA','BAILEYS','0.13.0','ACTIVE','["SEND_TEXT"]'::jsonb,1,1)`, gatewayID, "meta-route-gw-"+gatewayID[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meta_cloud_senders(id,organisation_id,sender_pool_id,waba_id,phone_number_id,display_name,business_phone_display,credential_key,graph_api_version,status,health_status,health_observed_at,effective_from,version,created_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,'Meta Sender','+234 ***','meta-route-key','v23.0','ACTIVE','HEALTHY',$6,$7,4,$8::uuid,$8::uuid,'approved Meta sender',$7,$6)`, metaSenderID, orgID, metaPoolID, "waba-"+orgID[:8], "phone-"+orgID[:8], now, now.Add(-time.Hour), actorID); err != nil {
		t.Fatal(err)
	}
	for _, def := range []struct {
		id, provider, engine, adapter, caps string
	}{{openDefID, "OPENWA", "BAILEYS", "0.13.0", "{SEND_TEXT}"}, {metaDefID, "META", "CLOUD_API", "1.0.0", "{SEND_TEXT,SEND_TEMPLATE}"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO provider_capability_definitions(id,provider,channel,engine,adapter_version,capabilities,status,effective_from,effective_to,version,created_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,$2,'WHATSAPP',$3,$4,$5::text[],'ACTIVE',$6,$7,1,$8::uuid,$8::uuid,'approved provider route',$6,$6)`, def.id, def.provider, def.engine, def.adapter, def.caps, now.Add(-time.Hour), end.Add(time.Hour), actorID); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_recipients WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_dispatch_shards WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM audience_snapshots WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM message_versions WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM contacts WHERE masked_msisdn='***R11'`)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_pool_capacity_reservations WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_routing_plan_pools WHERE routing_plan_id IN (SELECT id FROM campaign_routing_plans WHERE campaign_id=$1::uuid)`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_routing_plans WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM provider_capability_definitions WHERE id IN ($1::uuid,$2::uuid)`, openDefID, metaDefID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM meta_cloud_senders WHERE id IN ($1::uuid,$2::uuid)`, metaSenderID, alienMetaSenderID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pools WHERE id=$1::uuid`, gatewayID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_pools WHERE id IN ($1::uuid,$2::uuid,$3::uuid)`, openPoolID, metaPoolID, alienPoolID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id IN ($1::uuid,$2::uuid)`, orgID, alienOrgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	unboundPlanID := newDatabaseUUID(t, db, ctx)
	unboundPlan := RoutingPlan{ID: unboundPlanID, CampaignID: campaignID, DistributionMode: DistributionWeighted, RoutingPolicyVersion: "rp-unbound", CapacityEvidenceVersion: "cap-unbound", PacingPolicyVersion: "pace-unbound", FallbackMode: "NONE", ApprovedAt: now, ApprovedBy: actorID, IdempotencyKey: "unbound-route-" + campaignID, RequestHash: "hash-unbound-route-" + campaignID}
	unboundPlan.Routes = []PoolRoute{{SenderPoolID: openPoolID, GatewayPoolID: gatewayID, Provider: "OPENWA", Engine: "BAILEYS", AllocationWeight: 100, MaximumRecipients: 5, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 500}}
	unboundReservations := []CapacityReservation{{ID: newDatabaseUUID(t, db, ctx), CampaignID: campaignID, RoutingPlanID: unboundPlanID, SenderPoolID: openPoolID, ReservationStart: start, ReservationEnd: end, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 500, Status: "HELD", FencingVersion: 1, CreatedAt: now, UpdatedAt: now}}
	unboundStored, err := (&PostgreSQLRoutingPlanStore{DB: db}).Create(ctx, unboundPlan, unboundReservations)
	if err != nil {
		t.Fatalf("unbound route should round-trip zero frozen capability evidence as SQL NULL: %v", err)
	}
	unboundLoaded, err := (&PostgreSQLRoutingPlanStore{DB: db}).Get(ctx, unboundStored.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(unboundLoaded.Routes) != 1 || unboundLoaded.Routes[0].ProviderDefinitionID != "" || unboundLoaded.Routes[0].ProviderDefinitionVersion != 0 || unboundLoaded.Routes[0].GatewayPoolVersion != 0 || unboundLoaded.Routes[0].ProviderAdapterVersion != "" {
		t.Fatalf("unbound route evidence did not round-trip as zero values: %#v", unboundLoaded.Routes)
	}
	if err := (&PostgreSQLRoutingPlanStore{DB: db}).ReleaseReservations(ctx, unboundStored.ID, actorID, now); err != nil {
		t.Fatalf("release temporary unbound reservation: %v", err)
	}

	zeroVersionPlanID := newDatabaseUUID(t, db, ctx)
	zeroVersionPlan := RoutingPlan{ID: zeroVersionPlanID, CampaignID: campaignID, DistributionMode: DistributionWeighted, RoutingPolicyVersion: "rp-zero-meta-version", CapacityEvidenceVersion: "cap-zero-meta-version", PacingPolicyVersion: "pace-zero-meta-version", FallbackMode: "NONE", ApprovedAt: now, ApprovedBy: actorID, IdempotencyKey: "zero-meta-version-" + campaignID, RequestHash: "hash-zero-meta-version-" + campaignID}
	zeroVersionPlan.Routes = []PoolRoute{{SenderPoolID: metaPoolID, MetaSenderID: metaSenderID, Provider: "META", Engine: "CLOUD_API", ProviderAdapterVersion: "1.0.0", ProviderDefinitionID: metaDefID, ProviderDefinitionVersion: 1, MetaSenderVersion: 0, AllocationWeight: 100, MaximumRecipients: 5, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 500}}
	zeroVersionReservations := []CapacityReservation{{ID: newDatabaseUUID(t, db, ctx), CampaignID: campaignID, RoutingPlanID: zeroVersionPlanID, SenderPoolID: metaPoolID, ReservationStart: start, ReservationEnd: end, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 500, Status: "HELD", FencingVersion: 1, CreatedAt: now, UpdatedAt: now}}
	if _, err := (&PostgreSQLRoutingPlanStore{DB: db}).Create(ctx, zeroVersionPlan, zeroVersionReservations); err == nil {
		t.Fatal("Meta route with zero/NULL frozen sender version was accepted")
	}

	badPlanID := newDatabaseUUID(t, db, ctx)
	badPlan := RoutingPlan{ID: badPlanID, CampaignID: campaignID, DistributionMode: DistributionWeighted, RoutingPolicyVersion: "rp-alien", CapacityEvidenceVersion: "cap-alien", PacingPolicyVersion: "pace-alien", FallbackMode: "NONE", ApprovedAt: now, ApprovedBy: actorID, IdempotencyKey: "alien-route-" + campaignID, RequestHash: "hash-alien-" + campaignID}
	badPlan.Routes = []PoolRoute{{SenderPoolID: alienPoolID, GatewayPoolID: gatewayID, Provider: "OPENWA", Engine: "BAILEYS", ProviderAdapterVersion: "0.13.0", ProviderDefinitionID: openDefID, ProviderDefinitionVersion: 1, GatewayPoolVersion: 1, AllocationWeight: 100, MaximumRecipients: 5, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 500}}
	badReservations := []CapacityReservation{{ID: newDatabaseUUID(t, db, ctx), CampaignID: campaignID, RoutingPlanID: badPlanID, SenderPoolID: alienPoolID, ReservationStart: start, ReservationEnd: end, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 500, Status: "HELD", FencingVersion: 1, CreatedAt: now, UpdatedAt: now}}
	if _, err := (&PostgreSQLRoutingPlanStore{DB: db}).Create(ctx, badPlan, badReservations); !errors.Is(err, ErrRoutingPlanInvalid) {
		t.Fatalf("tenant-owned sender pool from another organisation was accepted: %v", err)
	}
	badMetaPlanID := newDatabaseUUID(t, db, ctx)
	badMetaPlan := RoutingPlan{ID: badMetaPlanID, CampaignID: campaignID, DistributionMode: DistributionWeighted, RoutingPolicyVersion: "rp-alien-meta", CapacityEvidenceVersion: "cap-alien-meta", PacingPolicyVersion: "pace-alien-meta", FallbackMode: "NONE", ApprovedAt: now, ApprovedBy: actorID, IdempotencyKey: "alien-meta-route-" + campaignID, RequestHash: "hash-alien-meta-" + campaignID}
	badMetaPlan.Routes = []PoolRoute{{SenderPoolID: metaPoolID, MetaSenderID: alienMetaSenderID, Provider: "META", Engine: "CLOUD_API", ProviderAdapterVersion: "1.0.0", ProviderDefinitionID: metaDefID, ProviderDefinitionVersion: 1, MetaSenderVersion: 2, AllocationWeight: 100, MaximumRecipients: 5, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 500}}
	badMetaReservations := []CapacityReservation{{ID: newDatabaseUUID(t, db, ctx), CampaignID: campaignID, RoutingPlanID: badMetaPlanID, SenderPoolID: metaPoolID, ReservationStart: start, ReservationEnd: end, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 500, Status: "HELD", FencingVersion: 1, CreatedAt: now, UpdatedAt: now}}
	if _, err := (&PostgreSQLRoutingPlanStore{DB: db}).Create(ctx, badMetaPlan, badMetaReservations); err == nil {
		t.Fatal("Meta sender owned by another organisation/pool was accepted for campaign route")
	}
	badOpenPlanID := newDatabaseUUID(t, db, ctx)
	badOpenPlan := RoutingPlan{ID: badOpenPlanID, CampaignID: campaignID, DistributionMode: DistributionWeighted, RoutingPolicyVersion: "rp-zero-gateway-version", CapacityEvidenceVersion: "cap-zero-gateway-version", PacingPolicyVersion: "pace-zero-gateway-version", FallbackMode: "NONE", ApprovedAt: now, ApprovedBy: actorID, IdempotencyKey: "zero-gateway-version-" + campaignID, RequestHash: "hash-zero-gateway-version-" + campaignID}
	badOpenPlan.Routes = []PoolRoute{{SenderPoolID: openPoolID, GatewayPoolID: gatewayID, Provider: "OPENWA", Engine: "BAILEYS", ProviderAdapterVersion: "0.13.0", ProviderDefinitionID: openDefID, ProviderDefinitionVersion: 1, GatewayPoolVersion: 0, AllocationWeight: 100, MaximumRecipients: 5, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 500}}
	badOpenReservations := []CapacityReservation{{ID: newDatabaseUUID(t, db, ctx), CampaignID: campaignID, RoutingPlanID: badOpenPlanID, SenderPoolID: openPoolID, ReservationStart: start, ReservationEnd: end, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 500, Status: "HELD", FencingVersion: 1, CreatedAt: now, UpdatedAt: now}}
	if _, err := (&PostgreSQLRoutingPlanStore{DB: db}).Create(ctx, badOpenPlan, badOpenReservations); err == nil {
		t.Fatal("OpenWA route with zero/NULL frozen gateway version was accepted")
	}

	planID := ids[0]
	planID = newDatabaseUUID(t, db, ctx)
	plan := RoutingPlan{ID: planID, CampaignID: campaignID, DistributionMode: DistributionWeighted, RoutingPolicyVersion: "rp-meta", CapacityEvidenceVersion: "cap-meta", PacingPolicyVersion: "pace-meta", FallbackMode: "NONE", ApprovedAt: now, ApprovedBy: actorID, IdempotencyKey: "meta-route-" + campaignID, RequestHash: "hash-meta-" + campaignID}
	plan.Routes = []PoolRoute{
		{SenderPoolID: openPoolID, GatewayPoolID: gatewayID, Provider: "OPENWA", Engine: "BAILEYS", ProviderAdapterVersion: "0.13.0", ProviderDefinitionID: openDefID, ProviderDefinitionVersion: 1, GatewayPoolVersion: 1, AllocationWeight: 30, MaximumRecipients: 5, ReservedMessagesPerMinute: 30, ReservedHourlyUnits: 300, ReservedDailyUnits: 1500},
		{SenderPoolID: metaPoolID, MetaSenderID: metaSenderID, Provider: "META", Engine: "CLOUD_API", ProviderAdapterVersion: "1.0.0", ProviderDefinitionID: metaDefID, ProviderDefinitionVersion: 1, MetaSenderVersion: 4, AllocationWeight: 70, MaximumRecipients: 5, ReservedMessagesPerMinute: 70, ReservedHourlyUnits: 700, ReservedDailyUnits: 3500},
	}
	reservations := []CapacityReservation{
		{ID: newDatabaseUUID(t, db, ctx), CampaignID: campaignID, RoutingPlanID: planID, SenderPoolID: openPoolID, ReservationStart: start, ReservationEnd: end, ReservedMessagesPerMinute: 30, ReservedHourlyUnits: 300, ReservedDailyUnits: 1500, Status: "HELD", FencingVersion: 1, CreatedAt: now, UpdatedAt: now},
		{ID: newDatabaseUUID(t, db, ctx), CampaignID: campaignID, RoutingPlanID: planID, SenderPoolID: metaPoolID, ReservationStart: start, ReservationEnd: end, ReservedMessagesPerMinute: 70, ReservedHourlyUnits: 700, ReservedDailyUnits: 3500, Status: "HELD", FencingVersion: 1, CreatedAt: now, UpdatedAt: now},
	}
	stored, err := (&PostgreSQLRoutingPlanStore{DB: db}).Create(ctx, plan, reservations)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := (&PostgreSQLRoutingPlanStore{DB: db}).Get(ctx, stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	var loadedMeta PoolRoute
	for _, route := range loaded.Routes {
		if route.Provider == "META" {
			loadedMeta = route
		}
	}
	if loaded.DistributionMode != DistributionWeighted || len(loaded.Routes) != 2 || loadedMeta.MetaSenderID != metaSenderID || loadedMeta.MetaSenderVersion != 4 {
		t.Fatalf("Meta routing evidence was not persisted: %#v", loaded)
	}
	capacity, err := (&PostgreSQLStore{DB: db}).RouteCapacity(ctx, plan.Routes[1], now)
	if err != nil {
		t.Fatal(err)
	}
	if capacity.AvailableMessagesPerMinute != 70 || capacity.AvailableDailyUnits != 7000 || capacity.HealthySessions != 1 || capacity.HealthyNodes != 1 {
		t.Fatalf("unexpected Meta capacity: %#v", capacity)
	}
	contactID := newDatabaseUUID(t, db, ctx)
	messageID := newDatabaseUUID(t, db, ctx)
	snapshotID := newDatabaseUUID(t, db, ctx)
	shardID := newDatabaseUUID(t, db, ctx)
	recipientID := newDatabaseUUID(t, db, ctx)
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***R11','ACTIVE',now())`, contactID, "terminal-report-"+contactID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','r11 terminal report',repeat('a',64),'APPROVED',$3)`, messageID, campaignID, "terminal-report-"+messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,1)`, snapshotID, campaignID, "terminal-report-"+snapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_dispatch_shards(id,campaign_id,ordinal,target_size,recipient_count,terminal_count,status,routing_plan_id,assigned_sender_pool_id) VALUES($1::uuid,$2::uuid,99,1,1,0,'PENDING',$3::uuid,$4::uuid)`, shardID, campaignID, stored.ID, openPoolID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,dispatch_shard_id,provider_message_id) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'GATEWAY_ACCEPTED',$7::uuid,'wamid.r11.accepted')`, recipientID, campaignID, snapshotID, contactID, messageID, "terminal-report-"+recipientID, shardID); err != nil {
		t.Fatal(err)
	}
	report, err := (&PostgreSQLRoutingPlanStore{DB: db}).PoolExecutionReport(ctx, stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(report) != 2 {
		t.Fatalf("unexpected route report: %#v", report)
	}
	for _, item := range report {
		if item.SenderPoolID == openPoolID && (item.SubmittedCount != 1 || item.TerminalCount != 0) {
			t.Fatalf("GATEWAY_ACCEPTED report counts submitted=%d terminal=%d", item.SubmittedCount, item.TerminalCount)
		}
	}
}

func newDatabaseUUID(t *testing.T, db *sql.DB, ctx context.Context) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
