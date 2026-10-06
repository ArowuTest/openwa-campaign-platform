package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/shared/id"
)

var (
	ErrEntitlementExceeded = errors.New("campaign entitlement exceeded")
	ErrReleaseConflict     = errors.New("release batch conflicts with existing obligation")
)

type Member struct {
	ContactID               string
	EligibilityEvidenceHash string
}
type EligibilityDecision struct {
	Eligible        bool
	ExclusionReason string
}
type EligibilityChecker interface {
	Check(context.Context, string, string, string, string, time.Time) (EligibilityDecision, error)
}
type EligibilityFunc func(context.Context, string, string, string, string, time.Time) (EligibilityDecision, error)

func (f EligibilityFunc) Check(ctx context.Context, contactID, organisationID, purposeID, channel string, asOf time.Time) (EligibilityDecision, error) {
	return f(ctx, contactID, organisationID, purposeID, channel, asOf)
}

type Command struct {
	CampaignID              string
	ExpectedCampaignVersion int64
	SnapshotID              string
	MessageVersionID        string
	OrganisationID          string
	PurposeID               string
	Channel                 string
	MaximumUniqueRecipients int64
	Members                 []Member
	ShardSize               int
	AsOf                    time.Time
}
type Outbox struct {
	ID          string
	DedupKey    string
	EventType   string
	AggregateID string
	Payload     json.RawMessage
	CreatedAt   time.Time
}
type Result struct {
	Authorised       int
	Excluded         int
	Existing         int
	OutboxCreated    int
	ExclusionReasons map[string]int
}

type AtomicStore interface {
	Authorise(context.Context, Command, EligibilityChecker) (Result, error)
}

// EvidenceStore exposes bounded, error-returning diagnostic reads over the
// authoritative recipient ledger and transactional outbox. These operations
// are intentionally separate from AtomicStore so release execution cannot
// accidentally depend on loading an entire campaign or outbox into memory.
type EvidenceStore interface {
	ListRecipients(context.Context, string, string, string, int) ([]delivery.Recipient, error)
	ListOutbox(context.Context, time.Time, string, int) ([]Outbox, error)
}

