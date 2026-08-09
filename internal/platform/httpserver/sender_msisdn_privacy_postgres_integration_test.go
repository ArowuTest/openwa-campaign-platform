package httpserver

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/identity"
	_ "campaign-platform/internal/persistence/database"
	"campaign-platform/internal/sender"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

func TestPostgreSQLSenderRegistrationEncryptsMSISDNAndReturnsOnlyMaskedIdentity(t *testing.T) {
	dsn := os.Getenv("POSTGRES_SENDER_PRIVACY_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_SENDER_PRIVACY_DATABASE_URL is not set")
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
	var actorID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Sender Privacy Evidence','DISABLED',false)`, actorID, "sender-privacy-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}

	protector, err := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{0x31}, 32), bytes.Repeat([]byte{0x72}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store := &sender.PostgreSQLGovernanceStore{DB: db}
	server := &Server{deps: Dependencies{
		SenderGovernance: &sender.GovernanceService{Store: store},
		MSISDNProtector:  protector,
	}}
	plain := "+2348012345678"
	masked := sharedcrypto.Mask(plain)
	body := `{"msisdn":"` + plain + `","ownerReference":"ops-team","registrationCountryIso2":"NG","profileDisplayName":"Primary sender","recoveryReference":"vault://sender/privacy-proof","engineType":"WHATSAPP_WEB_JS","safeMessagesPerMinute":10,"safeDailyCapacity":100,"inFlightLimit":1,"reason":"approved sender registration"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sender-sessions", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{
		User: identity.User{ID: actorID},
	}))
	response := httptest.NewRecorder()
	server.registerSenderSession(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), plain) {
		t.Fatalf("plaintext sender MSISDN leaked in response: %s", response.Body.String())
	}
	var created sender.GovernedSession
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.MaskedMSISDN != masked {
		t.Fatalf("unexpected sender response: %+v", created)
	}

	var encrypted []byte
	var storedMasked string
	if err := db.QueryRowContext(ctx, `SELECT encrypted_msisdn,masked_msisdn FROM sender_sessions WHERE id=$1::uuid`, created.ID).Scan(&encrypted, &storedMasked); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(encrypted, []byte(plain)) || storedMasked != masked {
		t.Fatalf("sender persistence was not encrypted/masked: encryptedEqualsPlain=%v masked=%q", bytes.Equal(encrypted, []byte(plain)), storedMasked)
	}
	decrypted, err := protector.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != plain {
		t.Fatalf("decrypted sender MSISDN=%q want=%q", decrypted, plain)
	}
	ordinaryRead, err := store.GetSession(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryJSON, err := json.Marshal(ordinaryRead)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ordinaryJSON, []byte(plain)) || !bytes.Contains(ordinaryJSON, []byte(masked)) {
		t.Fatalf("ordinary sender read leaked plaintext or omitted mask: %s", ordinaryJSON)
	}
}
