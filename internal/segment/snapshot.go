package segment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/shared/id"
)

type Snapshot struct {
	ID                   string               `json:"id"`
	CampaignID           string               `json:"campaignId"`
	SegmentID            string               `json:"segmentId,omitempty"`
	Definition           audiencefilter.Group `json:"definition"`
	DefinitionVersion    int64                `json:"definitionVersion"`
	ConsentPolicyVersion string               `json:"consentPolicyVersion"`
	ConfigurationVersion string               `json:"configurationVersion"`
	EligibleCount        int64                `json:"eligibleCount"`
	SnapshotHash         string               `json:"snapshotHash"`
	CreatedBy            string               `json:"createdBy"`
	CreatedAt            time.Time            `json:"createdAt"`
}

type Builder struct {
	campaignID, segmentID, consentPolicyVersion, configurationVersion, createdBy string
	definition                                                                   audiencefilter.Group
	definitionVersion                                                            int64
	hash                                                                         [32]byte
	count                                                                        int64
	lastContactID                                                                string
}

func NewBuilder(campaignID, segmentID string, definition audiencefilter.Group, definitionVersion int64, consentPolicyVersion, configurationVersion, createdBy string) (*Builder, error) {
	if strings.TrimSpace(campaignID) == "" || strings.TrimSpace(createdBy) == "" || definitionVersion <= 0 || strings.TrimSpace(consentPolicyVersion) == "" || strings.TrimSpace(configurationVersion) == "" {
		return nil, errors.New("campaign, definition version, policy versions and creator are required")
	}
	seedPayload, err := json.Marshal(struct {
		CampaignID           string               `json:"campaignId"`
		SegmentID            string               `json:"segmentId,omitempty"`
		Definition           audiencefilter.Group `json:"definition"`
		DefinitionVersion    int64                `json:"definitionVersion"`
		ConsentPolicyVersion string               `json:"consentPolicyVersion"`
		ConfigurationVersion string               `json:"configurationVersion"`
	}{campaignID, segmentID, definition, definitionVersion, consentPolicyVersion, configurationVersion})
	if err != nil {
		return nil, err
	}
	seed := sha256.Sum256(append([]byte("snapshot-v2\x00"), seedPayload...))
	return &Builder{campaignID: campaignID, segmentID: segmentID, definition: definition, definitionVersion: definitionVersion, consentPolicyVersion: consentPolicyVersion, configurationVersion: configurationVersion, createdBy: createdBy, hash: seed}, nil
}

// RestoreBuilder recreates the deterministic rolling snapshot hash from a durable
// checkpoint. It is used by restart-safe audience materialisation workers and
// rejects malformed or inconsistent checkpoint evidence.
func RestoreBuilder(campaignID, segmentID string, definition audiencefilter.Group, definitionVersion int64, consentPolicyVersion, configurationVersion, createdBy, lastContactID, hash string, count int64) (*Builder, error) {
	builder, err := NewBuilder(campaignID, segmentID, definition, definitionVersion, consentPolicyVersion, configurationVersion, createdBy)
	if err != nil {
		return nil, err
	}
	if count < 0 {
		return nil, errors.New("snapshot checkpoint count cannot be negative")
	}
	if count == 0 {
		if strings.TrimSpace(lastContactID) != "" {
			return nil, errors.New("empty snapshot checkpoint cannot contain a last contact")
		}
		_, seedHash, _ := builder.Checkpoint()
		if strings.TrimSpace(hash) != "" && !strings.EqualFold(strings.TrimSpace(hash), seedHash) {
			return nil, errors.New("snapshot checkpoint seed hash is invalid")
		}
		return builder, nil
	}
	if strings.TrimSpace(lastContactID) == "" {
		return nil, errors.New("non-empty snapshot checkpoint requires a last contact")
	}
	decoded, err := hex.DecodeString(strings.TrimSpace(hash))
	if err != nil || len(decoded) != sha256.Size {
		return nil, errors.New("snapshot checkpoint hash must be a SHA-256 hex digest")
	}
	copy(builder.hash[:], decoded)
	builder.count = count
	builder.lastContactID = strings.TrimSpace(lastContactID)
	return builder, nil
}

