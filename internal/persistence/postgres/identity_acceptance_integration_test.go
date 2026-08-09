package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/identity"
	_ "campaign-platform/internal/persistence/database"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

func TestPostgreSQLIdentityMFAVisibleSessionsAndImmediateAdminRevocation(t *testing.T) {
	dsn := os.Getenv("POSTGRES_IDENTITY_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_IDENTITY_DATABASE_URL is not set")
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
	box, err := sharedcrypto.NewSecretBox(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	var actorID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'IAM acceptance actor','DISABLED',false)`, actorID, "iam-actor-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	sessions := &IdentitySessionRepository{DB: db}
	adminRepo := &IdentityAdministrationRepository{DB: db, Secrets: box}
	admin := identity.NewAdministrationService(adminRepo, sessions)
	temporaryCredential := "iam-test-credential-000001"
	provisioned, err := admin.Create(ctx, identity.CreateAccountInput{
		Email: "iam-user-" + actorID + "@example.test", DisplayName: "IAM Acceptance User",
		TemporaryPassword: temporaryCredential, RoleCodes: []string{"CAMPAIGN_OPERATOR", "FINANCE_USER"},
		ActorID: actorID, Reason: "provision unique IAM acceptance user",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := admin.Update(ctx, provisioned.Account.ID, identity.UpdateAccountInput{
		DisplayName: provisioned.Account.DisplayName, Status: identity.StatusActive, RoleCodes: provisioned.Account.RoleCodes,
		MFARequired: true, ExpectedVersion: provisioned.Account.Version, ActorID: actorID, Reason: "activate IAM acceptance user",
	})
	if err != nil {
		t.Fatal(err)
	}
	users := &IdentityRepository{DB: db, Secrets: box}
	events := &AuthenticationEventRecorder{DB: db}
	service := identity.NewPersistentServiceWithEvents(users, sessions, &IdentityChallengeRepository{DB: db}, events, 30*time.Minute, 12*time.Hour)
	login, err := service.LoginWithContext(ctx, account.Email, temporaryCredential, identity.AttemptContext{
		Email: account.Email, SourceIP: "192.0.2.44", UserAgent: "iam-acceptance-agent", CorrelationID: "iam-login-" + account.ID,
	})
	if err != nil || !login.MFARequired || login.ChallengeToken == "" || login.SessionToken != "" {
		t.Fatalf("first factor did not require MFA: result=%+v err=%v", login, err)
	}
	code := testTOTPCode(t, provisioned.TOTPSecret, time.Now().UTC())
	verified, err := service.VerifyMFAWithContext(ctx, login.ChallengeToken, code, identity.AttemptContext{
		SourceIP: "192.0.2.44", UserAgent: "iam-acceptance-agent", CorrelationID: "iam-mfa-" + account.ID,
	})
	if err != nil || verified.SessionToken == "" {
		t.Fatalf("MFA verification failed: result=%+v err=%v", verified, err)
	}
	user, session, err := service.Authenticate(ctx, verified.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if user.LastLoginAt == nil || session.SourceIP == "" || session.UserAgent != "iam-acceptance-agent" || session.MFAVerifiedAt == nil {
		t.Fatalf("authenticated access context incomplete: user=%+v session=%+v", user, session)
	}
	if !user.HasPermission("campaign.write") || !user.HasPermission("finance.write") {
		t.Fatalf("multi-role permission union incomplete: permissions=%v", user.PermissionList())
	}
	active, err := service.ActiveSessionsContext(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != session.ID || active[0].SourceIP == "" || active[0].UserAgent == "" {
		t.Fatalf("active sessions do not identify access context: %+v", active)
	}
	storedAccount, err := admin.Get(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedAccount.LastLoginAt == nil {
		t.Fatal("successful login was not visible through administration account data")
	}
	revoked, err := admin.RevokeSessions(ctx, user.ID, actorID, "revoke unexpected IAM acceptance access")
	if err != nil || revoked != 1 {
		t.Fatalf("admin revoke sessions: revoked=%d err=%v", revoked, err)
	}
	if _, _, err := service.Authenticate(ctx, verified.SessionToken); err == nil {
		t.Fatal("administrator-revoked session remained usable")
	}
	if _, err := service.LoginWithContext(ctx, "not-provisioned-"+actorID+"@example.test", temporaryCredential, identity.AttemptContext{CorrelationID: "iam-unprovisioned"}); err != identity.ErrInvalidCredentials {
		t.Fatalf("unprovisioned identity login err=%v want=%v", err, identity.ErrInvalidCredentials)
	}
	if _, ok := any(users).(identity.LoginStateRepository); !ok {
		t.Fatal("PostgreSQL identity repository does not satisfy LoginStateRepository")
	}
	if err := users.RecordFailedLogin(ctx, account.ID, time.Now().UTC(), 99, time.Minute); err != nil {
		t.Fatalf("direct failed-login update: %v", err)
	}
	var diagnosticCount int
	if err := db.QueryRowContext(ctx, `SELECT failed_login_count FROM internal_user_credentials WHERE user_id=$1::uuid`, account.ID).Scan(&diagnosticCount); err != nil || diagnosticCount != 1 {
		t.Fatalf("direct failed-login count=%d err=%v", diagnosticCount, err)
	}
	if err := users.RecordSuccessfulLogin(ctx, account.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_, err := service.LoginWithContext(ctx, account.Email, "definitely-not-the-credential", identity.AttemptContext{
			Email: account.Email, SourceIP: "192.0.2.45", CorrelationID: fmt.Sprintf("iam-failure-%d", i),
		})
		if err != identity.ErrInvalidCredentials {
			t.Fatalf("failed login %d err=%v", i+1, err)
		}
		var failedCount int
		if err := db.QueryRowContext(ctx, `SELECT failed_login_count FROM internal_user_credentials WHERE user_id=$1::uuid`, account.ID).Scan(&failedCount); err != nil || failedCount != i+1 {
			t.Fatalf("after failed login %d persisted count=%d err=%v", i+1, failedCount, err)
		}
	}
	lockedBeforeRetry, err := users.ByID(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lockedBeforeRetry.FailedLoginCount < 5 || lockedBeforeRetry.LockedUntil == nil {
		t.Fatalf("threshold did not persist lockout before retry: %+v", lockedBeforeRetry)
	}
	if _, err := service.Login(ctx, account.Email, temporaryCredential); err != identity.ErrAccountLocked {
		t.Fatalf("valid login after threshold err=%v want=%v lockedUntil=%v", err, identity.ErrAccountLocked, lockedBeforeRetry.LockedUntil)
	}
	locked, err := users.ByID(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if locked.FailedLoginCount < 5 || locked.LockedUntil == nil {
		t.Fatalf("lockout evidence missing: %+v", locked)
	}
	var loginSuccess, loginFailure, mfaSuccess int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE event_type='LOGIN_SUCCEEDED'),count(*) FILTER(WHERE event_type='LOGIN_FAILED'),count(*) FILTER(WHERE event_type='MFA_SUCCEEDED') FROM authentication_events WHERE user_id=$1::uuid`, account.ID).Scan(&loginSuccess, &loginFailure, &mfaSuccess); err != nil {
		t.Fatal(err)
	}
	if loginSuccess < 1 || loginFailure < 5 || mfaSuccess < 1 {
		t.Fatalf("authentication event evidence incomplete success=%d failure=%d mfa=%d", loginSuccess, loginFailure, mfaSuccess)
	}
	unlocked, err := admin.Unlock(ctx, account.ID, account.Version, actorID, "unlock account after lockout acceptance")
	if err != nil {
		t.Fatal(err)
	}
	secondLogin, err := service.Login(ctx, account.Email, temporaryCredential)
	if err != nil || !secondLogin.MFARequired {
		t.Fatalf("login after unlock: result=%+v err=%v", secondLogin, err)
	}
	secondVerified, err := service.VerifyMFA(ctx, secondLogin.ChallengeToken, testTOTPCode(t, provisioned.TOTPSecret, time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := admin.Update(ctx, account.ID, identity.UpdateAccountInput{
		DisplayName: unlocked.DisplayName, Status: identity.StatusDisabled, RoleCodes: unlocked.RoleCodes, MFARequired: true,
		ExpectedVersion: unlocked.Version, ActorID: actorID, Reason: "disable compromised IAM acceptance account",
	})
	if err != nil || disabled.Status != identity.StatusDisabled {
		t.Fatalf("disable account: account=%+v err=%v", disabled, err)
	}
	if _, _, err := service.Authenticate(ctx, secondVerified.SessionToken); err == nil {
		t.Fatal("disabled account retained an active session")
	}
}

func testTOTPCode(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	var payload [8]byte
	binary.BigEndian.PutUint64(payload[:], uint64(now.UTC().Unix()/30))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(payload[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	value := (uint32(digest[offset])&0x7f)<<24 |
		(uint32(digest[offset+1])&0xff)<<16 |
		(uint32(digest[offset+2])&0xff)<<8 |
		(uint32(digest[offset+3]) & 0xff)
	return fmt.Sprintf("%06d", value%1_000_000)
}
