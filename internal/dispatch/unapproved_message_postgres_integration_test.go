package dispatch

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
	_ "campaign-platform/internal/persistence/database"
	"campaign-platform/internal/sender"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

type aprNeverAllocator struct{}

func (aprNeverAllocator) Assign(context.Context, string, sender.AllocationRoute, time.Time) (string, error) {
	return "", errors.New("allocator must not run for an unapproved message")
}

func TestPostgreSQLDispatchMaterialRejectsUnapprovedMessageVersion(t *testing.T) {
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
	if err = db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 7)
	args := make([]any, 7)
	for i := range ids {
		args[i] = &ids[i]
	}
	if err = db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	org, purpose, campaignID, messageID, contactID, recipientID, snapshotID := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6]
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err = db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, org, "APR message org "+org); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'APR purpose','WHATSAPP','v1')`, purpose, org, "APR_"+purpose); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,maximum_messages_per_recipient) VALUES($1::uuid,$2::uuid,'APR message campaign',$3::uuid,'SCHEDULED',1,1)`, campaignID, org, purpose); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','unapproved message',repeat('d',64),'DRAFT',$3)`, messageID, campaignID, "apr-msg-"+messageID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count,configuration_version) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,1,'apr-test')`, snapshotID, campaignID, "snapshot-"+snapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***1234','ACTIVE',$3)`, contactID, contactID, now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,eligibility_evidence_hash,attempt_count,authorised_at,updated_at,version) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'AUTHORISED','apr-unapproved',0,$7,$7,1)`, recipientID, campaignID, snapshotID, contactID, messageID, "apr:"+recipientID, now); err != nil {
		t.Fatal(err)
	}
	recipient := delivery.Recipient{ID: recipientID, CampaignID: campaignID, ContactID: contactID, MessageVersionID: messageID, Status: delivery.StatusAuthorised}
	protector, protectErr := sharedcrypto.NewMSISDNProtector(make([]byte, 32), make([]byte, 32))
	if protectErr != nil {
		t.Fatal(protectErr)
	}
	_, err = (&PostgreSQLMaterialLoader{DB: db, Protector: protector, Allocator: aprNeverAllocator{}}).Load(ctx, recipient)
	if !errors.Is(err, delivery.ErrRecipientNotFound) {
		t.Fatalf("unapproved message dispatch material err=%v want=%v", err, delivery.ErrRecipientNotFound)
	}
}
