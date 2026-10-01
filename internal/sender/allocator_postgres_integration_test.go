package sender

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLAllocatorExcludesStaleHeartbeatAndExpiredLease(t *testing.T) {
	dsn := os.Getenv("POSTGRES_SENDER_ALLOCATOR_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_SENDER_ALLOCATOR_DATABASE_URL is not set")
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
	var orgID, purposeID, contactID, campaignID, messageID, snapshotID, nodeID, sessionID, recipientID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(
		&orgID, &purposeID, &contactID, &campaignID, &messageID, &snapshotID, &nodeID, &sessionID, &recipientID,
	); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_session_leases WHERE session_id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_recipients WHERE id=$1::uuid`, recipientID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_sessions WHERE id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_nodes WHERE id=$1::uuid`, nodeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM audience_snapshots WHERE id=$1::uuid`, snapshotID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM message_versions WHERE id=$1::uuid`, messageID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM contacts WHERE id=$1::uuid`, contactID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
	}()

	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "allocator-evidence-"+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),decode(replace($1::text,'-',''),'hex'),'***8008','ACTIVE',$2)`, contactID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,'ALLOCATOR_EVIDENCE','Allocator evidence','WHATSAPP','v1')`, purposeID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Allocator liveness evidence',$3::uuid,'DISPATCHING',1)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','allocator evidence',repeat('b',64),'APPROVED',$3)`, messageID, campaignID, "allocator-"+messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'allocator-v1',$3,1)`, snapshotID, campaignID, "allocator-"+snapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_nodes(id,name,status,last_heartbeat_at,capacity) VALUES($1::uuid,$2,'READY',$3,1)`, nodeID, "allocator-node-"+nodeID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_sessions(id,node_id,logical_sender_pool,encrypted_msisdn,masked_msisdn,engine_type,status,safe_messages_per_minute,safe_daily_capacity,last_heartbeat_at,in_flight_limit) VALUES($1::uuid,$2::uuid,'allocator-evidence',decode('00','hex'),'***9008','WHATSAPP_WEB_JS','READY',10,100,$3,1)`, sessionID, nodeID, now); err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte("allocator-live-lease"))
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_session_leases(session_id,worker_node_id,lease_token_hash,version,acquired_at,renewed_at,expires_at) VALUES($1::uuid,$2::uuid,$3,1,$4,$4,$5)`, sessionID, nodeID, tokenHash[:], now, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,authorised_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'AUTHORISED',$7,$7)`, recipientID, campaignID, snapshotID, contactID, messageID, "allocator-"+recipientID, now); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE sender_nodes SET boot_id='allocator-live-lease' WHERE id=$1::uuid`, nodeID); err != nil {
		t.Fatal(err)
	}
	allocator := &PostgreSQLAllocator{DB: db, HeartbeatTTL: 90 * time.Second}
	route := AllocationRoute{LegacyPool: "allocator-evidence"}
	assigned, err := allocator.Assign(ctx, recipientID, route, now)
	if err != nil || assigned != sessionID {
		t.Fatalf("fresh worker was not allocatable: assigned=%q err=%v", assigned, err)
	}

	for _, boot := range []string{"replacement-runtime", ""} {
		t.Run("current_boot_binding_"+boot, func(t *testing.T) {
			if _, err := db.ExecContext(ctx, `UPDATE sender_nodes SET boot_id=$2 WHERE id=$1::uuid`, nodeID, boot); err != nil {
				t.Fatal(err)
			}
			// Exercise both an existing assignment and fresh candidate selection.
			for _, existing := range []bool{true, false} {
				var assignedValue any
				if existing {
					assignedValue = sessionID
				}
				if _, err := db.ExecContext(ctx, `UPDATE campaign_recipients SET assigned_session_id=$2::uuid WHERE id=$1::uuid`, recipientID, assignedValue); err != nil {
					t.Fatal(err)
				}
				if got, err := allocator.Assign(ctx, recipientID, route, now); !errors.Is(err, ErrNoHealthySession) {
					t.Errorf("old-boot lease remained eligible: existing=%v assigned=%q err=%v", existing, got, err)
				}
			}
		})
	}
	// Restore the original, matching fixture for the independent liveness cases.
	if _, err := db.ExecContext(ctx, `UPDATE sender_nodes SET boot_id='allocator-live-lease' WHERE id=$1::uuid`, nodeID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE campaign_recipients SET assigned_session_id=NULL WHERE id=$1::uuid`, recipientID); err != nil {
		t.Fatal(err)
	}
	staleAt := now.Add(-91 * time.Second)
	if _, err := db.ExecContext(ctx, `UPDATE sender_nodes SET last_heartbeat_at=$2 WHERE id=$1::uuid`, nodeID, staleAt); err != nil {
		t.Fatal(err)
	}
	if _, err := allocator.Assign(ctx, recipientID, route, now); !errors.Is(err, ErrNoHealthySession) {
		t.Fatalf("stale worker remained allocatable: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE sender_nodes SET last_heartbeat_at=$2 WHERE id=$1::uuid`, nodeID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE sender_sessions SET sent_today=safe_daily_capacity WHERE id=$1::uuid`, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := allocator.Assign(ctx, recipientID, route, now); !errors.Is(err, ErrNoHealthySession) {
		t.Fatalf("capacity-exhausted sender remained allocatable: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE sender_sessions SET sent_today=0 WHERE id=$1::uuid`, sessionID); err != nil {
		t.Fatal(err)
	}
	afterExpiry := now.Add(6 * time.Minute)
	if _, err := db.ExecContext(ctx, `UPDATE sender_nodes SET last_heartbeat_at=$2 WHERE id=$1::uuid`, nodeID, afterExpiry); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE sender_sessions SET last_heartbeat_at=$2 WHERE id=$1::uuid`, sessionID, afterExpiry); err != nil {
		t.Fatal(err)
	}
	if _, err := allocator.Assign(ctx, recipientID, route, afterExpiry); !errors.Is(err, ErrNoHealthySession) {
		t.Fatalf("expired worker lease remained allocatable: %v", err)
	}
}
