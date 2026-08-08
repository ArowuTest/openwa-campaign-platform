package identity

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"net/mail"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/shared/id"
)

var (
	ErrAccountNotFound      = errors.New("internal user account not found")
	ErrAccountConflict      = errors.New("internal user account version conflict")
	ErrAccountDuplicate     = errors.New("internal user email already exists")
	ErrLastSuperAdmin       = errors.New("cannot disable or remove the last active super administrator")
	ErrUnknownRole          = errors.New("one or more assigned roles do not exist")
	ErrGenericSharedAccount = errors.New("generic shared accounts are not permitted")
)

type Account struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"displayName"`
	Status      Status     `json:"status"`
	MFARequired bool       `json:"mfaRequired"`
	RoleCodes   []string   `json:"roleCodes"`
	Version     int64      `json:"version"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type CreateAccountInput struct {
	Email             string   `json:"email"`
	DisplayName       string   `json:"displayName"`
	TemporaryPassword string   `json:"temporaryPassword"`
	RoleCodes         []string `json:"roleCodes"`
	ActorID           string   `json:"-"`
	Reason            string   `json:"reason"`
}

type UpdateAccountInput struct {
	DisplayName     string   `json:"displayName"`
	Status          Status   `json:"status"`
	RoleCodes       []string `json:"roleCodes"`
	MFARequired     bool     `json:"mfaRequired"`
	ExpectedVersion int64    `json:"expectedVersion"`
	ActorID         string   `json:"-"`
	Reason          string   `json:"reason"`
}

type ResetCredentialInput struct {
	TemporaryPassword string `json:"temporaryPassword"`
	ExpectedVersion   int64  `json:"expectedVersion"`
	ActorID           string `json:"-"`
	Reason            string `json:"reason"`
}

type ProvisionedAccount struct {
	Account         Account `json:"account"`
	TOTPSecret      string  `json:"totpSecret"`
	SessionsRevoked int     `json:"sessionsRevoked,omitempty"`
}

type AdministrationRepository interface {
	ListAccounts(context.Context) ([]Account, error)
	GetAccount(context.Context, string) (Account, error)
	CreateAccount(context.Context, Account, string, string, string, string) error
	CompareAndSwapAccount(context.Context, Account, int64, string, string) error
	ResetCredentials(context.Context, string, string, string, int64, string, string, time.Time) (Account, error)
	UnlockAccount(context.Context, string, int64, string, string, time.Time) (Account, error)
}

type AdministrationService struct {
	repository AdministrationRepository
	sessions   SessionRepository
	clock      func() time.Time
}

func NewAdministrationService(repository AdministrationRepository, sessions SessionRepository) *AdministrationService {
	return &AdministrationService{repository: repository, sessions: sessions, clock: time.Now}
}

