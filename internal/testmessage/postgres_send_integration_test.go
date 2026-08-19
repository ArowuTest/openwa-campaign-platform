package testmessage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLTestMessageSendPersistsVariableValuesIdempotently(t *testing.T) {
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
	ids := make([]string, 10)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, purposeID, campaignID, messageID := ids[0], ids[1], ids[2], ids[3], ids[4]
	recipientID, gatewayPoolID, providerDefinitionID, nodeID, sessionID := ids[5], ids[6], ids[7], ids[8], ids[9]
	now := time.Date(2099, 3, 2, 17, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 test message','DISABLED',false)`, actorID, "test-message-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `UPDATE campaigns SET transport_provider=NULL,transport_engine=NULL,transport_routing_mode=NULL,transport_session_id=NULL,transport_sender_pool_id=NULL,gateway_pool_id=NULL,gateway_pool_version=NULL,provider_adapter_version=NULL,provider_capability_definition_id=NULL,provider_capability_definition_version=NULL WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM test_message_sends WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_session_leases WHERE session_id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_sessions WHERE id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_nodes WHERE id=$1::uuid`, nodeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM approved_test_recipients WHERE id=$1::uuid`, recipientID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM message_versions WHERE id=$1::uuid`, messageID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM provider_capability_definitions WHERE id=$1::uuid`, providerDefinitionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pools WHERE id=$1::uuid`, gatewayPoolID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Task 6 test message "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Task 6 test message','WHATSAPP','v1')`, purposeID, orgID, "TEST_"+purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Task 6 test message',$3::uuid,'DRAFT',1)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id,created_by) VALUES($1::uuid,$2::uuid,1,'TEXT','Hello {{name}}',$3,'APPROVED',$4,$5::uuid)`, messageID, campaignID, strings.Repeat("a", 64), "task6-message-"+messageID, actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO approved_test_recipients(id,label,msisdn_encrypted,msisdn_lookup_hash,masked_msisdn,status,created_by,approved_by,reason,version,created_at,updated_at) VALUES($1::uuid,'Task 6 recipient',decode('00','hex'),digest($2,'sha256'),'+234 ***','ACTIVE',$3::uuid,$3::uuid,'task six approved test recipient',1,$4,$4)`, recipientID, "recipient-"+recipientID, actorID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO gateway_pools(id,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,version,created_by,approved_by,effective_from,approval_reason) VALUES($1::uuid,('Task 6 gateway ' || $1::text),'OPENWA','BAILEYS','0.13.0+task6','ACTIVE','["SEND_TEXT"]'::jsonb,1,1,$2::uuid,$2::uuid,$3,'task six gateway approval')`, gatewayPoolID, actorID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO provider_capability_definitions(id,provider,channel,engine,adapter_version,capabilities,status,effective_from,version,created_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,'OPENWA','WHATSAPP','BAILEYS','0.13.0+task6',ARRAY['SEND_TEXT'],'ACTIVE',$2,1,$3::uuid,$3::uuid,'task six provider approval',$2,$2)`, providerDefinitionID, now.Add(-time.Minute), actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_nodes(id,name,status,gateway_pool_id,internal_url,provider,engine,adapter_version,governance_version,last_heartbeat_at) VALUES($1::uuid,('Task 6 node ' || $1::text),'READY',$2::uuid,'http://task6-node.internal:2785','OPENWA','BAILEYS','0.13.0+task6',1,$3)`, nodeID, gatewayPoolID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_sessions(id,node_id,encrypted_msisdn,masked_msisdn,engine_type,status,gateway_pool_id,governance_version,last_heartbeat_at) VALUES($1::uuid,$2::uuid,decode('00','hex'),'+234 ***','BAILEYS','READY',$3::uuid,1,$4)`, sessionID, nodeID, gatewayPoolID, now); err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte("task6-test-route-lease"))
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_session_leases(session_id,worker_node_id,lease_token_hash,version,acquired_at,renewed_at,expires_at) VALUES($1::uuid,$2::uuid,$3,1,$4,$4,$5)`, sessionID, nodeID, tokenHash[:], now, now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET transport_provider='OPENWA',transport_engine='BAILEYS',transport_routing_mode='SPECIFIC_SESSION',transport_session_id=$4::uuid,provider_adapter_version='0.13.0+task6',gateway_pool_id=$2::uuid,gateway_pool_version=1,provider_capability_definition_id=$3::uuid,provider_capability_definition_version=1 WHERE id=$1::uuid`, campaignID, gatewayPoolID, providerDefinitionID, sessionID); err != nil {
		t.Fatal(err)
	}
	var sendID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&sendID); err != nil {
		t.Fatal(err)
	}
	send := Send{
		ID: sendID, CampaignID: campaignID, MessageVersionID: messageID,
		MessageContentHash: strings.Repeat("a", 64), TestRecipientID: recipientID,
		GatewayPoolID: gatewayPoolID, GatewayPoolVersion: 1, Provider: "OPENWA", Engine: "BAILEYS",
		ProviderAdapterVersion: "0.13.0+task6", ProviderDefinitionID: providerDefinitionID,
		ProviderDefinitionVersion: 1, SenderSessionID: sessionID, GatewayNodeID: nodeID, GatewayNodeVersion: 1,
		SessionLeaseVersion: 1, SessionConfigurationVersion: 1, AuthorityExpiresAt: now.Add(time.Minute),
		RouteReference: "task6:test-message", VariableValues: map[string]string{"name": "Ada"}, Status: SendPending,
		CreatedBy: actorID, Reason: "task six governed test send", IdempotencyKey: "task6-test-send-" + sendID, CreatedAt: now, UpdatedAt: now,
	}
	repo := &PostgreSQLRepository{DB: db}
	stored, err := repo.CreateSend(ctx, send)
	if err != nil {
		t.Fatal(err)
	}
	if stored.VariableValues["name"] != "Ada" || stored.Status != SendPending {
		t.Fatalf("unexpected stored send: %+v", stored)
	}
	replayed, err := repo.CreateSend(ctx, send)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != stored.ID || replayed.VariableValues["name"] != "Ada" {
		t.Fatalf("unexpected replayed send: %+v", replayed)
	}
	evidence, err := repo.ValidateTestRoute(ctx, RouteRequirements{CampaignID: campaignID, GatewayPoolID: gatewayPoolID, Provider: "OPENWA", Engine: "BAILEYS", SessionID: sessionID, RequiredCapabilities: []string{"SEND_TEXT"}, At: now})
	if err != nil {
		t.Fatalf("validate governed test route: %v", err)
	}
	if evidence.GatewayNodeID != nodeID || evidence.GatewayNodeURL != "http://task6-node.internal:2785" {
		t.Fatalf("governed test route lost node destination: %+v", evidence)
	}
}