// Add must receive contact IDs in strictly increasing order. Database snapshot workers
// therefore use ORDER BY contact_id and may resume from the last committed ID.
func (b *Builder) Add(contactID, eligibilityEvidenceHash string) error {
	contactID = strings.TrimSpace(contactID)
	eligibilityEvidenceHash = strings.TrimSpace(eligibilityEvidenceHash)
	if contactID == "" || eligibilityEvidenceHash == "" {
		return errors.New("contact ID and eligibility evidence hash are required")
	}
	if b.lastContactID != "" && contactID <= b.lastContactID {
		return fmt.Errorf("snapshot members must be strictly ordered: %s after %s", contactID, b.lastContactID)
	}
	leaf := sha256.Sum256([]byte(contactID + "\x1f" + eligibilityEvidenceHash))
	combined := make([]byte, 0, 64)
	combined = append(combined, b.hash[:]...)
	combined = append(combined, leaf[:]...)
	b.hash = sha256.Sum256(combined)
	b.lastContactID = contactID
	b.count++
	return nil
}
func (b *Builder) Checkpoint() (lastContactID, hash string, count int64) {
	return b.lastContactID, hex.EncodeToString(b.hash[:]), b.count
}
func (b *Builder) Finalise(now time.Time) (Snapshot, error) {
	if b.count <= 0 {
		return Snapshot{}, errors.New("snapshot must contain at least one eligible contact")
	}
	identifier, err := id.New()
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{ID: identifier, CampaignID: b.campaignID, SegmentID: b.segmentID, Definition: b.definition, DefinitionVersion: b.definitionVersion, ConsentPolicyVersion: b.consentPolicyVersion, ConfigurationVersion: b.configurationVersion, EligibleCount: b.count, SnapshotHash: hex.EncodeToString(b.hash[:]), CreatedBy: b.createdBy, CreatedAt: now.UTC()}, nil
}

type Repository interface {
	Create(context.Context, Snapshot) error
	Get(context.Context, string) (Snapshot, error)
}

var (
	ErrSnapshotNotFound = errors.New("audience snapshot not found")
	ErrSnapshotConflict = errors.New("audience snapshot idempotency conflict")
)

type MemoryRepository struct {
	mu             sync.RWMutex
	items          map[string]Snapshot
	members        map[string][]Member
	byCampaignHash map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{items: make(map[string]Snapshot), members: make(map[string][]Member), byCampaignHash: make(map[string]string)}
}
func (r *MemoryRepository) Create(_ context.Context, s Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[s.ID]; ok {
		return errors.New("snapshot exists")
	}
	r.items[s.ID] = s
	return nil
}
func (r *MemoryRepository) Get(_ context.Context, id string) (Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.items[id]
	if !ok {
		return Snapshot{}, ErrSnapshotNotFound
	}
	return s, nil
}

type Member struct {
	ContactID               string `json:"contactId"`
	EligibilityEvidenceHash string `json:"eligibilityEvidenceHash"`
}

type CreateInput struct {
	CampaignID           string               `json:"campaignId"`
	SegmentID            string               `json:"segmentId,omitempty"`
	Definition           audiencefilter.Group `json:"definition"`
	DefinitionVersion    int64                `json:"definitionVersion"`
	ConsentPolicyVersion string               `json:"consentPolicyVersion"`
	ConfigurationVersion string               `json:"configurationVersion"`
	CreatedBy            string               `json:"-"`
	Members              []Member             `json:"members"`
}

type Store interface {
	Repository
	CreateWithMembers(context.Context, Snapshot, []Member) error
	EnsureWithMembers(context.Context, Snapshot, []Member) (Snapshot, bool, error)
	Members(context.Context, string, string, int) ([]Member, error)
}

type Service struct {
	store Store
	clock func() time.Time
}

func NewService(store Store) *Service { return &Service{store: store, clock: time.Now} }

