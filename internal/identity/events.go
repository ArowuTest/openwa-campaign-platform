package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/shared/id"
)

type AuthenticationEventType string

const (
	EventLoginSucceeded     AuthenticationEventType = "LOGIN_SUCCEEDED"
	EventLoginFailed        AuthenticationEventType = "LOGIN_FAILED"
	EventMFASucceeded       AuthenticationEventType = "MFA_SUCCEEDED"
	EventMFAFailed          AuthenticationEventType = "MFA_FAILED"
	EventAccountLocked      AuthenticationEventType = "ACCOUNT_LOCKED"
	EventSessionRevoked     AuthenticationEventType = "SESSION_REVOKED"
	EventAllSessionsRevoked AuthenticationEventType = "ALL_SESSIONS_REVOKED"
	EventPermissionDenied   AuthenticationEventType = "PERMISSION_DENIED"
	EventStepUpRequired     AuthenticationEventType = "STEP_UP_REQUIRED"
	EventNetworkDenied      AuthenticationEventType = "NETWORK_ACCESS_DENIED"
)

type AttemptContext struct {
	Email         string
	SourceIP      string
	UserAgent     string
	CorrelationID string
}

type AuthenticationEvent struct {
	ID            string
	UserID        string
	EmailHash     string
	Type          AuthenticationEventType
	SourceIP      string
	UserAgent     string
	CorrelationID string
	OccurredAt    time.Time
	Metadata      map[string]string
}

type EventRecorder interface {
	Append(context.Context, AuthenticationEvent) error
}

type MemoryEventRecorder struct {
	mu     sync.Mutex
	events []AuthenticationEvent
}

func NewMemoryEventRecorder() *MemoryEventRecorder { return &MemoryEventRecorder{} }
func (r *MemoryEventRecorder) Append(_ context.Context, e AuthenticationEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e.Metadata = cloneMetadata(e.Metadata)
	r.events = append(r.events, e)
	return nil
}
func (r *MemoryEventRecorder) Events() []AuthenticationEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]AuthenticationEvent, len(r.events))
	for i, e := range r.events {
		e.Metadata = cloneMetadata(e.Metadata)
		out[i] = e
	}
	return out
}

func newAuthenticationEvent(kind AuthenticationEventType, userID string, attempt AttemptContext, now time.Time, metadata map[string]string) (AuthenticationEvent, error) {
	if strings.TrimSpace(attempt.CorrelationID) == "" {
		return AuthenticationEvent{}, errors.New("authentication event correlation ID is required")
	}
	identifier, err := id.New()
	if err != nil {
		return AuthenticationEvent{}, err
	}
	email := strings.ToLower(strings.TrimSpace(attempt.Email))
	emailHash := ""
	if email != "" {
		sum := sha256.Sum256([]byte(email))
		emailHash = hex.EncodeToString(sum[:])
	}
	return AuthenticationEvent{ID: identifier, UserID: strings.TrimSpace(userID), EmailHash: emailHash, Type: kind, SourceIP: strings.TrimSpace(attempt.SourceIP), UserAgent: strings.TrimSpace(attempt.UserAgent), CorrelationID: strings.TrimSpace(attempt.CorrelationID), OccurredAt: now.UTC(), Metadata: cloneMetadata(metadata)}, nil
}
func cloneMetadata(input map[string]string) map[string]string {
	out := make(map[string]string, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}
