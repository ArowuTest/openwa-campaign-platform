package dispatch

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLGovernedRouteCarriesExactNodeURL(t *testing.T) {
	dsn := os.Getenv("POSTGRES_FINAL_ELIGIBILITY_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_FINAL_ELIGIBILITY_DATABASE_URL is not set")
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
	ids := make([]string, 12)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	orgID, purposeID, campaignID, messageID := ids[0], ids[1], ids[2], ids[3]
	snapshotID, contactID, recipientID, poolID := ids[4], ids[5], ids[6], ids[7]
	definitionID, nodeID, sessionID, actorID := ids[8], ids[9], ids[10], ids[11]
	now := time.Date(2099, 4, 3, 11, 0, 0, 0, time.UTC)
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = db.ExecContext(cleanup, `UPDATE campaigns SET transport_provider=NULL,transport_engine=NULL,transport_routing_mode=NULL,transport_session_id=NULL,gateway_pool_id=NULL,gateway_pool_version=NULL,provider_adapter_version=NULL,provider_capability_definition_id=NULL,provider_capability_definition_version=NULL WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_session_authority_events WHERE session_id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_session_authorities WHERE session_id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_session_leases WHERE session_id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_sessions WHERE id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_nodes WHERE id=$1::uuid`, nodeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM provider_capability_definitions WHERE id=$1::uuid`, definitionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pools WHERE id=$1::uuid`, poolID)
	}()
	must := func(query string, values ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, values...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Node URL actor','ACTIVE',false)`, actorID, "node-url-"+actorID+"@internal.invalid")
	must(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Node URL org "+orgID)
	must(`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Node URL purpose','WHATSAPP','v1')`, purposeID, orgID, "NODE_"+purposeID)
	must(`INSERT INTO gateway_pools(id,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,version) VALUES($1::uuid,$2,'OPENWA','BAILEYS','0.13.0','ACTIVE','["SEND_TEXT"]'::jsonb,1,1)`, poolID, "node-url-"+poolID)
	must(`INSERT INTO provider_capability_definitions(id,provider,channel,engine,adapter_version,capabilities,status,effective_from,version,created_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,'OPENWA','WHATSAPP','BAILEYS','0.13.0',ARRAY['SEND_TEXT'],'ACTIVE',$2,1,$3::uuid,$3::uuid,'node url governed definition',$2,$2)`, definitionID, now.Add(-time.Minute), actorID)
	must(`INSERT INTO sender_nodes(id,name,status,gateway_pool_id,internal_url,provider,engine,adapter_version,governance_version,last_heartbeat_at) VALUES($1::uuid,$2,'READY',$3::uuid,'http://10.20.30.42:2785','OPENWA','BAILEYS','0.13.0',4,$4)`, nodeID, "node-url-"+nodeID, poolID, now)
	must(`INSERT INTO sender_sessions(id,node_id,encrypted_msisdn,masked_msisdn,engine_type,status,gateway_pool_id,governance_version,last_heartbeat_at) VALUES($1::uuid,$2::uuid,decode('00','hex'),'***4242','BAILEYS','READY',$3::uuid,6,$4)`, sessionID, nodeID, poolID, now)
	must(`INSERT INTO sender_session_leases(session_id,worker_node_id,lease_token_hash,version,acquired_at,renewed_at,expires_at) VALUES($1::uuid,$2::uuid,decode(repeat('ab',32),'hex'),8,$3,$3,$4)`, sessionID, nodeID, now, now.Add(10*time.Minute))
	must(`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,maximum_messages_per_recipient,transport_provider,transport_engine,transport_routing_mode,transport_session_id,gateway_pool_id,gateway_pool_version,provider_adapter_version,provider_capability_definition_id,provider_capability_definition_version,required_capabilities) VALUES($1::uuid,$2::uuid,'Node URL campaign',$3::uuid,'SCHEDULED',1,1,'OPENWA','BAILEYS','SPECIFIC_SESSION',$4::uuid,$5,1,'0.13.0',$6::uuid,1,'["SEND_TEXT"]'::jsonb)`, campaignID, orgID, purposeID, sessionID, poolID, definitionID)
	must(`INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','node url',repeat('a',64),'APPROVED',$3)`, messageID, campaignID, "node-url-msg-"+messageID)
	must(`INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count,configuration_version) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,1,'node-url')`, snapshotID, campaignID, "node-url-snapshot-"+snapshotID)
	must(`INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***4242','ACTIVE',$3)`, contactID, contactID, now)
	must(`INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,eligibility_evidence_hash,attempt_count,authorised_at,updated_at,version) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'AUTHORISED','node-url-evidence',0,$7,$7,1)`, recipientID, campaignID, snapshotID, contactID, messageID, "node-url:"+recipientID, now)

	loader := &PostgreSQLMaterialLoader{DB: db}
	route, err := loader.loadGovernedRoute(ctx, recipientID, sessionID, "text", campaignID+":"+recipientID, now)
	if err != nil {
		t.Fatal(err)
	}
	if route.GatewayNodeID != nodeID || route.GatewayNodeURL != "http://10.20.30.42:2785" {
		t.Fatalf("governed campaign route lost exact node destination: %+v", route)
	}
}
