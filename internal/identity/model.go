package identity

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

type Status string

const (
	StatusInvited   Status = "INVITED"
	StatusActive    Status = "ACTIVE"
	StatusSuspended Status = "SUSPENDED"
	StatusDisabled  Status = "DISABLED"
)

type User struct {
	ID               string              `json:"id"`
	Email            string              `json:"email"`
	DisplayName      string              `json:"displayName"`
	Status           Status              `json:"status"`
	PasswordHash     string              `json:"-"`
	TOTPSecret       string              `json:"-"`
	MFARequired      bool                `json:"mfaRequired"`
	Permissions      map[string]struct{} `json:"-"`
	FailedLoginCount int                 `json:"-"`
	LockedUntil      *time.Time          `json:"lockedUntil,omitempty"`
	LastLoginAt      *time.Time          `json:"lastLoginAt,omitempty"`
}

func (u User) HasPermission(permission string) bool {
	if _, ok := u.Permissions["*"]; ok {
		return true
	}
	_, ok := u.Permissions[permission]
	return ok
}

func (u User) PermissionList() []string {
	values := make([]string, 0, len(u.Permissions))
	for value := range u.Permissions {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

var ErrUserNotFound = errors.New("user not found")

type Repository interface {
	ByEmail(context.Context, string) (User, error)
	ByID(context.Context, string) (User, error)
	Save(context.Context, User) error
}

type MemoryRepository struct {
	mu      sync.RWMutex
	byID    map[string]User
	byEmail map[string]string
}

func NewMemoryRepository(users ...User) *MemoryRepository {
	repository := &MemoryRepository{byID: make(map[string]User), byEmail: make(map[string]string)}
	for _, user := range users {
		_ = repository.Save(context.Background(), user)
	}
	return repository
}

func (r *MemoryRepository) ByEmail(_ context.Context, email string) (User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	identifier, ok := r.byEmail[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return cloneUser(r.byID[identifier]), nil
}

func (r *MemoryRepository) ByID(_ context.Context, identifier string) (User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	user, ok := r.byID[identifier]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return cloneUser(user), nil
}

func (r *MemoryRepository) Save(_ context.Context, user User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	user.Email = strings.ToLower(strings.TrimSpace(user.Email))
	user.Permissions = clonePermissions(user.Permissions)
	r.byID[user.ID] = user
	r.byEmail[user.Email] = user.ID
	return nil
}

func cloneUser(user User) User {
	user.Permissions = clonePermissions(user.Permissions)
	return user
}

func clonePermissions(input map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{}, len(input))
	for key := range input {
		result[key] = struct{}{}
	}
	return result
}

// RecordFailedLogin updates only authentication counters, preventing a stale
// login attempt from overwriting concurrent profile, role or status changes.
func (r *MemoryRepository) RecordFailedLogin(_ context.Context, userID string, now time.Time, threshold int, lockDuration time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	user, ok := r.byID[userID]
	if !ok {
		return ErrUserNotFound
	}
	user.FailedLoginCount++
	if threshold > 0 && user.FailedLoginCount >= threshold {
		locked := now.UTC().Add(lockDuration)
		user.LockedUntil = &locked
	}
	r.byID[userID] = cloneUser(user)
	return nil
}

func (r *MemoryRepository) RecordSuccessfulLogin(_ context.Context, userID string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	user, ok := r.byID[userID]
	if !ok {
		return ErrUserNotFound
	}
	value := now.UTC()
	user.LastLoginAt = &value
	user.FailedLoginCount = 0
	user.LockedUntil = nil
	r.byID[userID] = cloneUser(user)
	return nil
}
