package privacy

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

func TestPostgreSQLPrivacyCaseLocatesKnownContactByProtectedExactMSISDN(t *testing.T) {
	dsn := os.Getenv("POSTGRES_CONTACT_RECTIFICATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_CONTACT_RECTIFICATION_DATABASE_URL is not set")
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

	protector, err := sharedcrypto.NewMSISDNProtector(make([]byte, 32), []byte("privacy-lookup-key-that-is-at-least-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	const msisdn = "+2348012345678"
	normalized, err := sharedcrypto.NormalizeE164(msisdn)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := protector.Encrypt(normalized)
	if err != nil {
		t.Fatal(err)
	}
	var actorID, contactID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&actorID, &contactID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM privacy_cases WHERE created_by=$1::uuid`, actorID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM contacts WHERE id=$1::uuid`, contactID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Privacy Lookup Operator','DISABLED',false)`, actorID, "privacy-lookup-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,$2,$3,'+234******5678','ACTIVE',$4)`, contactID, ciphertext, protector.LookupHMAC(normalized), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	service := &Service{Repository: &PostgreSQLRepository{DB: db}, Protector: protector, Clock: func() time.Time { return now }}
	created, err := service.Create(ctx, CreateInput{Type: CaseAccess, MSISDN: msisdn, Reason: "authorised exact subject lookup", CreatedBy: actorID}, "privacy-lookup")
	if err != nil {
		t.Fatal(err)
	}
	if created.ContactID != contactID {
		t.Fatalf("privacy case contactId=%q, want known contact %q", created.ContactID, contactID)
	}
	if created.SubjectMasked == msisdn {
		t.Fatal("privacy case exposed plaintext subject MSISDN")
	}
}