type MemoryStore struct {
	mu         sync.Mutex
	recipients map[string]delivery.Recipient
	byNatural  map[string]string
	outbox     map[string]Outbox
	exclusions map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{recipients: make(map[string]delivery.Recipient), byNatural: make(map[string]string), outbox: make(map[string]Outbox), exclusions: make(map[string]string)}
}
func (s *MemoryStore) Authorise(ctx context.Context, cmd Command, checker EligibilityChecker) (Result, error) {
	if err := validateCommand(cmd); err != nil {
		return Result{}, err
	}
	if checker == nil {
		return Result{}, errors.New("eligibility checker is required")
	}
	members := append([]Member(nil), cmd.Members...)
	sort.Slice(members, func(i, j int) bool { return members[i].ContactID < members[j].ContactID })
	seen := map[string]struct{}{}
	decisions := make([]EligibilityDecision, len(members))
	eligible := 0
	for i, m := range members {
		if strings.TrimSpace(m.ContactID) == "" || strings.TrimSpace(m.EligibilityEvidenceHash) == "" {
			return Result{}, errors.New("contact ID and eligibility evidence hash are required")
		}
		if _, ok := seen[m.ContactID]; ok {
			return Result{}, fmt.Errorf("duplicate contact %s in release batch", m.ContactID)
		}
		seen[m.ContactID] = struct{}{}
		decision, err := checker.Check(ctx, m.ContactID, cmd.OrganisationID, cmd.PurposeID, cmd.Channel, cmd.AsOf)
		if err != nil {
			return Result{}, err
		}
		decisions[i] = decision
		if decision.Eligible {
			eligible++
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Enforce the entitlement cumulatively across resumed/batched releases, not
	// merely against the current batch. Existing natural keys are excluded from
	// the prospective count so replay remains idempotent.
	existingCampaign := int64(0)
	newEligible := int64(0)
	for _, recipient := range s.recipients {
		if recipient.CampaignID == cmd.CampaignID {
			existingCampaign++
		}
	}
	for i, member := range members {
		if !decisions[i].Eligible {
			continue
		}
		natural := cmd.CampaignID + "\x1f" + member.ContactID + "\x1f" + cmd.MessageVersionID
		if _, exists := s.byNatural[natural]; !exists {
			newEligible++
		}
	}
	if existingCampaign+newEligible > cmd.MaximumUniqueRecipients {
		return Result{}, ErrEntitlementExceeded
	}
	result := Result{ExclusionReasons: make(map[string]int)}
	now := cmd.AsOf.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	for i, m := range members {
		decision := decisions[i]
		if !decision.Eligible {
			exclusionKey := cmd.CampaignID + "\x1f" + cmd.SnapshotID + "\x1f" + m.ContactID
			if _, exists := s.exclusions[exclusionKey]; exists {
				result.Existing++
				continue
			}
			reason := strings.TrimSpace(decision.ExclusionReason)
			if reason == "" {
				reason = "INELIGIBLE_FINAL_CHECK"
			}
			s.exclusions[exclusionKey] = reason
			result.Excluded++
			result.ExclusionReasons[reason]++
			continue
		}
		natural := cmd.CampaignID + "\x1f" + m.ContactID + "\x1f" + cmd.MessageVersionID
		if existingID, ok := s.byNatural[natural]; ok {
			existing := s.recipients[existingID]
			if existing.CampaignID != cmd.CampaignID || existing.ContactID != m.ContactID || existing.MessageVersionID != cmd.MessageVersionID {
				return Result{}, ErrReleaseConflict
			}
			result.Existing++
			continue
		}
		key, err := delivery.NewIdempotencyKey(cmd.CampaignID, m.ContactID, cmd.MessageVersionID)
		if err != nil {
			return Result{}, err
		}
		recipientID, err := id.New()
		if err != nil {
			return Result{}, err
		}
		recipient := delivery.Recipient{ID: recipientID, CampaignID: cmd.CampaignID, ContactID: m.ContactID, MessageVersionID: cmd.MessageVersionID, IdempotencyKey: key, Status: delivery.StatusAuthorised, UpdatedAt: now}
		outboxID, err := id.New()
		if err != nil {
			return Result{}, err
		}
		payload, err := json.Marshal(map[string]any{"campaignRecipientId": recipientID, "campaignId": cmd.CampaignID, "snapshotId": cmd.SnapshotID, "shard": shardFor(m.ContactID, cmd.MaximumUniqueRecipients, cmd.ShardSize), "eligibilityEvidenceHash": m.EligibilityEvidenceHash})
		if err != nil {
			return Result{}, err
		}
		out := Outbox{ID: outboxID, DedupKey: "dispatch:" + key, EventType: "CAMPAIGN_RECIPIENT_AUTHORISED", AggregateID: recipientID, Payload: payload, CreatedAt: now}
		s.recipients[recipientID] = recipient
		s.byNatural[natural] = recipientID
		s.outbox[out.DedupKey] = out
		result.Authorised++
		result.OutboxCreated++
	}
	return result, nil
}
func (s *MemoryStore) ListRecipients(_ context.Context, campaignID, afterContactID, afterID string, limit int) ([]delivery.Recipient, error) {
	if err := validateRecipientCursor(afterContactID, afterID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 5_000 {
		limit = 1_000
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []delivery.Recipient{}
	for _, v := range s.recipients {
		if campaignID != "" && v.CampaignID != campaignID {
			continue
		}
		if afterContactID != "" && (v.ContactID < afterContactID || (v.ContactID == afterContactID && v.ID <= afterID)) {
			continue
		}
		if campaignID == "" || v.CampaignID == campaignID {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ContactID == out[j].ContactID {
			return out[i].ID < out[j].ID
		}
		return out[i].ContactID < out[j].ContactID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (s *MemoryStore) ListOutbox(_ context.Context, afterCreatedAt time.Time, afterID string, limit int) ([]Outbox, error) {
	if err := validateOutboxCursor(afterCreatedAt, afterID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 5_000 {
		limit = 1_000
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Outbox, 0, len(s.outbox))
	for _, v := range s.outbox {
		if !afterCreatedAt.IsZero() && (v.CreatedAt.Before(afterCreatedAt) || (v.CreatedAt.Equal(afterCreatedAt) && v.ID <= afterID)) {
			continue
		}
		v.Payload = append([]byte(nil), v.Payload...)
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func validateRecipientCursor(afterContactID, afterID string) error {
	hasContact := strings.TrimSpace(afterContactID) != ""
	hasID := strings.TrimSpace(afterID) != ""
	if hasContact != hasID {
		return errors.New("recipient cursor requires both contact ID and recipient ID")
	}
	return nil
}

func validateOutboxCursor(afterCreatedAt time.Time, afterID string) error {
	hasTime := !afterCreatedAt.IsZero()
	hasID := strings.TrimSpace(afterID) != ""
	if hasTime != hasID {
		return errors.New("outbox cursor requires both creation time and outbox ID")
	}
	return nil
}

func validateCommand(c Command) error {
	if c.CampaignID == "" || c.SnapshotID == "" || c.MessageVersionID == "" || c.OrganisationID == "" || c.PurposeID == "" || c.Channel == "" {
		return errors.New("campaign, snapshot, message, organisation, purpose and channel are required")
	}
	if c.MaximumUniqueRecipients <= 0 {
		return errors.New("positive campaign entitlement is required")
	}
	if c.ShardSize <= 0 {
		c.ShardSize = 10000
	}
	if c.AsOf.IsZero() {
		return errors.New("release evaluation time is required")
	}
	return nil
}
func shardFor(contactID string, maximumRecipients int64, targetShardSize int) int {
	if targetShardSize <= 0 {
		targetShardSize = 10000
	}
	shardCount := int((maximumRecipients + int64(targetShardSize) - 1) / int64(targetShardSize))
	if shardCount < 1 {
		shardCount = 1
	}
	sum := sha256.Sum256([]byte(contactID))
	value := int(sum[0])<<24 | int(sum[1])<<16 | int(sum[2])<<8 | int(sum[3])
	if value < 0 {
		value = -value
	}
	return value % shardCount
}
func HashEvidence(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x1f")))
	return hex.EncodeToString(sum[:])
}
