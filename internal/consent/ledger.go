package consent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/shared/id"
)

type GrantStatus string

const (
	GrantPendingVerification GrantStatus = "PENDING_VERIFICATION"
	GrantActive              GrantStatus = "ACTIVE"
	GrantExpired             GrantStatus = "EXPIRED"
	GrantWithdrawn           GrantStatus = "WITHDRAWN"
	GrantRevoked             GrantStatus = "REVOKED"
	GrantSuperseded          GrantStatus = "SUPERSEDED"
)

type Grant struct {
	ID                 string      `json:"id"`
	ContactID          string      `json:"contactId"`
	OrganisationID     string      `json:"organisationId"`
	PurposeID          string      `json:"purposeId"`
	Channel            string      `json:"channel"`
	ConsentReviewID    string      `json:"consentReviewId"`
	WordingVersion     string      `json:"wordingVersion"`
	SourceType         string      `json:"sourceType"`
	SourceReference    string      `json:"sourceReference,omitempty"`
	EvidenceObjectKey  string      `json:"evidenceObjectKey,omitempty"`
	EvidenceChecksum   string      `json:"evidenceChecksum"`
	EffectiveFrom      time.Time   `json:"effectiveFrom"`
	GrantedAt          time.Time   `json:"grantedAt"`
	ExpiresAt          *time.Time  `json:"expiresAt,omitempty"`
	Status             GrantStatus `json:"status"`
	CreatedBy          string      `json:"createdBy"`
	ClientRequestID    string      `json:"clientRequestId"`
	RequestFingerprint string      `json:"requestFingerprint"`
	Version            int64       `json:"version"`
	CreatedAt          time.Time   `json:"createdAt"`
	UpdatedAt          time.Time   `json:"updatedAt"`
}
type GrantInput struct {
	ContactID         string      `json:"contactId"`
	OrganisationID    string      `json:"organisationId"`
	PurposeID         string      `json:"purposeId"`
	Channel           string      `json:"channel"`
	ConsentReviewID   string      `json:"consentReviewId"`
	WordingVersion    string      `json:"wordingVersion"`
	SourceType        string      `json:"sourceType"`
	SourceReference   string      `json:"sourceReference"`
	EvidenceObjectKey string      `json:"evidenceObjectKey"`
	EvidenceChecksum  string      `json:"evidenceChecksum"`
	EffectiveFrom     *time.Time  `json:"effectiveFrom"`
	GrantedAt         *time.Time  `json:"grantedAt"`
	ExpiresAt         *time.Time  `json:"expiresAt"`
	Status            GrantStatus `json:"status"`
	CreatedBy         string      `json:"-"`
	ClientRequestID   string      `json:"-"`
}
type WithdrawInput struct {
	ActorID         string `json:"-"`
	Reason          string `json:"reason"`
	SourceReference string `json:"sourceReference"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

type SuppressionScope string

const (
	SuppressionGlobal       SuppressionScope = "GLOBAL"
	SuppressionOrganisation SuppressionScope = "ORGANISATION"
	SuppressionPurpose      SuppressionScope = "PURPOSE"
	SuppressionChannel      SuppressionScope = "CHANNEL"
	SuppressionTemporary    SuppressionScope = "TEMPORARY"
)

type Suppression struct {
	ID                 string           `json:"id"`
	ContactID          string           `json:"contactId,omitempty"`
	MSISDNLookupHMAC   []byte           `json:"-"`
	OrganisationID     string           `json:"organisationId,omitempty"`
	PurposeID          string           `json:"purposeId,omitempty"`
	Channel            string           `json:"channel,omitempty"`
	Scope              SuppressionScope `json:"scope"`
	Reason             string           `json:"reason"`
	EffectiveAt        time.Time        `json:"effectiveAt"`
	ExpiresAt          *time.Time       `json:"expiresAt,omitempty"`
	Active             bool             `json:"active"`
	SourceReference    string           `json:"sourceReference,omitempty"`
	CreatedBy          string           `json:"createdBy"`
	ClientRequestID    string           `json:"clientRequestId"`
	RequestFingerprint string           `json:"requestFingerprint"`
	RevokedBy          string           `json:"revokedBy,omitempty"`
	RevokedAt          *time.Time       `json:"revokedAt,omitempty"`
	RevokeReason       string           `json:"revokeReason,omitempty"`
	Version            int64            `json:"version"`
	CreatedAt          time.Time        `json:"createdAt"`
}
type SuppressionInput struct {
	ContactID        string           `json:"contactId"`
	MSISDNLookupHMAC []byte           `json:"-"`
	OrganisationID   string           `json:"organisationId"`
	PurposeID        string           `json:"purposeId"`
	Channel          string           `json:"channel"`
	Scope            SuppressionScope `json:"scope"`
	Reason           string           `json:"reason"`
	EffectiveAt      *time.Time       `json:"effectiveAt"`
	ExpiresAt        *time.Time       `json:"expiresAt"`
	SourceReference  string           `json:"sourceReference"`
	CreatedBy        string           `json:"-"`
	ClientRequestID  string           `json:"-"`
}
type RevokeSuppressionInput struct {
	ActorID         string `json:"-"`
	Reason          string `json:"reason"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

type ConsentEvent struct {
	ID              string    `json:"id"`
	ContactID       string    `json:"contactId"`
	GrantID         string    `json:"grantId,omitempty"`
	SuppressionID   string    `json:"suppressionId,omitempty"`
	EventType       string    `json:"eventType"`
	ActorID         string    `json:"actorId,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	SourceReference string    `json:"sourceReference,omitempty"`
	OccurredAt      time.Time `json:"occurredAt"`
}

var (
	ErrGrantNotFound        = errors.New("consent grant not found")
	ErrSuppressionNotFound  = errors.New("suppression not found")
	ErrLedgerConflict       = errors.New("consent ledger version conflict")
	ErrLedgerReplayConflict = errors.New("idempotency key reused with different consent evidence")
)

type LedgerRepository interface {
	CreateGrant(context.Context, Grant) (Grant, bool, error)
	WithdrawGrant(context.Context, string, WithdrawInput, time.Time) (Grant, error)
	CreateSuppression(context.Context, Suppression) (Suppression, bool, error)
	RevokeSuppression(context.Context, string, RevokeSuppressionInput, time.Time) (Suppression, error)
	Events(context.Context, string, int) ([]ConsentEvent, error)
}
type LedgerService struct {
	repository LedgerRepository
	clock      func() time.Time
}

func NewLedgerService(r LedgerRepository) *LedgerService {
	return &LedgerService{repository: r, clock: time.Now}
}
func (s *LedgerService) CreateGrant(ctx context.Context, in GrantInput) (Grant, bool, error) {
	if s == nil || s.repository == nil {
		return Grant{}, false, errors.New("consent ledger repository is required")
	}
	v, err := newGrant(in, s.clock().UTC())
	if err != nil {
		return Grant{}, false, err
	}
	return s.repository.CreateGrant(ctx, v)
}
func (s *LedgerService) Withdraw(ctx context.Context, id string, in WithdrawInput) (Grant, error) {
	return s.repository.WithdrawGrant(ctx, id, in, s.clock().UTC())
}
func (s *LedgerService) CreateSuppression(ctx context.Context, in SuppressionInput) (Suppression, bool, error) {
	if s == nil || s.repository == nil {
		return Suppression{}, false, errors.New("consent ledger repository is required")
	}
	v, err := newSuppression(in, s.clock().UTC())
	if err != nil {
		return Suppression{}, false, err
	}
	return s.repository.CreateSuppression(ctx, v)
}
func (s *LedgerService) RevokeSuppression(ctx context.Context, id string, in RevokeSuppressionInput) (Suppression, error) {
	return s.repository.RevokeSuppression(ctx, id, in, s.clock().UTC())
}
func (s *LedgerService) Events(ctx context.Context, contactID string, limit int) ([]ConsentEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	return s.repository.Events(ctx, contactID, limit)
}

func newGrant(in GrantInput, now time.Time) (Grant, error) {
	channel := strings.ToUpper(strings.TrimSpace(in.Channel))
	status := in.Status
	if status == "" {
		status = GrantActive
	}
	if status != GrantActive && status != GrantPendingVerification {
		return Grant{}, errors.New("new grant status must be ACTIVE or PENDING_VERIFICATION")
	}
	if strings.TrimSpace(in.ContactID) == "" || strings.TrimSpace(in.OrganisationID) == "" || strings.TrimSpace(in.PurposeID) == "" || channel == "" || strings.TrimSpace(in.ConsentReviewID) == "" {
		return Grant{}, errors.New("contact, organisation, purpose, channel and consent review are required")
	}
	if strings.TrimSpace(in.WordingVersion) == "" || strings.TrimSpace(in.SourceType) == "" || strings.TrimSpace(in.EvidenceChecksum) == "" || strings.TrimSpace(in.ClientRequestID) == "" || strings.TrimSpace(in.CreatedBy) == "" {
		return Grant{}, errors.New("wording, source, evidence checksum, idempotency key and actor are required")
	}
	granted := now
	if in.GrantedAt != nil {
		granted = in.GrantedAt.UTC()
	}
	effective := granted
	if in.EffectiveFrom != nil {
		effective = in.EffectiveFrom.UTC()
	}
	expires := normaliseLedgerTime(in.ExpiresAt)
	if expires != nil && !expires.After(effective) {
		return Grant{}, errors.New("grant expiry must be after effective time")
	}
	identifier, err := id.New()
	if err != nil {
		return Grant{}, err
	}
	v := Grant{ID: identifier, ContactID: strings.TrimSpace(in.ContactID), OrganisationID: strings.TrimSpace(in.OrganisationID), PurposeID: strings.TrimSpace(in.PurposeID), Channel: channel, ConsentReviewID: strings.TrimSpace(in.ConsentReviewID), WordingVersion: strings.TrimSpace(in.WordingVersion), SourceType: strings.TrimSpace(in.SourceType), SourceReference: strings.TrimSpace(in.SourceReference), EvidenceObjectKey: strings.TrimSpace(in.EvidenceObjectKey), EvidenceChecksum: strings.ToLower(strings.TrimSpace(in.EvidenceChecksum)), EffectiveFrom: effective, GrantedAt: granted, ExpiresAt: expires, Status: status, CreatedBy: strings.TrimSpace(in.CreatedBy), ClientRequestID: strings.TrimSpace(in.ClientRequestID), Version: 1, CreatedAt: now, UpdatedAt: now}
	v.RequestFingerprint = grantFingerprint(v)
	return v, nil
}
func newSuppression(in SuppressionInput, now time.Time) (Suppression, error) {
	if strings.TrimSpace(in.ContactID) == "" && len(in.MSISDNLookupHMAC) == 0 {
		return Suppression{}, errors.New("contact ID or minimised lookup HMAC is required")
	}
	if strings.TrimSpace(in.Reason) == "" || strings.TrimSpace(in.CreatedBy) == "" || strings.TrimSpace(in.ClientRequestID) == "" {
		return Suppression{}, errors.New("suppression reason, actor and idempotency key are required")
	}
	scope := in.Scope
	if scope == "" {
		scope = SuppressionGlobal
	}
	channel := strings.ToUpper(strings.TrimSpace(in.Channel))
	switch scope {
	case SuppressionGlobal:
	case SuppressionOrganisation:
		if strings.TrimSpace(in.OrganisationID) == "" {
			return Suppression{}, errors.New("organisation suppression requires organisation")
		}
	case SuppressionPurpose:
		if strings.TrimSpace(in.PurposeID) == "" {
			return Suppression{}, errors.New("purpose suppression requires purpose")
		}
	case SuppressionChannel:
		if channel == "" {
			return Suppression{}, errors.New("channel suppression requires channel")
		}
	case SuppressionTemporary:
	default:
		return Suppression{}, errors.New("suppression scope is invalid")
	}
	effective := now
	if in.EffectiveAt != nil {
		effective = in.EffectiveAt.UTC()
	}
	expires := normaliseLedgerTime(in.ExpiresAt)
	if expires != nil && !expires.After(effective) {
		return Suppression{}, errors.New("suppression expiry must be after effective time")
	}
	identifier, err := id.New()
	if err != nil {
		return Suppression{}, err
	}
	v := Suppression{ID: identifier, ContactID: strings.TrimSpace(in.ContactID), MSISDNLookupHMAC: append([]byte(nil), in.MSISDNLookupHMAC...), OrganisationID: strings.TrimSpace(in.OrganisationID), PurposeID: strings.TrimSpace(in.PurposeID), Channel: channel, Scope: scope, Reason: strings.TrimSpace(in.Reason), EffectiveAt: effective, ExpiresAt: expires, Active: true, SourceReference: strings.TrimSpace(in.SourceReference), CreatedBy: strings.TrimSpace(in.CreatedBy), ClientRequestID: strings.TrimSpace(in.ClientRequestID), Version: 1, CreatedAt: now}
	v.RequestFingerprint = suppressionFingerprint(v)
	return v, nil
}
func normaliseLedgerTime(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	t := v.UTC()
	return &t
}
func hashParts(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
func grantFingerprint(v Grant) string {
	return hashParts(v.ContactID, v.OrganisationID, v.PurposeID, v.Channel, v.ConsentReviewID, v.WordingVersion, v.SourceType, v.SourceReference, v.EvidenceObjectKey, v.EvidenceChecksum, timeValue(v.ExpiresAt), string(v.Status), v.CreatedBy)
}
func suppressionFingerprint(v Suppression) string {
	return hashParts(v.ContactID, hex.EncodeToString(v.MSISDNLookupHMAC), v.OrganisationID, v.PurposeID, v.Channel, string(v.Scope), v.Reason, timeValue(v.ExpiresAt), v.SourceReference, v.CreatedBy)
}
func timeValue(v *time.Time) string {
	if v == nil {
		return ""
	}
	return v.UTC().Format(time.RFC3339Nano)
}

type MemoryLedgerRepository struct {
	mu           sync.Mutex
	grants       map[string]Grant
	suppressions map[string]Suppression
	byRequest    map[string]string
	events       []ConsentEvent
}

func NewMemoryLedgerRepository() *MemoryLedgerRepository {
	return &MemoryLedgerRepository{grants: map[string]Grant{}, suppressions: map[string]Suppression{}, byRequest: map[string]string{}}
}
func (r *MemoryLedgerRepository) CreateGrant(_ context.Context, v Grant) (Grant, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := "grant\x1f" + v.CreatedBy + "\x1f" + v.ClientRequestID
	if existingID, ok := r.byRequest[key]; ok {
		existing := r.grants[existingID]
		if existing.RequestFingerprint != v.RequestFingerprint {
			return Grant{}, false, ErrLedgerReplayConflict
		}
		return existing, false, nil
	}
	r.grants[v.ID] = v
	r.byRequest[key] = v.ID
	r.appendEvent(v.ContactID, v.ID, "", "GRANT_CREATED", v.CreatedBy, "", v.SourceReference, v.CreatedAt)
	return v, true, nil
}
func (r *MemoryLedgerRepository) WithdrawGrant(_ context.Context, identifier string, in WithdrawInput, now time.Time) (Grant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.grants[identifier]
	if !ok {
		return Grant{}, ErrGrantNotFound
	}
	if in.ExpectedVersion != v.Version {
		return Grant{}, ErrLedgerConflict
	}
	if strings.TrimSpace(in.ActorID) == "" || strings.TrimSpace(in.Reason) == "" {
		return Grant{}, errors.New("withdrawal actor and reason are required")
	}
	if v.Status == GrantWithdrawn {
		return v, nil
	}
	v.Status = GrantWithdrawn
	v.Version++
	v.UpdatedAt = now
	r.grants[identifier] = v
	r.appendEvent(v.ContactID, v.ID, "", "GRANT_WITHDRAWN", in.ActorID, in.Reason, in.SourceReference, now)
	return v, nil
}
func (r *MemoryLedgerRepository) CreateSuppression(_ context.Context, v Suppression) (Suppression, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := "suppression\x1f" + v.CreatedBy + "\x1f" + v.ClientRequestID
	if existingID, ok := r.byRequest[key]; ok {
		existing := r.suppressions[existingID]
		if existing.RequestFingerprint != v.RequestFingerprint {
			return Suppression{}, false, ErrLedgerReplayConflict
		}
		return existing, false, nil
	}
	r.suppressions[v.ID] = v
	r.byRequest[key] = v.ID
	r.appendEvent(v.ContactID, "", v.ID, "SUPPRESSION_CREATED", v.CreatedBy, v.Reason, v.SourceReference, v.CreatedAt)
	return v, true, nil
}
func (r *MemoryLedgerRepository) RevokeSuppression(_ context.Context, identifier string, in RevokeSuppressionInput, now time.Time) (Suppression, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.suppressions[identifier]
	if !ok {
		return Suppression{}, ErrSuppressionNotFound
	}
	if in.ExpectedVersion != v.Version {
		return Suppression{}, ErrLedgerConflict
	}
	if strings.TrimSpace(in.ActorID) == "" || strings.TrimSpace(in.Reason) == "" {
		return Suppression{}, errors.New("revocation actor and reason are required")
	}
	if !v.Active {
		return v, nil
	}
	v.Active = false
	v.RevokedBy = in.ActorID
	v.RevokeReason = strings.TrimSpace(in.Reason)
	v.RevokedAt = &now
	v.Version++
	r.suppressions[identifier] = v
	r.appendEvent(v.ContactID, "", v.ID, "SUPPRESSION_REVOKED", in.ActorID, in.Reason, "", now)
	return v, nil
}
func (r *MemoryLedgerRepository) Events(_ context.Context, contactID string, limit int) ([]ConsentEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ConsentEvent, 0)
	for i := len(r.events) - 1; i >= 0 && len(out) < limit; i-- {
		if contactID == "" || r.events[i].ContactID == contactID {
			out = append(out, r.events[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].OccurredAt.After(out[j].OccurredAt) })
	return out, nil
}
func (r *MemoryLedgerRepository) appendEvent(contact, grant, suppression, eventType, actor, reason, source string, at time.Time) {
	eventID, _ := id.New()
	r.events = append(r.events, ConsentEvent{ID: eventID, ContactID: contact, GrantID: grant, SuppressionID: suppression, EventType: eventType, ActorID: actor, Reason: strings.TrimSpace(reason), SourceReference: strings.TrimSpace(source), OccurredAt: at})
}

func (r *MemoryLedgerRepository) EventPage(_ context.Context, contactID string, limit int, before *time.Time, beforeID string) ([]ConsentEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]ConsentEvent, 0)
	for _, event := range r.events {
		if contactID != "" && event.ContactID != contactID {
			continue
		}
		if before != nil && !(event.OccurredAt.Before(*before) || (event.OccurredAt.Equal(*before) && event.ID < beforeID)) {
			continue
		}
		items = append(items, event)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].OccurredAt.Equal(items[j].OccurredAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].OccurredAt.After(items[j].OccurredAt)
	})
	if limit <= 0 || limit > 1001 {
		limit = 100
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
