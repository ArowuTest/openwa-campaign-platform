package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrAccountLocked      = errors.New("account temporarily locked")
	ErrMFARequired        = errors.New("multi-factor authentication required")
	ErrInvalidMFA         = errors.New("invalid multi-factor code")
	ErrSessionNotFound    = errors.New("session not found")
	ErrSessionExpired     = errors.New("session expired")
)

type LoginResult struct {
	MFARequired    bool      `json:"mfaRequired"`
	ChallengeToken string    `json:"challengeToken,omitempty"`
	SessionToken   string    `json:"sessionToken,omitempty"`
	CSRFToken      string    `json:"csrfToken,omitempty"`
	ExpiresAt      time.Time `json:"expiresAt,omitempty"`
}

type Session struct {
	ID            string     `json:"id"`
	UserID        string     `json:"userId"`
	CSRFHash      []byte     `json:"-"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastSeenAt    time.Time  `json:"lastSeenAt"`
	ExpiresAt     time.Time  `json:"expiresAt"`
	AbsoluteAt    time.Time  `json:"absoluteAt"`
	MFAVerifiedAt *time.Time `json:"mfaVerifiedAt,omitempty"`
	SourceIP      string     `json:"sourceIp,omitempty"`
	UserAgent     string     `json:"userAgent,omitempty"`
}

type Challenge struct {
	UserID    string
	ExpiresAt time.Time
	UsedAt    *time.Time
}

type SessionRepository interface {
	Create(context.Context, [32]byte, Session) error
	Get(context.Context, [32]byte) (Session, error)
	Touch(context.Context, [32]byte, time.Time, time.Time) (Session, error)
	Revoke(context.Context, [32]byte, time.Time, string) error
	RevokeAll(context.Context, string, time.Time, string) (int, error)
	ActiveByUser(context.Context, string, time.Time) ([]Session, error)
	MarkMFAVerified(context.Context, [32]byte, time.Time) (Session, error)
}

type ChallengeRepository interface {
	Create(context.Context, [32]byte, Challenge) error
	Consume(context.Context, [32]byte, time.Time) (Challenge, error)
}

// LoginStateRepository provides atomic login-state updates so authentication cannot
// overwrite a concurrent administrator suspension, role change or profile update.
type LoginStateRepository interface {
	RecordFailedLogin(context.Context, string, time.Time, int, time.Duration) error
	RecordSuccessfulLogin(context.Context, string, time.Time) error
}

type Service struct {
	users           Repository
	sessions        SessionRepository
	challenges      ChallengeRepository
	idleTimeout     time.Duration
	absoluteTimeout time.Duration
	clock           func() time.Time
	events          EventRecorder
}

func NewService(users Repository, idleTimeout, absoluteTimeout time.Duration) *Service {
	return NewPersistentServiceWithEvents(users, NewMemorySessionRepository(), NewMemoryChallengeRepository(), NewMemoryEventRecorder(), idleTimeout, absoluteTimeout)
}

func NewPersistentService(users Repository, sessions SessionRepository, challenges ChallengeRepository, idleTimeout, absoluteTimeout time.Duration) *Service {
	return NewPersistentServiceWithEvents(users, sessions, challenges, NewMemoryEventRecorder(), idleTimeout, absoluteTimeout)
}

func NewPersistentServiceWithEvents(users Repository, sessions SessionRepository, challenges ChallengeRepository, events EventRecorder, idleTimeout, absoluteTimeout time.Duration) *Service {
	if idleTimeout <= 0 {
		idleTimeout = 30 * time.Minute
	}
	if absoluteTimeout <= 0 {
		absoluteTimeout = 12 * time.Hour
	}
	return &Service{users: users, sessions: sessions, challenges: challenges, events: events, idleTimeout: idleTimeout, absoluteTimeout: absoluteTimeout, clock: time.Now}
}

func (s *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	return s.LoginWithContext(ctx, email, password, AttemptContext{Email: email, CorrelationID: "legacy-login"})
}

func (s *Service) LoginWithContext(ctx context.Context, email, password string, attempt AttemptContext) (LoginResult, error) {
	user, err := s.users.ByEmail(ctx, email)
	if err != nil || !VerifyPassword(user.PasswordHash, password) {
		if err == nil {
			s.recordFailure(ctx, user)
			s.recordEvent(ctx, EventLoginFailed, user.ID, attempt, nil)
		} else {
			s.recordEvent(ctx, EventLoginFailed, "", attempt, nil)
		}
		return LoginResult{}, ErrInvalidCredentials
	}
	now := s.clock().UTC()
	if user.Status != StatusActive {
		return LoginResult{}, ErrInvalidCredentials
	}
	if user.LockedUntil != nil && user.LockedUntil.After(now) {
		s.recordEvent(ctx, EventAccountLocked, user.ID, attempt, nil)
		return LoginResult{}, ErrAccountLocked
	}
	user.FailedLoginCount = 0
	user.LockedUntil = nil
	if user.MFARequired {
		token, digest, err := randomToken()
		if err != nil {
			return LoginResult{}, err
		}
		if err := s.challenges.Create(ctx, digest, Challenge{UserID: user.ID, ExpiresAt: now.Add(5 * time.Minute)}); err != nil {
			return LoginResult{}, fmt.Errorf("persist MFA challenge: %w", err)
		}
		return LoginResult{MFARequired: true, ChallengeToken: token}, nil
	}
	return s.createSession(ctx, user, false, attempt)
}

func (s *Service) VerifyMFA(ctx context.Context, challengeToken, code string) (LoginResult, error) {
	return s.VerifyMFAWithContext(ctx, challengeToken, code, AttemptContext{CorrelationID: "legacy-mfa"})
}

func (s *Service) VerifyMFAWithContext(ctx context.Context, challengeToken, code string, attempt AttemptContext) (LoginResult, error) {
	digest := sha256.Sum256([]byte(strings.TrimSpace(challengeToken)))
	now := s.clock().UTC()
	item, err := s.challenges.Consume(ctx, digest, now)
	if err != nil {
		s.recordEvent(ctx, EventMFAFailed, "", attempt, nil)
		return LoginResult{}, ErrInvalidMFA
	}
	user, err := s.users.ByID(ctx, item.UserID)
	if err != nil || !VerifyTOTP(user.TOTPSecret, strings.TrimSpace(code), now) {
		s.recordEvent(ctx, EventMFAFailed, item.UserID, attempt, nil)
		return LoginResult{}, ErrInvalidMFA
	}
	result, err := s.createSession(ctx, user, true, attempt)
	if err == nil {
		s.recordEvent(ctx, EventMFASucceeded, user.ID, attempt, nil)
	}
	return result, err
}

func (s *Service) Authenticate(ctx context.Context, token string) (User, Session, error) {
	digest := sha256.Sum256([]byte(strings.TrimSpace(token)))
	now := s.clock().UTC()
	session, err := s.sessions.Get(ctx, digest)
	if err != nil {
		return User{}, Session{}, ErrSessionNotFound
	}
	if !session.ExpiresAt.After(now) || !session.AbsoluteAt.After(now) {
		_ = s.sessions.Revoke(ctx, digest, now, "expired")
		return User{}, Session{}, ErrSessionExpired
	}
	idleExpiry := now.Add(s.idleTimeout)
	if idleExpiry.After(session.AbsoluteAt) {
		idleExpiry = session.AbsoluteAt
	}
	session, err = s.sessions.Touch(ctx, digest, now, idleExpiry)
	if err != nil {
		return User{}, Session{}, err
	}
	user, err := s.users.ByID(ctx, session.UserID)
	if err != nil || user.Status != StatusActive {
		_ = s.sessions.Revoke(ctx, digest, now, "user unavailable")
		return User{}, Session{}, ErrSessionNotFound
	}
	return user, session, nil
}

func (s *Service) Revoke(token string) {
	_ = s.RevokeContext(context.Background(), token, "logout")
}

func (s *Service) RevokeContext(ctx context.Context, token, reason string) error {
	digest := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return s.sessions.Revoke(ctx, digest, s.clock().UTC(), strings.TrimSpace(reason))
}

func (s *Service) createSession(ctx context.Context, user User, mfaVerified bool, attempt AttemptContext) (LoginResult, error) {
	token, digest, err := randomToken()
	if err != nil {
		return LoginResult{}, err
	}
	csrf, csrfDigest, err := randomToken()
	if err != nil {
		return LoginResult{}, err
	}
	now := s.clock().UTC()
	session := Session{ID: newSessionID(digest), UserID: user.ID, CSRFHash: append([]byte(nil), csrfDigest[:]...), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(s.idleTimeout), AbsoluteAt: now.Add(s.absoluteTimeout), SourceIP: strings.TrimSpace(attempt.SourceIP), UserAgent: strings.TrimSpace(attempt.UserAgent)}
	if mfaVerified {
		verified := now
		session.MFAVerifiedAt = &verified
	}
	if err := s.sessions.Create(ctx, digest, session); err != nil {
		return LoginResult{}, fmt.Errorf("persist session: %w", err)
	}
	if state, ok := s.users.(LoginStateRepository); ok {
		if err := state.RecordSuccessfulLogin(ctx, user.ID, now); err != nil {
			_ = s.sessions.Revoke(ctx, digest, now, "login state update failed")
			return LoginResult{}, fmt.Errorf("update login state: %w", err)
		}
	} else {
		user.LastLoginAt = &now
		user.FailedLoginCount = 0
		user.LockedUntil = nil
		if err := s.users.Save(ctx, user); err != nil {
			_ = s.sessions.Revoke(ctx, digest, now, "login state update failed")
			return LoginResult{}, fmt.Errorf("update login state: %w", err)
		}
	}
	s.recordEvent(ctx, EventLoginSucceeded, user.ID, attempt, nil)
	return LoginResult{SessionToken: token, CSRFToken: csrf, ExpiresAt: session.ExpiresAt}, nil
}

func (s *Service) recordFailure(ctx context.Context, user User) {
	now := s.clock().UTC()
	if state, ok := s.users.(LoginStateRepository); ok {
		_ = state.RecordFailedLogin(ctx, user.ID, now, 5, 15*time.Minute)
		return
	}
	user.FailedLoginCount++
	if user.FailedLoginCount >= 5 {
		locked := now.Add(15 * time.Minute)
		user.LockedUntil = &locked
	}
	_ = s.users.Save(ctx, user)
}

func (s *Service) ActiveSessions(userID string) []Session {
	items, _ := s.ActiveSessionsContext(context.Background(), userID)
	return items
}

func (s *Service) ActiveSessionsContext(ctx context.Context, userID string) ([]Session, error) {
	return s.sessions.ActiveByUser(ctx, strings.TrimSpace(userID), s.clock().UTC())
}

func (s *Service) RevokeAll(userID string) int {
	count, _ := s.RevokeAllContext(context.Background(), userID, "administrator revoke all")
	return count
}

func (s *Service) RevokeAllContext(ctx context.Context, userID, reason string) (int, error) {
	return s.sessions.RevokeAll(ctx, strings.TrimSpace(userID), s.clock().UTC(), strings.TrimSpace(reason))
}

func (s *Service) StepUp(ctx context.Context, token, code string, attempt AttemptContext) (Session, error) {
	digest := sha256.Sum256([]byte(strings.TrimSpace(token)))
	now := s.clock().UTC()
	session, err := s.sessions.Get(ctx, digest)
	if err != nil || !session.ExpiresAt.After(now) || !session.AbsoluteAt.After(now) {
		s.recordEvent(ctx, EventStepUpRequired, "", attempt, map[string]string{"outcome": "invalid_session"})
		return Session{}, ErrSessionNotFound
	}
	user, err := s.users.ByID(ctx, session.UserID)
	if err != nil || user.Status != StatusActive || !VerifyTOTP(user.TOTPSecret, strings.TrimSpace(code), now) {
		s.recordEvent(ctx, EventMFAFailed, session.UserID, attempt, map[string]string{"purpose": "step_up"})
		return Session{}, ErrInvalidMFA
	}
	session, err = s.sessions.MarkMFAVerified(ctx, digest, now)
	if err != nil {
		return Session{}, err
	}
	s.recordEvent(ctx, EventMFASucceeded, user.ID, attempt, map[string]string{"purpose": "step_up"})
	return session, nil
}

func (s *Service) RecordPermissionDenied(ctx context.Context, userID, permission string, attempt AttemptContext) {
	s.recordEvent(ctx, EventPermissionDenied, userID, attempt, map[string]string{"permission": strings.TrimSpace(permission)})
}
func (s *Service) RecordStepUpRequired(ctx context.Context, userID, action string, attempt AttemptContext) {
	s.recordEvent(ctx, EventStepUpRequired, userID, attempt, map[string]string{"action": strings.TrimSpace(action)})
}

func (s *Service) RecordNetworkDenied(ctx context.Context, clientIP string, attempt AttemptContext) {
	attempt.SourceIP = strings.TrimSpace(clientIP)
	s.recordEvent(ctx, EventNetworkDenied, "", attempt, map[string]string{"network_policy": "allowlist"})
}
func (s *Service) recordEvent(ctx context.Context, kind AuthenticationEventType, userID string, attempt AttemptContext, metadata map[string]string) {
	if s == nil || s.events == nil {
		return
	}
	if strings.TrimSpace(attempt.CorrelationID) == "" {
		attempt.CorrelationID = "untracked"
	}
	event, err := newAuthenticationEvent(kind, userID, attempt, s.clock().UTC(), metadata)
	if err == nil {
		_ = s.events.Append(ctx, event)
	}
}

func StepUpSatisfied(session Session, now time.Time, maximumAge time.Duration) bool {
	if session.MFAVerifiedAt == nil || maximumAge <= 0 {
		return false
	}
	return session.MFAVerifiedAt.Add(maximumAge).After(now.UTC())
}

func CSRFTokenValid(session Session, token string) bool {
	if len(session.CSRFHash) != sha256.Size || strings.TrimSpace(token) == "" {
		return false
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(token)))
	var diff byte
	for i := range digest {
		diff |= digest[i] ^ session.CSRFHash[i]
	}
	return diff == 0
}

func randomToken() (string, [32]byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", [32]byte{}, fmt.Errorf("generate secure token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, sha256.Sum256([]byte(token)), nil
}

func newSessionID(digest [32]byte) string {
	// Session IDs are persisted as PostgreSQL uuid values. Derive a stable UUID
	// from the already-random token digest without exposing any token material.
	digest[6] = (digest[6] & 0x0f) | 0x40
	digest[8] = (digest[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", digest[0:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16])
}

// MemorySessionRepository is the development/test implementation of the same
// contract used by PostgreSQL. It deliberately stores only token and CSRF hashes.
type MemorySessionRepository struct {
	mu       sync.Mutex
	sessions map[[32]byte]Session
}

func NewMemorySessionRepository() *MemorySessionRepository {
	return &MemorySessionRepository{sessions: map[[32]byte]Session{}}
}
func (r *MemorySessionRepository) Create(_ context.Context, hash [32]byte, s Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s.CSRFHash = append([]byte(nil), s.CSRFHash...)
	r.sessions[hash] = s
	return nil
}
func (r *MemorySessionRepository) Get(_ context.Context, hash [32]byte) (Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[hash]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	s.CSRFHash = append([]byte(nil), s.CSRFHash...)
	return s, nil
}
func (r *MemorySessionRepository) Touch(_ context.Context, hash [32]byte, seen, expires time.Time) (Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[hash]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	s.LastSeenAt, s.ExpiresAt = seen.UTC(), expires.UTC()
	r.sessions[hash] = s
	s.CSRFHash = append([]byte(nil), s.CSRFHash...)
	return s, nil
}
func (r *MemorySessionRepository) Revoke(_ context.Context, hash [32]byte, _ time.Time, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, hash)
	return nil
}
func (r *MemorySessionRepository) RevokeAll(_ context.Context, userID string, _ time.Time, _ string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for h, s := range r.sessions {
		if s.UserID == userID {
			delete(r.sessions, h)
			n++
		}
	}
	return n, nil
}
func (r *MemorySessionRepository) ActiveByUser(_ context.Context, userID string, now time.Time) ([]Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Session{}
	for h, s := range r.sessions {
		if !s.ExpiresAt.After(now) || !s.AbsoluteAt.After(now) {
			delete(r.sessions, h)
			continue
		}
		if s.UserID == userID {
			s.CSRFHash = append([]byte(nil), s.CSRFHash...)
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (r *MemorySessionRepository) MarkMFAVerified(_ context.Context, hash [32]byte, at time.Time) (Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[hash]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	v := at.UTC()
	s.MFAVerifiedAt = &v
	r.sessions[hash] = s
	s.CSRFHash = append([]byte(nil), s.CSRFHash...)
	return s, nil
}

type MemoryChallengeRepository struct {
	mu    sync.Mutex
	items map[[32]byte]Challenge
}

func NewMemoryChallengeRepository() *MemoryChallengeRepository {
	return &MemoryChallengeRepository{items: map[[32]byte]Challenge{}}
}
func (r *MemoryChallengeRepository) Create(_ context.Context, h [32]byte, c Challenge) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[h] = c
	return nil
}
func (r *MemoryChallengeRepository) Consume(_ context.Context, h [32]byte, now time.Time) (Challenge, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.items[h]
	if !ok || c.UsedAt != nil || !c.ExpiresAt.After(now) {
		return Challenge{}, ErrInvalidMFA
	}
	v := now.UTC()
	c.UsedAt = &v
	r.items[h] = c
	return c, nil
}
