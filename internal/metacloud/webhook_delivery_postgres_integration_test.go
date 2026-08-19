package metacloud

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
	_ "campaign-platform/internal/persistence/database"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

func TestCouncilPostgreSQLWebhookDeliveryResolverBindsSenderAndOrganisation(t *testing.T) {
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
	ids := make([]string, 13)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, purposeID, campaignID, poolID, senderID, messageID, contactID, snapshotID, recipientID, wrongOrgID, routingPlanID, shardID := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6], ids[7], ids[8], ids[9], ids[10], ids[11], ids[12]
	suffix := orgID[:8]
	rotatedSenderID := ""
	unsentMessageID, unsentRecipientID := "", ""
	mustExec := func(q string, a ...any) {
		t.Helper()
		if _, e := db.ExecContext(ctx, q, a...); e != nil {
			t.Fatal(e)
		}
	}
	mustExec(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Meta webhook actor','ACTIVE',false)`, actorID, "meta-webhook-"+suffix+"@example.test")
	mustExec(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Meta webhook "+suffix)
	mustExec(`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Meta webhook','WHATSAPP','v1')`, purposeID, orgID, "MW_"+suffix)
	mustExec(`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Meta webhook',$3::uuid,'SCHEDULED',1)`, campaignID, orgID, purposeID)
	mustExec(`INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',10,100)`, poolID, "meta-webhook-"+suffix, orgID)
	mustExec(`INSERT INTO meta_cloud_senders(id,organisation_id,sender_pool_id,waba_id,phone_number_id,display_name,business_phone_display,credential_key,graph_api_version,status,health_status,health_observed_at,effective_from,version,created_by,approved_by,reason) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,'Meta webhook','+234 ***','meta-webhook-key','v23.0','ACTIVE','HEALTHY',now(),now()-interval '1 hour',3,$6::uuid,$6::uuid,'approved webhook sender')`, senderID, orgID, poolID, "waba-"+suffix, "phone-"+suffix, actorID)
	mustExec(`UPDATE campaigns SET transport_provider='META',transport_engine='CLOUD_API',transport_routing_mode='SENDER_POOL',transport_sender_pool_id=$2::uuid,meta_sender_id=$3::uuid WHERE id=$1::uuid`, campaignID, poolID, senderID)
	mustExec(`INSERT INTO message_versions(id,campaign_id,version,message_type,body,destination_links,variables,content_hash,client_request_id,status,created_by,approved_by,created_at,approved_at) VALUES($1::uuid,$2::uuid,1,'TEXT','Hello','[]'::jsonb,'[]'::jsonb,repeat('e',64),$3,'APPROVED',$4::uuid,$4::uuid,now(),now())`, messageID, campaignID, "meta-webhook-"+messageID, actorID)
	protector, err := sharedcrypto.NewMSISDNProtector([]byte("0123456789abcdef0123456789abcdef"), []byte("abcdef0123456789abcdef0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	mustExec(`INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,'x'::bytea,$2::bytea,'+234******5678','ACTIVE',now())`, contactID, protector.LookupHMAC("+2348012345678"))
	mustExec(`INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count,configuration_version) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,1,'meta-webhook')`, snapshotID, campaignID, "snapshot-"+snapshotID)
	mustExec(`INSERT INTO campaign_routing_plans(id,campaign_id,plan_version,routing_policy_version,capacity_evidence_version,pacing_policy_version,fallback_mode,approved_at,approved_by,idempotency_key,request_hash) VALUES($1::uuid,$2::uuid,1,'route-v1','capacity-v1','pacing-v1','NONE',now(),$3::uuid,$4,$5)`, routingPlanID, campaignID, actorID, "meta-webhook-plan:"+routingPlanID, "request-hash-"+routingPlanID)
	mustExec(`INSERT INTO campaign_routing_plan_pools(routing_plan_id,sender_pool_id,gateway_pool_id,provider,engine,allocation_weight,maximum_recipients,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,allow_reallocation_in,allow_reallocation_out,meta_sender_id,meta_sender_version) VALUES($1::uuid,$2::uuid,NULL,'META','CLOUD_API',10000,1,10,10,100,false,false,$3::uuid,3)`, routingPlanID, poolID, senderID)
	mustExec(`INSERT INTO campaign_dispatch_shards(id,campaign_id,ordinal,target_size,recipient_count,status,assigned_sender_pool_id,routing_plan_id) VALUES($1::uuid,$2::uuid,0,1,1,'PENDING',$3::uuid,$4::uuid)`, shardID, campaignID, poolID, routingPlanID)
	mustExec(`INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,provider_message_id,attempt_count,authorised_at,updated_at,version,dispatch_shard_id) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'GATEWAY_ACCEPTED','wamid.bound',1,now(),now(),1,$7::uuid)`, recipientID, campaignID, snapshotID, contactID, messageID, "meta-webhook:"+recipientID, shardID)
	defer func() {
		c := context.Background()
		if unsentRecipientID != "" {
			_, _ = db.ExecContext(c, `DELETE FROM campaign_recipients WHERE id=$1::uuid`, unsentRecipientID)
		}
		_, _ = db.ExecContext(c, `DELETE FROM campaign_recipients WHERE id=$1::uuid`, recipientID)
		_, _ = db.ExecContext(c, `DELETE FROM campaign_dispatch_shards WHERE id=$1::uuid`, shardID)
		_, _ = db.ExecContext(c, `DELETE FROM campaign_routing_plan_pools WHERE routing_plan_id=$1::uuid`, routingPlanID)
		_, _ = db.ExecContext(c, `DELETE FROM campaign_routing_plan_quarantined_routes WHERE routing_plan_id=$1::uuid`, routingPlanID)
		_, _ = db.ExecContext(c, `DELETE FROM campaign_routing_plan_quarantine_evidence WHERE routing_plan_id=$1::uuid`, routingPlanID)
		_, _ = db.ExecContext(c, `DELETE FROM campaign_routing_plans WHERE id=$1::uuid`, routingPlanID)
		_, _ = db.ExecContext(c, `DELETE FROM audience_snapshots WHERE id=$1::uuid`, snapshotID)
		_, _ = db.ExecContext(c, `DELETE FROM contacts WHERE id=$1::uuid`, contactID)
		_, _ = db.ExecContext(c, `UPDATE campaigns SET meta_sender_id=NULL,transport_sender_pool_id=NULL,transport_provider=NULL,transport_engine=NULL,transport_routing_mode=NULL WHERE id=$1::uuid`, campaignID)
		if rotatedSenderID != "" {
			_, _ = db.ExecContext(c, `DELETE FROM meta_cloud_senders WHERE id=$1::uuid`, rotatedSenderID)
		}
		_, _ = db.ExecContext(c, `DELETE FROM meta_cloud_senders WHERE id=$1::uuid`, senderID)
		if unsentMessageID != "" {
			_, _ = db.ExecContext(c, `DELETE FROM message_versions WHERE id=$1::uuid`, unsentMessageID)
		}
		_, _ = db.ExecContext(c, `DELETE FROM message_versions WHERE id=$1::uuid`, messageID)
		_, _ = db.ExecContext(c, `DELETE FROM sender_pools WHERE id=$1::uuid`, poolID)
		_, _ = db.ExecContext(c, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(c, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(c, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(c, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	deliveries := delivery.NewService(&delivery.PostgreSQLRepository{DB: db})
	resolver := &PostgreSQLWebhookDeliveryResolver{DB: db, Deliveries: deliveries, Protector: protector}
	bound := Sender{ID: senderID, OrganisationID: orgID, WABAID: "waba-" + suffix, PhoneNumberID: "phone-" + suffix, CredentialKey: "meta-webhook-key", Status: StatusActive}
	got, err := resolver.ResolveMetaWebhookRecipient(ctx, bound, "wamid.bound")
	if err != nil || got.ID != recipientID {
		t.Fatalf("bound sender resolution got=%#v err=%v", got, err)
	}
	wrong := Sender{ID: "00000000-0000-4000-8000-000000000999", OrganisationID: wrongOrgID, WABAID: "waba-wrong", PhoneNumberID: "phone-wrong", CredentialKey: "meta-webhook-key", Status: StatusActive}
	if _, err := resolver.ResolveMetaWebhookRecipient(ctx, wrong, "wamid.bound"); !errors.Is(err, ErrWebhookSenderMismatch) {
		t.Fatalf("cross-sender WAMID resolved: %v", err)
	}
	sameOrgWrongSender := Sender{ID: "00000000-0000-4000-8000-000000000998", OrganisationID: orgID, WABAID: "waba-wrong-same-org", PhoneNumberID: "phone-wrong-same-org", CredentialKey: "meta-webhook-key", Status: StatusActive}
	if _, err := resolver.ResolveMetaWebhookRecipient(ctx, sameOrgWrongSender, "wamid.bound"); !errors.Is(err, ErrWebhookSenderMismatch) {
		t.Fatalf("same-organisation cross-sender WAMID classification=%v", err)
	}
	byMSISDN, err := resolver.ResolveMetaWebhookInboundRecipient(ctx, bound, "+2348012345678")
	if err != nil || byMSISDN.ID != recipientID {
		t.Fatalf("bound sender MSISDN resolution got=%#v err=%v", byMSISDN, err)
	}

	// R17 regression: a frozen Meta plan may exist before dispatch shards are assigned.
	// The frozen plan must still bind an unsharded recipient to its approved Meta sender.
	mustExec(`UPDATE campaign_recipients SET dispatch_shard_id=NULL WHERE id=$1::uuid`, recipientID)
	byMSISDN, err = resolver.ResolveMetaWebhookInboundRecipient(ctx, bound, "+2348012345678")
	if err != nil || byMSISDN.ID != recipientID {
		t.Fatalf("frozen plan unsharded recipient was not correlated: got=%#v err=%v", byMSISDN, err)
	}
	mustExec(`UPDATE campaign_recipients SET dispatch_shard_id=$2::uuid WHERE id=$1::uuid`, recipientID, shardID)

	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&unsentMessageID, &unsentRecipientID); err != nil {
		t.Fatal(err)
	}
	mustExec(`INSERT INTO message_versions(id,campaign_id,version,message_type,body,destination_links,variables,content_hash,client_request_id,status,created_by,approved_by,created_at,approved_at) VALUES($1::uuid,$2::uuid,2,'TEXT','Unsent follow-up','[]'::jsonb,'[]'::jsonb,repeat('f',64),$3,'APPROVED',$4::uuid,$4::uuid,now(),now())`, unsentMessageID, campaignID, "meta-unsent-"+unsentMessageID, actorID)
	mustExec(`INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,attempt_count,authorised_at,updated_at,version) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'AUTHORISED',0,now(),now()+interval '10 minutes',1)`, unsentRecipientID, campaignID, snapshotID, contactID, unsentMessageID, "meta-unsent:"+unsentRecipientID)
	byMSISDN, err = resolver.ResolveMetaWebhookInboundRecipient(ctx, bound, "+2348012345678")
	if err != nil || byMSISDN.ID != recipientID {
		t.Fatalf("newer unsent campaign recipient stole inbound sender correlation: got=%#v err=%v want=%s", byMSISDN, err, recipientID)
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&rotatedSenderID); err != nil {
		t.Fatal(err)
	}
	mustExec(`INSERT INTO meta_cloud_senders(id,organisation_id,sender_pool_id,waba_id,phone_number_id,display_name,business_phone_display,credential_key,graph_api_version,status,health_status,health_observed_at,effective_from,version,created_by,approved_by,reason) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,'Rotated webhook sender','+234 ***','meta-webhook-key-rotated','v23.0','ACTIVE','HEALTHY',now(),now()-interval '1 hour',1,$6::uuid,$6::uuid,'rotated webhook sender')`, rotatedSenderID, orgID, poolID, "waba-rotated-"+suffix, "phone-rotated-"+suffix, actorID)
	mustExec(`UPDATE campaigns SET meta_sender_id=$2::uuid WHERE id=$1::uuid`, campaignID, rotatedSenderID)
	rotated := Sender{ID: rotatedSenderID, OrganisationID: orgID, WABAID: "waba-rotated-" + suffix, PhoneNumberID: "phone-rotated-" + suffix, CredentialKey: "meta-webhook-key-rotated", Status: StatusActive}
	if gotRotated, rotateErr := resolver.ResolveMetaWebhookInboundRecipient(ctx, rotated, "+2348012345678"); !errors.Is(rotateErr, delivery.ErrRecipientNotFound) {
		t.Fatalf("mutable campaign sender displaced frozen routing authority: got=%#v err=%v", gotRotated, rotateErr)
	}
	mustExec(`UPDATE campaigns SET meta_sender_id=$2::uuid WHERE id=$1::uuid`, campaignID, senderID)
	mustExec(`DELETE FROM campaign_recipients WHERE id=$1::uuid`, unsentRecipientID)

	if _, err := resolver.ResolveMetaWebhookInboundRecipient(ctx, wrong, "+2348012345678"); !errors.Is(err, delivery.ErrRecipientNotFound) {
		t.Fatalf("cross-sender MSISDN resolved: %v", err)
	}

	for _, status := range []string{"SUBMITTING", "FAILED_RETRYABLE"} {
		mustExec(`UPDATE campaign_recipients SET status=$2,updated_at=now() WHERE id=$1::uuid`, recipientID, status)
		inFlight, resolveErr := resolver.ResolveMetaWebhookInboundRecipient(ctx, bound, "+2348012345678")
		if resolveErr != nil || inFlight.ID != recipientID {
			t.Fatalf("inbound sender binding disappeared in status %s: got=%#v err=%v", status, inFlight, resolveErr)
		}
	}
	mustExec(`UPDATE campaign_recipients SET status='GATEWAY_ACCEPTED',updated_at=now() WHERE id=$1::uuid`, recipientID)

	mustExec(`INSERT INTO campaign_routing_plan_quarantine_evidence(routing_plan_id,campaign_id,reason,route_evidence,reservation_evidence) VALUES($1::uuid,$2::uuid,'TEST_QUARANTINED_ROUTE','[]'::jsonb,'[]'::jsonb)`, routingPlanID, campaignID)
	mustExec(`INSERT INTO campaign_routing_plan_quarantined_routes(routing_plan_id,campaign_id,sender_pool_id,provider,engine,meta_sender_id,meta_sender_version) SELECT routing_plan_id,$2::uuid,sender_pool_id,provider,engine,meta_sender_id,meta_sender_version FROM campaign_routing_plan_pools WHERE routing_plan_id=$1::uuid AND sender_pool_id=$3::uuid`, routingPlanID, campaignID, poolID)
	mustExec(`DELETE FROM campaign_routing_plan_pools WHERE routing_plan_id=$1::uuid AND sender_pool_id=$2::uuid`, routingPlanID, poolID)

	got, err = resolver.ResolveMetaWebhookRecipient(ctx, bound, "wamid.bound")
	if err != nil || got.ID != recipientID {
		t.Fatalf("quarantined-route status resolution got=%#v err=%v", got, err)
	}
	byMSISDN, err = resolver.ResolveMetaWebhookInboundRecipient(ctx, bound, "+2348012345678")
	if err != nil || byMSISDN.ID != recipientID {
		t.Fatalf("quarantined-route inbound resolution got=%#v err=%v", byMSISDN, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_routing_plan_pools(routing_plan_id,sender_pool_id,gateway_pool_id,provider,engine,allocation_weight,maximum_recipients,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,allow_reallocation_in,allow_reallocation_out,meta_sender_id,meta_sender_version) VALUES($1::uuid,$2::uuid,NULL,'META','CLOUD_API',10000,1,10,10,100,false,false,$3::uuid,3)`, routingPlanID, poolID, senderID); err == nil {
		t.Fatal("quarantined routing plan accepted a new active route")
	}
}
