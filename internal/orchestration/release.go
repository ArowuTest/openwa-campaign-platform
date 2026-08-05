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
	Recipients(context.Context, string) []delivery.Recipient
	Outbox(context.Context) []Outbox
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
		key, _ := delivery.NewIdempotencyKey(cmd.CampaignID, m.ContactID, cmd.MessageVersionID)
		recipientID, err := id.New()
		if err != nil {
			return Result{}, err
		}
		recipient := delivery.Recipient{ID: recipientID, CampaignID: cmd.CampaignID, ContactID: m.ContactID, MessageVersionID: cmd.MessageVersionID, IdempotencyKey: key, Status: delivery.StatusAuthorised, UpdatedAt: now}
		outboxID, err := id.New()
		if err != nil {
			return Result{}, err
		}
		payload, _ := json.Marshal(map[string]any{"campaignRecipientId": recipientID, "campaignId": cmd.CampaignID, "snapshotId": cmd.SnapshotID, "shard": shardFor(m.ContactID, cmd.ShardSize), "eligibilityEvidenceHash": m.EligibilityEvidenceHash})
		out := Outbox{ID: outboxID, DedupKey: "dispatch:" + key, EventType: "CAMPAIGN_RECIPIENT_AUTHORISED", AggregateID: recipientID, Payload: payload, CreatedAt: now}
		s.recipients[recipientID] = recipient
		s.byNatural[natural] = recipientID
		s.outbox[out.DedupKey] = out
		result.Authorised++
		result.OutboxCreated++
	}
	return result, nil
}
func (s *MemoryStore) Recipients(_ context.Context, campaignID string) []delivery.Recipient {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []delivery.Recipient{}
	for _, v := range s.recipients {
		if campaignID == "" || v.CampaignID == campaignID {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ContactID < out[j].ContactID })
	return out
}
func (s *MemoryStore) Outbox(_ context.Context) []Outbox {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Outbox, 0, len(s.outbox))
	for _, v := range s.outbox {
		v.Payload = append([]byte(nil), v.Payload...)
		out = append(out, v)
	}
	return out
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
func shardFor(contactID string, size int) int {
	if size <= 0 {
		size = 10000
	}
	sum := sha256.Sum256([]byte(contactID))
	value := int(sum[0])<<8 | int(sum[1])
	return value % size
}
func HashEvidence(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x1f")))
	return hex.EncodeToString(sum[:])
}