func (s *Service) Create(ctx context.Context, input CreateInput) (Snapshot, error) {
	if s == nil || s.store == nil {
		return Snapshot{}, errors.New("snapshot store is required")
	}
	if len(input.Members) == 0 {
		return Snapshot{}, errors.New("snapshot requires eligible members")
	}
	if len(input.Members) > 100_000 {
		return Snapshot{}, errors.New("single snapshot materialisation batch exceeds 100000 members")
	}
	builder, err := NewBuilder(input.CampaignID, input.SegmentID, input.Definition, input.DefinitionVersion, input.ConsentPolicyVersion, input.ConfigurationVersion, input.CreatedBy)
	if err != nil {
		return Snapshot{}, err
	}
	members := append([]Member(nil), input.Members...)
	sort.Slice(members, func(i, j int) bool { return members[i].ContactID < members[j].ContactID })
	for i, member := range members {
		if i > 0 && member.ContactID == members[i-1].ContactID {
			return Snapshot{}, fmt.Errorf("duplicate snapshot contact %s", member.ContactID)
		}
		if err := builder.Add(member.ContactID, member.EligibilityEvidenceHash); err != nil {
			return Snapshot{}, err
		}
	}
	snapshot, err := builder.Finalise(s.clock())
	if err != nil {
		return Snapshot{}, err
	}
	stored, _, err := s.store.EnsureWithMembers(ctx, snapshot, members)
	if err != nil {
		return Snapshot{}, err
	}
	return stored, nil
}

func (s *Service) Get(ctx context.Context, identifier string) (Snapshot, error) {
	if strings.TrimSpace(identifier) == "" {
		return Snapshot{}, ErrSnapshotNotFound
	}
	return s.store.Get(ctx, identifier)
}

func (s *Service) Members(ctx context.Context, identifier, afterContactID string, limit int) ([]Member, error) {
	if limit <= 0 || limit > 10_000 {
		limit = 1_000
	}
	return s.store.Members(ctx, identifier, afterContactID, limit)
}

func (r *MemoryRepository) CreateWithMembers(ctx context.Context, snapshot Snapshot, members []Member) error {
	_, _, err := r.EnsureWithMembers(ctx, snapshot, members)
	return err
}

func validateSnapshotIntegrity(snapshot Snapshot, members []Member) error {
	if strings.TrimSpace(snapshot.ID) == "" || strings.TrimSpace(snapshot.SnapshotHash) == "" {
		return errors.New("snapshot identity and hash are required")
	}
	if int64(len(members)) != snapshot.EligibleCount {
		return errors.New("snapshot member count does not match eligible count")
	}
	builder, err := NewBuilder(snapshot.CampaignID, snapshot.SegmentID, snapshot.Definition, snapshot.DefinitionVersion, snapshot.ConsentPolicyVersion, snapshot.ConfigurationVersion, snapshot.CreatedBy)
	if err != nil {
		return err
	}
	for _, member := range members {
		if err := builder.Add(member.ContactID, member.EligibilityEvidenceHash); err != nil {
			return err
		}
	}
	_, computedHash, count := builder.Checkpoint()
	if count != snapshot.EligibleCount || computedHash != snapshot.SnapshotHash {
		return ErrSnapshotConflict
	}
	return nil
}

