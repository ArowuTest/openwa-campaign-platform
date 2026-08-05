package identity

import (
	"context"
	"encoding/base32"
	"testing"
	"time"
)

func testService(t *testing.T) (*Service, User, string) {
	t.Helper()
	hash, err := HashPassword("a-very-long-development-password")
	if err != nil {
		t.Fatal(err)
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	user := User{ID: "user-1", Email: "admin@example.test", DisplayName: "Admin", Status: StatusActive,
		PasswordHash: hash, TOTPSecret: secret, MFARequired: true, Permissions: map[string]struct{}{"*": {}}}
	service := NewService(NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	fixed := time.Unix(59, 0).UTC()
	service.clock = func() time.Time { return fixed }
	return service, user, secret
}

func TestPasswordHashRoundTrip(t *testing.T) {
	encoded, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(encoded, "correct horse battery staple") || VerifyPassword(encoded, "wrong-password-value") {
		t.Fatal("password verification failed")
	}
}

func TestLoginRequiresMFAAndCreatesSession(t *testing.T) {
	service, _, secret := testService(t)
	login, err := service.Login(context.Background(), "admin@example.test", "a-very-long-development-password")
	if err != nil || !login.MFARequired || login.ChallengeToken == "" {
		t.Fatalf("login result=%+v err=%v", login, err)
	}
	key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	code := totpCode(key, service.clock().Unix()/30)
	verified, err := service.VerifyMFA(context.Background(), login.ChallengeToken, code)
	if err != nil || verified.SessionToken == "" || verified.CSRFToken == "" {
		t.Fatalf("verify result=%+v err=%v", verified, err)
	}
	user, session, err := service.Authenticate(context.Background(), verified.SessionToken)
	if err != nil || user.ID != "user-1" || !CSRFTokenValid(session, verified.CSRFToken) {
		t.Fatalf("authenticate user=%+v session=%+v err=%v", user, session, err)
	}
}

func TestStepUpMarksExistingSessionAndRecordsEvents(t *testing.T) {
	hash, err := HashPassword("a-very-long-development-password")
	if err != nil {
		t.Fatal(err)
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	user := User{ID: "user-step", Email: "step@example.test", DisplayName: "Step", Status: StatusActive, PasswordHash: hash, TOTPSecret: secret, MFARequired: false, Permissions: map[string]struct{}{"*": {}}}
	events := NewMemoryEventRecorder()
	service := NewPersistentServiceWithEvents(NewMemoryRepository(user), NewMemorySessionRepository(), NewMemoryChallengeRepository(), events, 30*time.Minute, 12*time.Hour)
	fixed := time.Unix(59, 0).UTC()
	service.clock = func() time.Time { return fixed }
	login, err := service.LoginWithContext(context.Background(), user.Email, "a-very-long-development-password", AttemptContext{Email: user.Email, CorrelationID: "login-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, session, err := service.Authenticate(context.Background(), login.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if session.MFAVerifiedAt != nil {
		t.Fatal("non-MFA login unexpectedly marked verified")
	}
	key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	code := totpCode(key, fixed.Unix()/30)
	updated, err := service.StepUp(context.Background(), login.SessionToken, code, AttemptContext{CorrelationID: "step-1"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.MFAVerifiedAt == nil || !StepUpSatisfied(updated, fixed, 10*time.Minute) {
		t.Fatalf("step-up not recorded: %+v", updated)
	}
	if len(events.Events()) < 2 {
		t.Fatalf("expected login and MFA events, got %d", len(events.Events()))
	}
}

func TestAtomicLoginFailureUpdatesDoNotReplaceProfile(t *testing.T) {
	hash, _ := HashPassword("a-very-long-development-password")
	repo := NewMemoryRepository(User{ID: "user-atomic", Email: "atomic@example.test", DisplayName: "Original", Status: StatusActive, PasswordHash: hash, Permissions: map[string]struct{}{"a": {}}})
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		if err := repo.RecordFailedLogin(context.Background(), "user-atomic", now, 5, 15*time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	user, err := repo.ByID(context.Background(), "user-atomic")
	if err != nil {
		t.Fatal(err)
	}
	if user.DisplayName != "Original" || user.FailedLoginCount != 5 || user.LockedUntil == nil {
		t.Fatalf("unexpected atomic state: %+v", user)
	}
	if err := repo.RecordSuccessfulLogin(context.Background(), user.ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	user, _ = repo.ByID(context.Background(), user.ID)
	if user.FailedLoginCount != 0 || user.LockedUntil != nil || user.LastLoginAt == nil {
		t.Fatalf("success state not reset: %+v", user)
	}
}

func TestRecordNetworkDeniedProducesMinimisedSecurityEvent(t *testing.T) {
	hash, err := HashPassword("a-very-long-development-password")
	if err != nil {
		t.Fatal(err)
	}
	recorder := NewMemoryEventRecorder()
	service := NewPersistentServiceWithEvents(NewMemoryRepository(User{ID: "network-admin", Email: "person@example.test", Status: StatusActive, PasswordHash: hash}), NewMemorySessionRepository(), NewMemoryChallengeRepository(), recorder, 30*time.Minute, 12*time.Hour)
	service.RecordNetworkDenied(context.Background(), "198.51.100.10", AttemptContext{Email: "person@example.test", UserAgent: "test-agent", CorrelationID: "req-network-denied"})
	events := recorder.Events()
	if len(events) != 1 || events[0].Type != EventNetworkDenied || events[0].SourceIP != "198.51.100.10" || events[0].EmailHash == "" {
		t.Fatalf("unexpected network denial event: %#v", events)
	}
	if events[0].Metadata["network_policy"] != "allowlist" {
		t.Fatalf("network policy metadata missing: %#v", events[0].Metadata)
	}
}
