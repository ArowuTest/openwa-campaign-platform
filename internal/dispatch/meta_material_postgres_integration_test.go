package dispatch

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/metacloud"
	_ "campaign-platform/internal/persistence/database"
	"campaign-platform/internal/sender"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

type metaNeverAllocator struct{ calls int }

func (a *metaNeverAllocator) Assign(context.Context, string, sender.AllocationRoute, time.Time) (string, error) {
	a.calls++
	return "", errors.New("Meta dispatch must not invoke the session allocator")
}

func TestPostgreSQLMetaMaterialUsesFrozenSenderWithoutSessionAllocation(t *testing.T) {
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
	ids := make([]string, 13)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, purposeID, campaignID := ids[0], ids[1], ids[2], ids[3]
	messageID, contactID, snapshotID, recipientID := ids[4], ids[5], ids[6], ids[7]
	poolID, metaSenderID, providerDefID, routingPlanID, shardID := ids[8], ids[9], ids[10], ids[11], ids[12]
	now := time.Date(2026, 8, 12, 4, 30, 0, 0, time.UTC)
	protector, err := sharedcrypto.NewMSISDNProtector(make([]byte, 32), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	e164 := "+2348012345678"
	encrypted, err := protector.Encrypt(e164)
	if err != nil {
		t.Fatal(err)
	}
	lookup := protector.LookupHMAC(e164)
	templateComponents := []byte(`[{"type":"BODY","text":"Hello {{1}}"}]`)
	templateHash, err := metacloud.CanonicalComponentHash(templateComponents)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Meta material actor','ACTIVE',false)`, actorID, "meta-material-"+actorID[:8]+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Meta material "+orgID[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Meta material','WHATSAPP','v1')`, purposeID, orgID, "MM_"+purposeID[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,maximum_messages_per_recipient,required_capabilities) VALUES($1::uuid,$2::uuid,'Meta material',$3::uuid,'SCHEDULED',1,1,'["SEND_TEXT"]'::jsonb)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,destination_links,variables,content_hash,client_request_id,status,created_by,approved_by,created_at,approved_at) VALUES($1::uuid,$2::uuid,1,'TEXT','Hello {{contact.msisdn_masked}}','[]'::jsonb,'[{"name":"contact.msisdn_masked","dataType":"TEXT","fallback":""}]'::jsonb,repeat('c',64),$3,'APPROVED',$4::uuid,$4::uuid,$5,$5)`, messageID, campaignID, "meta-material-"+messageID, actorID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET approved_message_version_id=$2::uuid WHERE id=$1::uuid`, campaignID, messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',50,5000)`, poolID, "meta-material-"+poolID[:8], orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meta_cloud_senders(id,organisation_id,sender_pool_id,waba_id,phone_number_id,display_name,business_phone_display,credential_key,graph_api_version,status,health_status,health_observed_at,effective_from,version,created_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,'Meta Material','+234 ***','meta-material-key','v23.0','ACTIVE','HEALTHY',$6,$7,4,$8::uuid,$8::uuid,'approved Meta sender',$7,$6)`, metaSenderID, orgID, poolID, "waba-"+orgID[:8], "phone-"+orgID[:8], now, now.Add(-time.Hour), actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_capability_definitions(id,provider,channel,engine,adapter_version,capabilities,status,effective_from,effective_to,version,created_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,'META','WHATSAPP','CLOUD_API','1.0.0','{SEND_TEXT,SEND_TEMPLATE}'::text[],'ACTIVE',$2,$3,1,$4::uuid,$4::uuid,'approved Meta provider',$2,$2)`, providerDefID, now.Add(-time.Hour), now.Add(time.Hour), actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meta_cloud_templates(organisation_id,waba_id,meta_template_id,name,language,category,status,components,component_hash,last_synced_at) VALUES($1::uuid,$2,'tpl-1','hello_masked','en_US','MARKETING','APPROVED',$3::jsonb,$4,$5)`, orgID, "waba-"+orgID[:8], string(templateComponents), templateHash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meta_cloud_message_bindings(message_version_id,template_name,language,body_variable_names,component_bindings,template_component_hash,created_by,created_at) VALUES($1::uuid,'hello_masked','en_US','["contact.msisdn_masked"]'::jsonb,'[{"type":"BODY","subType":"TEXT","parameterNames":["contact.msisdn_masked"]}]'::jsonb,$2,$3::uuid,$4)`, messageID, templateHash, actorID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_routing_plans(id,campaign_id,plan_version,distribution_mode,routing_policy_version,capacity_evidence_version,pacing_policy_version,fallback_mode,approved_at,approved_by,idempotency_key,request_hash) VALUES($1::uuid,$2::uuid,1,'WEIGHTED','rp-meta','cap-meta','pace-meta','NONE',$3,$4::uuid,$5,$6)`, routingPlanID, campaignID, now, actorID, "meta-material-route-"+routingPlanID, "meta-material-hash-"+routingPlanID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_routing_plan_pools(routing_plan_id,sender_pool_id,meta_sender_id,provider,engine,provider_adapter_version,provider_capability_definition_id,provider_capability_definition_version,meta_sender_version,allocation_weight,maximum_recipients,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units) VALUES($1::uuid,$2::uuid,$3::uuid,'META','CLOUD_API','1.0.0',$4::uuid,1,4,100,1,50,500,5000)`, routingPlanID, poolID, metaSenderID, providerDefID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_dispatch_shards(id,campaign_id,ordinal,target_size,recipient_count,status,assigned_sender_pool_id,routing_plan_id) VALUES($1::uuid,$2::uuid,0,1,1,'PENDING',$3::uuid,$4::uuid)`, shardID, campaignID, poolID, routingPlanID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count,configuration_version) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,1,'meta-material')`, snapshotID, campaignID, "snapshot-"+snapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,$2,$3,$4,'ACTIVE',$5)`, contactID, encrypted, lookup, "+234******5678", now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,eligibility_evidence_hash,attempt_count,authorised_at,updated_at,version,dispatch_shard_id) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'AUTHORISED','meta-material',0,$7,$7,1,$8::uuid)`, recipientID, campaignID, snapshotID, contactID, messageID, "meta-material:"+recipientID, now, shardID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		_, _ = db.ExecContext(cleanup, `DELETE FROM meta_cloud_conversation_windows WHERE meta_sender_id=$1::uuid`, metaSenderID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_recipients WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM audience_snapshots WHERE id=$1::uuid`, snapshotID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_dispatch_shards WHERE id=$1::uuid`, shardID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_routing_plan_pools WHERE routing_plan_id=$1::uuid`, routingPlanID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_routing_plans WHERE id=$1::uuid`, routingPlanID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM meta_cloud_message_bindings WHERE message_version_id=$1::uuid`, messageID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM meta_cloud_templates WHERE organisation_id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `UPDATE campaigns SET approved_message_version_id=NULL,transport_channel=NULL,transport_provider=NULL,transport_engine=NULL,transport_routing_mode=NULL,transport_sender_pool_id=NULL,meta_sender_id=NULL,provider_adapter_version=NULL,provider_capability_definition_id=NULL,provider_capability_definition_version=NULL,routing_policy_version=NULL,capacity_evidence_version=NULL WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM provider_capability_definitions WHERE id=$1::uuid`, providerDefID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM meta_cloud_senders WHERE id=$1::uuid`, metaSenderID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM contacts WHERE id=$1::uuid`, contactID)
		_, _ = db.ExecContext(cleanup, `UPDATE campaigns SET approved_message_version_id=NULL WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM message_versions WHERE id=$1::uuid`, messageID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_pools WHERE id=$1::uuid`, poolID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	allocator := &metaNeverAllocator{}
	recipient := delivery.Recipient{ID: recipientID, CampaignID: campaignID, ContactID: contactID, MessageVersionID: messageID, IdempotencyKey: "meta-material:" + recipientID, Status: delivery.StatusAuthorised}
	material, err := (&PostgreSQLMaterialLoader{DB: db, Protector: protector, Allocator: allocator, Clock: func() time.Time { return now }}).Load(ctx, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if allocator.calls != 0 {
		t.Fatalf("Meta material invoked session allocator %d times", allocator.calls)
	}
	if material.Provider != "META" || material.Engine != "CLOUD_API" || material.MetaSenderID != metaSenderID || material.MetaSenderVersion != 4 {
		t.Fatalf("unexpected Meta route evidence: %#v", material)
	}
	if material.MetaRepresentation != MetaRepresentationTemplate {
		t.Fatalf("Meta recipient without explicit window evidence must use template representation: %#v", material)
	}
	if !material.MetaFreeFormEligibleUntil.IsZero() {
		t.Fatalf("template material carried free-form eligibility deadline: %s", material.MetaFreeFormEligibleUntil)
	}
	if material.SessionID != "" || material.GatewayNodeID != "" || material.GatewayPoolID != "" {
		t.Fatalf("Meta material fabricated OpenWA authority: %#v", material)
	}
	if material.RecipientE164 != e164 || material.MetaTemplateName != "hello_masked" || material.MetaTemplateLanguage != "en_US" || len(material.MetaBodyParameters) != 1 || material.MetaBodyParameters[0] != "+234******5678" {
		t.Fatalf("unexpected Meta rendered material: %#v", material)
	}
	if len(material.MetaComponents) != 1 || material.MetaComponents[0].Type != "BODY" || len(material.MetaComponents[0].Parameters) != 1 || material.MetaComponents[0].Parameters[0].Text != "+234******5678" {
		t.Fatalf("typed Meta components were not rendered from binding evidence: %#v", material.MetaComponents)
	}

	windowStore := &metacloud.PostgreSQLConversationWindowStore{DB: db}
	if _, _, err := windowStore.ObserveInbound(ctx, metacloud.ConversationWindowObservation{
		MetaSenderID: metaSenderID, ContactID: contactID, ProviderMessageID: "wamid.freeform.1", OccurredAt: now.Add(-time.Hour),
	}, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	freeForm, err := (&PostgreSQLMaterialLoader{DB: db, Protector: protector, Allocator: allocator, MetaConversationWindow: 24 * time.Hour, Clock: func() time.Time { return now }}).Load(ctx, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if freeForm.MetaRepresentation != MetaRepresentationFreeForm || freeForm.Body != "Hello +234******5678" || freeForm.MetaTemplateName != "" || len(freeForm.MetaComponents) != 0 {
		t.Fatalf("fresh sender/contact conversation evidence did not choose canonical free-form material: %#v", freeForm)
	}
	if want := now.Add(23 * time.Hour); !freeForm.MetaFreeFormEligibleUntil.Equal(want) {
		t.Fatalf("free-form deadline=%s want %s", freeForm.MetaFreeFormEligibleUntil, want)
	}
	shortened, err := (&PostgreSQLMaterialLoader{DB: db, Protector: protector, Allocator: allocator, MetaConversationWindow: 30 * time.Minute, Clock: func() time.Time { return now }}).Load(ctx, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if shortened.MetaRepresentation != MetaRepresentationTemplate {
		t.Fatalf("tightened current policy did not expire older free-form evidence: %#v", shortened)
	}
	if !shortened.MetaFreeFormEligibleUntil.IsZero() {
		t.Fatalf("template fallback retained stale free-form deadline: %s", shortened.MetaFreeFormEligibleUntil)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM meta_cloud_message_bindings WHERE message_version_id=$1::uuid`, messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM meta_cloud_templates WHERE organisation_id=$1::uuid`, orgID); err != nil {
		t.Fatal(err)
	}
	freeFormWithoutTemplate, err := (&PostgreSQLMaterialLoader{DB: db, Protector: protector, Allocator: allocator, MetaConversationWindow: 24 * time.Hour, Clock: func() time.Time { return now }}).Load(ctx, recipient)
	if err != nil {
		t.Fatalf("fresh free-form evidence incorrectly required a template binding: %v", err)
	}
	if freeFormWithoutTemplate.MetaRepresentation != MetaRepresentationFreeForm {
		t.Fatalf("fresh free-form material without template binding=%#v", freeFormWithoutTemplate)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM meta_cloud_conversation_windows WHERE meta_sender_id=$1::uuid AND contact_id=$2::uuid`, metaSenderID, contactID); err != nil {
		t.Fatal(err)
	}
	if _, err := (&PostgreSQLMaterialLoader{DB: db, Protector: protector, Allocator: allocator, MetaConversationWindow: 24 * time.Hour, Clock: func() time.Time { return now }}).Load(ctx, recipient); err == nil {
		t.Fatal("Meta material without template binding or fresh free-form evidence was accepted")
	} else {
		var permanent PermanentMaterialError
		if !errors.As(err, &permanent) {
			t.Fatalf("missing all Meta representations was not permanent: %T %v", err, err)
		}
	}

	// Restore template evidence for the sender-health classification checks below.
	if _, err := db.ExecContext(ctx, `INSERT INTO meta_cloud_templates(organisation_id,waba_id,meta_template_id,name,language,category,status,components,component_hash,last_synced_at) VALUES($1::uuid,$2,'tpl-1','hello_masked','en_US','MARKETING','APPROVED',$3::jsonb,$4,$5)`, orgID, "waba-"+orgID[:8], string(templateComponents), templateHash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meta_cloud_message_bindings(message_version_id,template_name,language,body_variable_names,component_bindings,template_component_hash,created_by,created_at) VALUES($1::uuid,'hello_masked','en_US','["contact.msisdn_masked"]'::jsonb,'[{"type":"BODY","subType":"TEXT","parameterNames":["contact.msisdn_masked"]}]'::jsonb,$2,$3::uuid,$4)`, messageID, templateHash, actorID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE meta_cloud_senders SET health_status='UNAVAILABLE',health_observed_at=$2 WHERE id=$1::uuid`, metaSenderID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := (&PostgreSQLMaterialLoader{DB: db, Protector: protector, Allocator: allocator, Clock: func() time.Time { return now }}).Load(ctx, recipient); err == nil {
		t.Fatal("unavailable Meta sender unexpectedly produced dispatch material")
	} else {
		var permanent PermanentMaterialError
		if errors.As(err, &permanent) {
			t.Fatalf("temporary Meta sender unavailability was classified permanent: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE meta_cloud_senders SET health_status='HEALTHY',health_observed_at=$2 WHERE id=$1::uuid`, metaSenderID, now.Add(-6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := (&PostgreSQLMaterialLoader{DB: db, Protector: protector, Allocator: allocator, Clock: func() time.Time { return now }}).Load(ctx, recipient); err == nil {
		t.Fatal("stale Meta health evidence unexpectedly produced dispatch material")
	} else {
		var permanent PermanentMaterialError
		if errors.As(err, &permanent) {
			t.Fatalf("stale Meta sender health was classified permanent: %v", err)
		}
	}
	if _, err := (&PostgreSQLMaterialLoader{DB: db, Protector: protector, Allocator: allocator, MetaHealthStaleAfter: 10 * time.Minute, Clock: func() time.Time { return now }}).Load(ctx, recipient); err != nil {
		t.Fatalf("configured 10m Meta health window rejected 6m evidence: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE meta_cloud_senders SET health_status='HEALTHY',health_observed_at=$2 WHERE id=$1::uuid`, metaSenderID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET transport_channel='WHATSAPP',transport_provider='META',transport_engine='CLOUD_API',transport_routing_mode='SENDER_POOL',transport_sender_pool_id=$2::uuid,gateway_pool_id=NULL,gateway_pool_version=NULL,meta_sender_id=$3::uuid,provider_adapter_version='1.0.0',provider_capability_definition_id=$4::uuid,provider_capability_definition_version=1,routing_policy_version='rp-meta-legacy',capacity_evidence_version='cap-meta-legacy' WHERE id=$1::uuid`, campaignID, poolID, metaSenderID, providerDefID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE campaign_recipients SET dispatch_shard_id=NULL WHERE id=$1::uuid`, recipientID); err != nil {
		t.Fatal(err)
	}
	if _, err := (&PostgreSQLMaterialLoader{DB: db, Protector: protector, Allocator: allocator, Clock: func() time.Time { return now }}).Load(ctx, recipient); err == nil {
		t.Fatal("Meta material loader accepted campaign-level sender evidence without a frozen routing-plan route")
	}
}