func (r *MemoryRepository) EnsureWithMembers(_ context.Context, snapshot Snapshot, members []Member) (Snapshot, bool, error) {
	if err := validateSnapshotIntegrity(snapshot, members); err != nil {
		return Snapshot{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := snapshot.CampaignID + "\x1f" + snapshot.SnapshotHash
	if existingID, exists := r.byCampaignHash[key]; exists {
		existing := r.items[existingID]
		if !sameSnapshotEvidence(existing, snapshot) || !reflect.DeepEqual(r.members[existingID], members) {
			return Snapshot{}, false, ErrSnapshotConflict
		}
		return existing, false, nil
	}
	if existing, exists := r.items[snapshot.ID]; exists {
		if !sameSnapshotEvidence(existing, snapshot) || !reflect.DeepEqual(r.members[snapshot.ID], members) {
			return Snapshot{}, false, ErrSnapshotConflict
		}
		return existing, false, nil
	}
	r.items[snapshot.ID] = snapshot
	r.members[snapshot.ID] = append([]Member(nil), members...)
	r.byCampaignHash[key] = snapshot.ID
	return snapshot, true, nil
}

func sameSnapshotEvidence(left, right Snapshot) bool {
	return left.CampaignID == right.CampaignID && left.SegmentID == right.SegmentID &&
		left.DefinitionVersion == right.DefinitionVersion &&
		left.ConsentPolicyVersion == right.ConsentPolicyVersion &&
		left.ConfigurationVersion == right.ConfigurationVersion &&
		left.EligibleCount == right.EligibleCount && left.SnapshotHash == right.SnapshotHash &&
		reflect.DeepEqual(left.Definition, right.Definition)
}

func (r *MemoryRepository) Members(_ context.Context, snapshotID, afterContactID string, limit int) ([]Member, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, exists := r.items[snapshotID]; !exists {
		return nil, ErrSnapshotNotFound
	}
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	items := r.members[snapshotID]
	start := 0
	if afterContactID != "" {
		start = sort.Search(len(items), func(i int) bool { return items[i].ContactID > afterContactID })
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	return append([]Member(nil), items[start:end]...), nil
}

type SnapshotOverlap struct {
	LeftSnapshotID  string  `json:"leftSnapshotId"`
	RightSnapshotID string  `json:"rightSnapshotId"`
	LeftCount       int64   `json:"leftCount"`
	RightCount      int64   `json:"rightCount"`
	Intersection    int64   `json:"intersection"`
	OnlyLeft        int64   `json:"onlyLeft"`
	OnlyRight       int64   `json:"onlyRight"`
	Union           int64   `json:"union"`
	OverlapPercent  float64 `json:"overlapPercent"`
}

type OverlapStore interface {
	Overlap(context.Context, string, string) (SnapshotOverlap, error)
}

func (s *Service) Overlap(ctx context.Context, leftID, rightID string) (SnapshotOverlap, error) {
	if s == nil || s.store == nil {
		return SnapshotOverlap{}, errors.New("snapshot store is required")
	}
	leftID, rightID = strings.TrimSpace(leftID), strings.TrimSpace(rightID)
	if leftID == "" || rightID == "" || leftID == rightID {
		return SnapshotOverlap{}, errors.New("two different snapshot IDs are required")
	}
	provider, ok := s.store.(OverlapStore)
	if !ok {
		return SnapshotOverlap{}, errors.New("snapshot overlap analysis is unavailable")
	}
	return provider.Overlap(ctx, leftID, rightID)
}

func (r *MemoryRepository) Overlap(_ context.Context, leftID, rightID string) (SnapshotOverlap, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	left, leftOK := r.items[leftID]
	right, rightOK := r.items[rightID]
	if !leftOK || !rightOK {
		return SnapshotOverlap{}, ErrSnapshotNotFound
	}
	leftSet := map[string]struct{}{}
	rightSet := map[string]struct{}{}
	for _, member := range r.members[leftID] {
		leftSet[member.ContactID] = struct{}{}
	}
	for _, member := range r.members[rightID] {
		rightSet[member.ContactID] = struct{}{}
	}
	var intersection int64
	for id := range leftSet {
		if _, ok := rightSet[id]; ok {
			intersection++
		}
	}
	union := int64(len(leftSet)+len(rightSet)) - intersection
	percent := 0.0
	if union > 0 {
		percent = float64(intersection) * 100 / float64(union)
	}
	return SnapshotOverlap{LeftSnapshotID: left.ID, RightSnapshotID: right.ID, LeftCount: int64(len(leftSet)), RightCount: int64(len(rightSet)), Intersection: intersection, OnlyLeft: int64(len(leftSet)) - intersection, OnlyRight: int64(len(rightSet)) - intersection, Union: union, OverlapPercent: percent}, nil
}