func (s *AdministrationService) List(ctx context.Context) ([]Account, error) {
	if s == nil || s.repository == nil {
		return nil, errors.New("identity administration repository is required")
	}
	return s.repository.ListAccounts(ctx)
}
func (s *AdministrationService) Get(ctx context.Context, accountID string) (Account, error) {
	if s == nil || s.repository == nil {
		return Account{}, errors.New("identity administration repository is required")
	}
	return s.repository.GetAccount(ctx, strings.TrimSpace(accountID))
}
func (s *AdministrationService) Create(ctx context.Context, input CreateAccountInput) (ProvisionedAccount, error) {
	if s == nil || s.repository == nil {
		return ProvisionedAccount{}, errors.New("identity administration repository is required")
	}
	email, display, roles, err := validateAccountIdentity(input.Email, input.DisplayName, input.RoleCodes)
	if err != nil {
		return ProvisionedAccount{}, err
	}
	if err := validateAdministrationChange(input.ActorID, input.Reason); err != nil {
		return ProvisionedAccount{}, err
	}
	if len(input.TemporaryPassword) < 20 {
		return ProvisionedAccount{}, errors.New("temporary password must contain at least 20 characters")
	}
	passwordHash, err := HashPassword(input.TemporaryPassword)
	if err != nil {
		return ProvisionedAccount{}, err
	}
	totp, err := generateTOTPSecret()
	if err != nil {
		return ProvisionedAccount{}, err
	}
	identifier, err := id.New()
	if err != nil {
		return ProvisionedAccount{}, err
	}
	now := s.clock().UTC()
	account := Account{ID: identifier, Email: email, DisplayName: display, Status: StatusInvited, MFARequired: true, RoleCodes: roles, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.repository.CreateAccount(ctx, account, passwordHash, totp, strings.TrimSpace(input.ActorID), strings.TrimSpace(input.Reason)); err != nil {
		return ProvisionedAccount{}, err
	}
	return ProvisionedAccount{Account: account, TOTPSecret: totp}, nil
}
func (s *AdministrationService) Update(ctx context.Context, accountID string, input UpdateAccountInput) (Account, error) {
	if s == nil || s.repository == nil {
		return Account{}, errors.New("identity administration repository is required")
	}
	if input.ExpectedVersion <= 0 {
		return Account{}, ErrAccountConflict
	}
	if err := validateAdministrationChange(input.ActorID, input.Reason); err != nil {
		return Account{}, err
	}
	current, err := s.repository.GetAccount(ctx, strings.TrimSpace(accountID))
	if err != nil {
		return Account{}, err
	}
	if current.Version != input.ExpectedVersion {
		return Account{}, ErrAccountConflict
	}
	_, display, roles, err := validateAccountIdentity(current.Email, input.DisplayName, input.RoleCodes)
	if err != nil {
		return Account{}, err
	}
	if input.Status != "" {
		current.Status = input.Status
	}
	if current.Status != StatusInvited && current.Status != StatusActive && current.Status != StatusSuspended && current.Status != StatusDisabled {
		return Account{}, errors.New("unsupported account status")
	}
	current.DisplayName = display
	current.RoleCodes = roles
	current.MFARequired = input.MFARequired
	current.Version++
	current.UpdatedAt = s.clock().UTC()
	if err := s.repository.CompareAndSwapAccount(ctx, current, input.ExpectedVersion, strings.TrimSpace(input.ActorID), strings.TrimSpace(input.Reason)); err != nil {
		return Account{}, err
	}
	if current.Status == StatusSuspended || current.Status == StatusDisabled {
		_, _ = s.sessions.RevokeAll(ctx, current.ID, s.clock().UTC(), "account disabled or suspended")
	}
	return current, nil
}
func (s *AdministrationService) ResetCredentials(ctx context.Context, accountID string, input ResetCredentialInput) (ProvisionedAccount, error) {
	if s == nil || s.repository == nil || s.sessions == nil {
		return ProvisionedAccount{}, errors.New("identity administration dependencies are required")
	}
	if input.ExpectedVersion <= 0 {
		return ProvisionedAccount{}, ErrAccountConflict
	}
	if err := validateAdministrationChange(input.ActorID, input.Reason); err != nil {
		return ProvisionedAccount{}, err
	}
	if len(input.TemporaryPassword) < 20 {
		return ProvisionedAccount{}, errors.New("temporary password must contain at least 20 characters")
	}
	hash, err := HashPassword(input.TemporaryPassword)
	if err != nil {
		return ProvisionedAccount{}, err
	}
	totp, err := generateTOTPSecret()
	if err != nil {
		return ProvisionedAccount{}, err
	}
	account, err := s.repository.ResetCredentials(ctx, strings.TrimSpace(accountID), hash, totp, input.ExpectedVersion, strings.TrimSpace(input.ActorID), strings.TrimSpace(input.Reason), s.clock().UTC())
	if err != nil {
		return ProvisionedAccount{}, err
	}
	revoked, err := s.sessions.RevokeAll(ctx, account.ID, s.clock().UTC(), "credentials reset")
	if err != nil {
		return ProvisionedAccount{}, err
	}
	return ProvisionedAccount{Account: account, TOTPSecret: totp, SessionsRevoked: revoked}, nil
}
func (s *AdministrationService) Unlock(ctx context.Context, accountID string, expected int64, actor, reason string) (Account, error) {
	if s == nil || s.repository == nil {
		return Account{}, errors.New("identity administration repository is required")
	}
	if expected <= 0 {
		return Account{}, ErrAccountConflict
	}
	if err := validateAdministrationChange(actor, reason); err != nil {
		return Account{}, err
	}
	return s.repository.UnlockAccount(ctx, strings.TrimSpace(accountID), expected, strings.TrimSpace(actor), strings.TrimSpace(reason), s.clock().UTC())
}
func (s *AdministrationService) RevokeSessions(ctx context.Context, accountID, actor, reason string) (int, error) {
	if s == nil || s.sessions == nil {
		return 0, errors.New("session repository is required")
	}
	if err := validateAdministrationChange(actor, reason); err != nil {
		return 0, err
	}
	return s.sessions.RevokeAll(ctx, strings.TrimSpace(accountID), s.clock().UTC(), strings.TrimSpace(reason))
}

func validateAccountIdentity(rawEmail, rawDisplay string, rawRoles []string) (string, string, []string, error) {
	email := strings.ToLower(strings.TrimSpace(rawEmail))
	address, err := mail.ParseAddress(email)
	if err != nil || strings.ToLower(address.Address) != email {
		return "", "", nil, errors.New("valid account email is required")
	}
	local := strings.SplitN(email, "@", 2)[0]
	switch local {
	case "admin", "administrator", "support", "operations", "ops", "team", "shared", "info", "noreply", "no-reply":
		return "", "", nil, ErrGenericSharedAccount
	}
	display := strings.TrimSpace(rawDisplay)
	if len(display) < 2 || len(display) > 200 {
		return "", "", nil, errors.New("display name must contain 2 to 200 characters")
	}
	roles := normaliseRoleCodes(rawRoles)
	if len(roles) == 0 {
		return "", "", nil, errors.New("at least one role is required")
	}
	return email, display, roles, nil
}
func validateAdministrationChange(actor, reason string) error {
	if strings.TrimSpace(actor) == "" {
		return errors.New("administrator identity is required")
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 5 || len(reason) > 1000 {
		return errors.New("a meaningful reason of 5 to 1000 characters is required")
	}
	return nil
}
func normaliseRoleCodes(values []string) []string {
	seen := map[string]struct{}{}
	for _, v := range values {
		v = strings.ToUpper(strings.TrimSpace(v))
		if v != "" {
			seen[v] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
func generateTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

// MemoryAdministrationRepository is used by behavioural tests and local development.
type memoryLoginState struct {
	failed      int
	lockedUntil *time.Time
}

type MemoryAdministrationRepository struct {
	mu              sync.Mutex
	accounts        map[string]Account
	byEmail         map[string]string
	passwords       map[string]string
	totp            map[string]string
	roles           map[string]struct{}
	rolePermissions map[string][]string
	loginState      map[string]memoryLoginState
}

func NewMemoryAdministrationRepository(roles ...string) *MemoryAdministrationRepository {
	r := &MemoryAdministrationRepository{
		accounts: map[string]Account{}, byEmail: map[string]string{}, passwords: map[string]string{}, totp: map[string]string{},
		roles: map[string]struct{}{}, rolePermissions: map[string][]string{}, loginState: map[string]memoryLoginState{},
	}
	defaults := map[string][]string{
		"SUPER_ADMIN":         {"*"},
		"CAMPAIGN_OPERATOR":   {"organisation.read", "consent.read", "audience.read", "audience.write", "campaign.read", "campaign.write"},
		"COMPLIANCE_REVIEWER": {"organisation.read", "consent.read", "consent.review", "audit.read"},
		"CAMPAIGN_APPROVER":   {"campaign.read", "campaign.approve", "audience.read", "consent.read"},
		"ANALYST":             {"report.read", "campaign.read"},
		"TECHNICAL_ADMIN":     {"sender.read", "sender.write", "queue.read", "queue.write"},
	}
	for _, value := range roles {
		code := strings.ToUpper(strings.TrimSpace(value))
		r.roles[code] = struct{}{}
		r.rolePermissions[code] = append([]string(nil), defaults[code]...)
	}
	return r
}

func (r *MemoryAdministrationRepository) ListAccounts(_ context.Context) ([]Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		out = append(out, cloneAccount(account))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (r *MemoryAdministrationRepository) ListAccountPage(_ context.Context, limit int, before *time.Time, beforeID string) ([]Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if before != nil && !(account.CreatedAt.Before(*before) || (account.CreatedAt.Equal(*before) && account.ID < beforeID)) {
			continue
		}
		out = append(out, cloneAccount(account))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
}

func (r *MemoryAdministrationRepository) GetAccount(_ context.Context, id string) (Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, ok := r.accounts[id]
	if !ok {
		return Account{}, ErrAccountNotFound
	}
	return cloneAccount(account), nil
}
func (r *MemoryAdministrationRepository) CreateAccount(_ context.Context, account Account, passwordHash, totp, _, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byEmail[account.Email]; ok {
		return ErrAccountDuplicate
	}
	if !r.validRoles(account.RoleCodes) {
		return ErrUnknownRole
	}
	r.accounts[account.ID] = cloneAccount(account)
	r.byEmail[account.Email] = account.ID
	r.passwords[account.ID] = passwordHash
	r.totp[account.ID] = totp
	return nil
}
func (r *MemoryAdministrationRepository) CompareAndSwapAccount(_ context.Context, account Account, expected int64, _, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.accounts[account.ID]
	if !ok {
		return ErrAccountNotFound
	}
	if current.Version != expected || account.Version != expected+1 {
		return ErrAccountConflict
	}
	if !r.validRoles(account.RoleCodes) {
		return ErrUnknownRole
	}
	if hasRole(current.RoleCodes, "SUPER_ADMIN") && (!hasRole(account.RoleCodes, "SUPER_ADMIN") || account.Status != StatusActive) && r.activeSuperAdminsLocked() <= 1 {
		return ErrLastSuperAdmin
	}
	r.accounts[account.ID] = cloneAccount(account)
	return nil
}
func (r *MemoryAdministrationRepository) ResetCredentials(_ context.Context, id, passwordHash, totp string, expected int64, _, _ string, now time.Time) (Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, ok := r.accounts[id]
	if !ok {
		return Account{}, ErrAccountNotFound
	}
	if account.Version != expected {
		return Account{}, ErrAccountConflict
	}
	account.Version++
	account.MFARequired = true
	account.UpdatedAt = now.UTC()
	r.accounts[id] = cloneAccount(account)
	r.passwords[id] = passwordHash
	r.totp[id] = totp
	r.loginState[id] = memoryLoginState{}
	return cloneAccount(account), nil
}
func (r *MemoryAdministrationRepository) UnlockAccount(_ context.Context, id string, expected int64, _, _ string, now time.Time) (Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, ok := r.accounts[id]
	if !ok {
		return Account{}, ErrAccountNotFound
	}
	if account.Version != expected {
		return Account{}, ErrAccountConflict
	}
	account.Version++
	account.UpdatedAt = now.UTC()
	r.accounts[id] = cloneAccount(account)
	r.loginState[id] = memoryLoginState{}
	return cloneAccount(account), nil
}
func (r *MemoryAdministrationRepository) validRoles(roles []string) bool {
	for _, role := range roles {
		if _, ok := r.roles[role]; !ok {
			return false
		}
	}
	return true
}
func (r *MemoryAdministrationRepository) activeSuperAdminsLocked() int {
	count := 0
	for _, account := range r.accounts {
		if account.Status == StatusActive && hasRole(account.RoleCodes, "SUPER_ADMIN") {
			count++
		}
	}
	return count
}
func cloneAccount(account Account) Account {
	account.RoleCodes = append([]string(nil), account.RoleCodes...)
	return account
}
func hasRole(roles []string, target string) bool {
	for _, role := range roles {
		if role == target {
			return true
		}
	}
	return false
}

// The memory administration repository also implements the login repository so
// users provisioned through the development API can authenticate immediately.
func (r *MemoryAdministrationRepository) ByEmail(_ context.Context, email string) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byEmail[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return r.userLocked(id)
}
func (r *MemoryAdministrationRepository) ByID(_ context.Context, id string) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.userLocked(id)
}
func (r *MemoryAdministrationRepository) userLocked(id string) (User, error) {
	account, ok := r.accounts[id]
	if !ok {
		return User{}, ErrUserNotFound
	}
	permissions := map[string]struct{}{}
	for _, role := range account.RoleCodes {
		for _, permission := range r.rolePermissions[role] {
			permissions[permission] = struct{}{}
		}
	}
	state := r.loginState[id]
	return User{ID: account.ID, Email: account.Email, DisplayName: account.DisplayName, Status: account.Status, PasswordHash: r.passwords[id], TOTPSecret: r.totp[id], MFARequired: account.MFARequired, Permissions: permissions, FailedLoginCount: state.failed, LockedUntil: state.lockedUntil, LastLoginAt: account.LastLoginAt}, nil
}
func (r *MemoryAdministrationRepository) Save(_ context.Context, user User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, ok := r.accounts[user.ID]
	if !ok {
		return ErrUserNotFound
	}
	delete(r.byEmail, account.Email)
	account.Email = strings.ToLower(strings.TrimSpace(user.Email))
	account.DisplayName = user.DisplayName
	account.Status = user.Status
	account.MFARequired = user.MFARequired
	account.LastLoginAt = user.LastLoginAt
	account.UpdatedAt = time.Now().UTC()
	r.accounts[user.ID] = account
	r.byEmail[account.Email] = account.ID
	r.passwords[user.ID] = user.PasswordHash
	r.totp[user.ID] = user.TOTPSecret
	r.loginState[user.ID] = memoryLoginState{failed: user.FailedLoginCount, lockedUntil: user.LockedUntil}
	return nil
}
func (r *MemoryAdministrationRepository) RecordFailedLogin(_ context.Context, userID string, now time.Time, threshold int, lockDuration time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.accounts[userID]; !ok {
		return ErrUserNotFound
	}
	state := r.loginState[userID]
	state.failed++
	if threshold > 0 && state.failed >= threshold {
		value := now.UTC().Add(lockDuration)
		state.lockedUntil = &value
	}
	r.loginState[userID] = state
	return nil
}
func (r *MemoryAdministrationRepository) RecordSuccessfulLogin(_ context.Context, userID string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, ok := r.accounts[userID]
	if !ok {
		return ErrUserNotFound
	}
	value := now.UTC()
	account.LastLoginAt = &value
	account.UpdatedAt = value
	r.accounts[userID] = account
	r.loginState[userID] = memoryLoginState{}
	return nil
}
