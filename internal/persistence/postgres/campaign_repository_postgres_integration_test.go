package postgres

import (
	"campaign-platform/internal/campaign"
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLCampaignRepositoryPersistsTransportAndMaterialEvidence(t *testing.T) {
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
	ids := make([]string, 6)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, reviewID, purposeID, poolID, campaignID := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]
	definitionID := ""
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_material_change_events WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		if definitionID != "" {
			_, _ = db.ExecContext(cleanup, `DELETE FROM provider_capability_definitions WHERE id=$1::uuid`, definitionID)
		}
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_pools WHERE id=$1::uuid`, poolID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_reviews WHERE id=$1::uuid`, reviewID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 campaign','DISABLED',false)`, actorID, "campaign-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Task 6 campaign "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,status) VALUES($1::uuid,$2::uuid,'Task 6 campaign','WHATSAPP','DIRECT','v1','DRAFT')`, reviewID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Task 6 campaign','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "CAMPAIGN_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',60,1000)`, poolID, "Task 6 pool "+poolID, orgID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2099, 3, 3, 12, 0, 0, 0, time.UTC)
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&definitionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_capability_definitions(id,provider,channel,engine,adapter_version,capabilities,status,effective_from,version,created_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,'OPENWA','WHATSAPP','BAILEYS','task6-adapter','{SEND_TEXT}'::text[],'DRAFT',$2,1,$3::uuid,$3::uuid,'campaign repository definition fixture',$2,$2)`, definitionID, now.Add(-time.Hour), actorID); err != nil {
		t.Fatal(err)
	}
	value := campaign.Campaign{
		ID: campaignID, OrganisationID: orgID, Name: "Task 6 campaign", PurposeID: purposeID, ConsentReviewID: reviewID,
		Status: campaign.StatusDraft, Timezone: "UTC", MaximumUniqueRecipients: 10, MaximumMessagesPerRecipient: 1,
		Transport: campaign.TransportSelection{
			Channel: "WHATSAPP", Provider: campaign.ProviderOpenWA, Engine: campaign.EngineBaileys,
			RoutingMode: campaign.RoutingSenderPool, GatewayPoolID: "task6-baileys-gateway", GatewayPoolVersion: 1, SenderPoolID: poolID, AdapterVersion: "task6-adapter",
			ProviderDefinitionID: definitionID, ProviderDefinitionVersion: 1,
			RequiredCapabilities: []string{"SEND_TEXT"}, FallbackMode: campaign.FallbackNone,
			RoutingPolicyVersion: "task6-routing-v1", CapacityEvidenceVersion: "task6-capacity-v1",
		},
		CreatedBy: actorID, CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	repo := &CampaignRepository{DB: db}
	if err := repo.Create(ctx, value); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET transport_engine=NULL WHERE id=$1::uuid`, campaignID); err == nil {
		t.Fatal("database accepted OpenWA transport_provider with NULL transport_engine")
	}
	loaded, err := repo.Get(ctx, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Transport.RequiredCapabilities) != 1 || loaded.Transport.RequiredCapabilities[0] != "SEND_TEXT" {
		t.Fatalf("unexpected persisted transport evidence: %+v", loaded.Transport)
	}

	invalidFrozen := loaded
	invalidFrozen.Transport.ProviderDefinitionID = definitionID
	invalidFrozen.Transport.ProviderDefinitionVersion = 0
	invalidFrozen.Transport.GatewayPoolVersion = 0
	invalidFrozen.Version = 2
	invalidFrozen.UpdatedAt = now.Add(30 * time.Second)
	if err := repo.CompareAndSwap(ctx, invalidFrozen, 1); err == nil {
		t.Fatal("campaign repository accepted partial frozen provider/gateway version evidence")
	}

	loaded.Status = campaign.Status("CONSENT_REVIEW_PENDING")
	loaded.Version = 2
	loaded.UpdatedAt = now.Add(time.Minute)
	if err := repo.CompareAndSwap(ctx, loaded, 1); err != nil {
		t.Fatal(err)
	}
	var eventID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	amended := loaded
	amended.MaximumUniqueRecipients = 20
	amended.Version = 3
	amended.UpdatedAt = now.Add(2 * time.Minute)
	event := campaign.MaterialChangeEvent{
		ID: eventID, CampaignID: campaignID, ActorID: actorID, Reason: "increase approved audience ceiling",
		ChangedFields: []string{"maximumUniqueRecipients"}, PreviousStatus: loaded.Status, NewStatus: amended.Status,
		PreviousVersion: 2, NewVersion: 3, CreatedAt: amended.UpdatedAt,
	}
	if err := repo.AmendMaterial(ctx, amended, event, 2); err != nil {
		t.Fatal(err)
	}
	events, err := repo.ListMaterialChangePage(ctx, campaignID, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || len(events[0].ChangedFields) != 1 || events[0].ChangedFields[0] != "maximumUniqueRecipients" {
		t.Fatalf("unexpected material-change evidence: %+v", events)
	}
}

func TestPostgreSQLCampaignRepositoryPersistsMetaSenderEvidence(t *testing.T) {
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
	ids := make([]string, 8)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, reviewID, purposeID, poolID, senderID, campaignID, eventID := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6], ids[7]
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_material_change_events WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM meta_cloud_senders WHERE id=$1::uuid`, senderID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_pools WHERE id=$1::uuid`, poolID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_reviews WHERE id=$1::uuid`, reviewID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Meta campaign repository','DISABLED',false)`, actorID, "meta-campaign-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Meta campaign repository "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,status) VALUES($1::uuid,$2::uuid,'Meta campaign repository','WHATSAPP','DIRECT','v1','DRAFT')`, reviewID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Meta campaign repository','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "META_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',60,1000)`, poolID, "Meta campaign pool "+poolID, orgID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2099, 3, 4, 12, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO meta_cloud_senders(id,organisation_id,sender_pool_id,waba_id,phone_number_id,display_name,business_phone_display,credential_key,graph_api_version,status,health_status,health_observed_at,effective_from,version,created_by,reason,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,'Meta repository sender','+2348012345678',$6,'v23.0','ACTIVE','HEALTHY',$7,$8,1,$9::uuid,'repository Meta sender',$7,$7)`, senderID, orgID, poolID, "waba-"+senderID, "phone-"+senderID, "cred-"+senderID, now, now.Add(-time.Hour), actorID); err != nil {
		t.Fatal(err)
	}
	value := campaign.Campaign{
		ID: campaignID, OrganisationID: orgID, Name: "Meta campaign repository", PurposeID: purposeID, ConsentReviewID: reviewID,
		Status: campaign.StatusDraft, Timezone: "UTC", MaximumUniqueRecipients: 10, MaximumMessagesPerRecipient: 1,
		Transport: campaign.TransportSelection{
			Channel: "WHATSAPP", Provider: campaign.ProviderMeta, Engine: campaign.EngineMetaCloud,
			RoutingMode: campaign.RoutingSenderPool, SenderPoolID: poolID, MetaSenderID: senderID,
			RequiredCapabilities: []string{"SEND_TEMPLATE"}, FallbackMode: campaign.FallbackNone,
			RoutingPolicyVersion: "meta-routing-v1", CapacityEvidenceVersion: "meta-capacity-v1",
		},
		CreatedBy: actorID, CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	repo := &CampaignRepository{DB: db}
	if err := repo.Create(ctx, value); err != nil {
		t.Fatalf("create Meta campaign: %v", err)
	}
	loaded, err := repo.Get(ctx, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Transport.MetaSenderID != senderID {
		t.Fatalf("Meta sender evidence lost on read: got %q want %q", loaded.Transport.MetaSenderID, senderID)
	}
	loaded.Version = 2
	loaded.UpdatedAt = now.Add(time.Minute)
	if err := repo.CompareAndSwap(ctx, loaded, 1); err != nil {
		t.Fatalf("CAS Meta campaign: %v", err)
	}
	amended := loaded
	amended.MaximumUniqueRecipients = 20
	amended.Version = 3
	amended.UpdatedAt = now.Add(2 * time.Minute)
	event := campaign.MaterialChangeEvent{ID: eventID, CampaignID: campaignID, ActorID: actorID, Reason: "increase Meta audience ceiling", ChangedFields: []string{"maximumUniqueRecipients"}, PreviousStatus: loaded.Status, NewStatus: amended.Status, PreviousVersion: 2, NewVersion: 3, CreatedAt: amended.UpdatedAt}
	if err := repo.AmendMaterial(ctx, amended, event, 2); err != nil {
		t.Fatalf("amend Meta campaign: %v", err)
	}
	loaded, err = repo.Get(ctx, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Transport.MetaSenderID != senderID || loaded.Version != 3 {
		t.Fatalf("Meta sender evidence changed after CAS/amend: %+v", loaded.Transport)
	}
}

func TestPostgreSQLCampaignRepositoryRejectsCrossTenantMetaSenderEvidence(t *testing.T) {
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
	ids := make([]string, 9)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgA, orgB, reviewID, purposeID, poolA, poolB, senderB, campaignID := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6], ids[7], ids[8]
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM meta_cloud_senders WHERE id=$1::uuid`, senderB)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_pools WHERE id IN ($1::uuid,$2::uuid)`, poolA, poolB)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_reviews WHERE id=$1::uuid`, reviewID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id IN ($1::uuid,$2::uuid)`, orgA, orgB)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Cross tenant Meta campaign','DISABLED',false)`, actorID, "cross-meta-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE'),($3::uuid,$4,'ACTIVE')`, orgA, "Cross Meta A "+orgA, orgB, "Cross Meta B "+orgB); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,status) VALUES($1::uuid,$2::uuid,'Cross Meta review','WHATSAPP','DIRECT','v1','DRAFT')`, reviewID, orgA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Cross Meta purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgA, "CROSS_META_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,'Cross pool A',$2::uuid,'ACTIVE',60,1000),($3::uuid,'Cross pool B',$4::uuid,'ACTIVE',60,1000)`, poolA, orgA, poolB, orgB); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2099, 3, 5, 12, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO meta_cloud_senders(id,organisation_id,sender_pool_id,waba_id,phone_number_id,display_name,business_phone_display,credential_key,graph_api_version,status,health_status,health_observed_at,effective_from,version,created_by,reason,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,'Cross Sender','+2348012345678',$6,'v23.0','ACTIVE','HEALTHY',$7,$8,1,$9::uuid,'cross tenant sender',$7,$7)`, senderB, orgB, poolB, "waba-"+senderB, "phone-"+senderB, "cred-"+senderB, now, now.Add(-time.Hour), actorID); err != nil {
		t.Fatal(err)
	}
	value := campaign.Campaign{ID: campaignID, OrganisationID: orgA, Name: "Cross tenant Meta campaign", PurposeID: purposeID, ConsentReviewID: reviewID, Status: campaign.StatusDraft, Timezone: "UTC", MaximumUniqueRecipients: 10, MaximumMessagesPerRecipient: 1, Transport: campaign.TransportSelection{Channel: "WHATSAPP", Provider: campaign.ProviderMeta, Engine: campaign.EngineMetaCloud, RoutingMode: campaign.RoutingSenderPool, SenderPoolID: poolA, MetaSenderID: senderB, RequiredCapabilities: []string{"SEND_TEMPLATE"}, FallbackMode: campaign.FallbackNone, RoutingPolicyVersion: "cross-route-v1", CapacityEvidenceVersion: "cross-capacity-v1"}, CreatedBy: actorID, CreatedAt: now, UpdatedAt: now, Version: 1}
	if err := (&CampaignRepository{DB: db}).Create(ctx, value); err == nil {
		t.Fatal("campaign repository accepted Meta sender owned by another organisation/pool")
	}
}
func TestPostgreSQLCampaignCompareAndSwapPreservesNullTransportFields(t *testing.T) {
	dsn := os.Getenv("POSTGRES_PAGINATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PAGINATION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var orgID, purposeID, campaignID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&orgID, &purposeID, &campaignID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
	}()
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Task 6 null transport "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Task 6 null transport','WHATSAPP','v1')`, purposeID, orgID, "NULL_"+purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Task 6 null transport',$3::uuid,'DISPATCHING',1)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	repo := &CampaignRepository{DB: db}
	loaded, err := repo.Get(ctx, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Transport.Provider != "" || loaded.Transport.Engine != "" || loaded.Transport.RoutingMode != "" {
		t.Fatalf("expected empty Go transport view for SQL NULLs, got %+v", loaded.Transport)
	}
	loaded.Status = campaign.StatusCompleted
	loaded.Version = 2
	loaded.UpdatedAt = time.Date(2026, 8, 9, 21, 40, 0, 0, time.UTC)
	if err := repo.CompareAndSwap(ctx, loaded, 1); err != nil {
		t.Fatalf("status-only compare-and-swap corrupted nullable transport fields: %v", err)
	}
	var channelNull, providerNull, engineNull, routingNull bool
	if err := db.QueryRowContext(ctx, `SELECT transport_channel IS NULL,transport_provider IS NULL,transport_engine IS NULL,transport_routing_mode IS NULL FROM campaigns WHERE id=$1::uuid`, campaignID).Scan(&channelNull, &providerNull, &engineNull, &routingNull); err != nil {
		t.Fatal(err)
	}
	if !channelNull || !providerNull || !engineNull || !routingNull {
		t.Fatalf("status-only compare-and-swap did not preserve SQL NULL transport fields")
	}
}

func TestPostgreSQLCampaignRepositoryCreatesUnfrozenTransportAsSQLNull(t *testing.T) {
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
	ids := make([]string, 5)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, reviewID, purposeID, campaignID := ids[0], ids[1], ids[2], ids[3], ids[4]
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_reviews WHERE id=$1::uuid`, reviewID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Unfrozen campaign','DISABLED',false)`, actorID, "unfrozen-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Unfrozen campaign "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,status) VALUES($1::uuid,$2::uuid,'Unfrozen campaign','WHATSAPP','DIRECT','v1','DRAFT')`, reviewID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Unfrozen campaign','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "UNFROZEN_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2099, 3, 6, 12, 0, 0, 0, time.UTC)
	value := campaign.Campaign{ID: campaignID, OrganisationID: orgID, Name: "Unfrozen campaign", PurposeID: purposeID, ConsentReviewID: reviewID, Status: campaign.StatusDraft, Timezone: "UTC", MaximumUniqueRecipients: 10, MaximumMessagesPerRecipient: 1, CreatedBy: actorID, CreatedAt: now, UpdatedAt: now, Version: 1}
	repo := &CampaignRepository{DB: db}
	if err := repo.Create(ctx, value); err != nil {
		t.Fatalf("create unfrozen campaign: %v", err)
	}
	var channelNull, providerNull, engineNull, routingNull, gatewayNull, adapterNull, routingPolicyNull, capacityNull bool
	var fallback string
	var caps string
	if err := db.QueryRowContext(ctx, `SELECT transport_channel IS NULL,transport_provider IS NULL,transport_engine IS NULL,transport_routing_mode IS NULL,gateway_pool_id IS NULL,provider_adapter_version IS NULL,routing_policy_version IS NULL,capacity_evidence_version IS NULL,fallback_mode,required_capabilities::text FROM campaigns WHERE id=$1::uuid`, campaignID).Scan(&channelNull, &providerNull, &engineNull, &routingNull, &gatewayNull, &adapterNull, &routingPolicyNull, &capacityNull, &fallback, &caps); err != nil {
		t.Fatal(err)
	}
	if !channelNull || !providerNull || !engineNull || !routingNull || !gatewayNull || !adapterNull || !routingPolicyNull || !capacityNull {
		t.Fatal("unfrozen transport evidence was persisted as non-NULL text")
	}
	if fallback != "NONE" || caps != "[]" {
		t.Fatalf("unexpected unfrozen defaults: fallback=%q capabilities=%s", fallback, caps)
	}
	loaded, err := repo.Get(ctx, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Version = 2
	loaded.UpdatedAt = now.Add(time.Minute)
	if err := repo.CompareAndSwap(ctx, loaded, 1); err != nil {
		t.Fatalf("CAS unfrozen campaign: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT routing_policy_version IS NULL,capacity_evidence_version IS NULL FROM campaigns WHERE id=$1::uuid`, campaignID).Scan(&routingPolicyNull, &capacityNull); err != nil {
		t.Fatal(err)
	}
	if !routingPolicyNull || !capacityNull {
		t.Fatal("CAS converted unfrozen nullable route evidence into empty text")
	}
}
