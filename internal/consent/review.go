package consent

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/organisation"
	"campaign-platform/internal/shared/id"
)

type ReviewStatus string

const (
	StatusDraft      ReviewStatus = "DRAFT"
	StatusPending    ReviewStatus = "PENDING_REVIEW"
	StatusApproved   ReviewStatus = "APPROVED"
	StatusRejected   ReviewStatus = "REJECTED"
	StatusExpired    ReviewStatus = "EXPIRED"
	StatusSuspended  ReviewStatus = "SUSPENDED"
	StatusRevoked    ReviewStatus = "REVOKED"
	StatusSuperseded ReviewStatus = "SUPERSEDED"
)

type ReviewScope string

const (
	ReviewScopeOrganisation ReviewScope = "ORGANISATION"
	ReviewScopeSource       ReviewScope = "SOURCE"
	ReviewScopeCampaign     ReviewScope = "CAMPAIGN"
)

type ReviewOutcome string

const (
	OutcomePending                  ReviewOutcome = "PENDING"
	OutcomeApproved                 ReviewOutcome = "APPROVED"
	OutcomeApprovedWithRestrictions ReviewOutcome = "APPROVED_WITH_RESTRICTIONS"
	OutcomeRejected                 ReviewOutcome = "REJECTED"
)

type ExternalEvidenceReference struct {
	Reference      string `json:"reference"`
	SHA256Checksum string `json:"sha256Checksum"`
}

var (
	sha256Pattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	potentialEmailPII = regexp.MustCompile(`(?i)\b[[:alnum:]._%+-]+@[[:alnum:].-]+\.[a-z]{2,}\b`)
	potentialPhonePII = regexp.MustCompile(`(?:\+?\d[\s().-]*){7,}`)
)

type Review struct {
	ID                            string                      `json:"id"`
	OrganisationID                string                      `json:"organisationId"`
	Scope                         ReviewScope                 `json:"scope"`
	CampaignID                    string                      `json:"campaignId,omitempty"`
	SourceSystem                  string                      `json:"sourceSystem,omitempty"`
	Name                          string                      `json:"name"`
	PurposeDescription            string                      `json:"purposeDescription"`
	PurposeCode                   string                      `json:"purposeCode,omitempty"`
	Channel                       string                      `json:"channel"`
	ConsentSource                 string                      `json:"consentSource"`
	CollectionMethod              string                      `json:"collectionMethod,omitempty"`
	CollectionPeriodFrom          *time.Time                  `json:"collectionPeriodFrom,omitempty"`
	CollectionPeriodTo            *time.Time                  `json:"collectionPeriodTo,omitempty"`
	ControllerRole                string                      `json:"controllerRole,omitempty"`
	WordingVersion                string                      `json:"wordingVersion"`
	ExactConsentWording           string                      `json:"exactConsentWording,omitempty"`
	PrivacyNoticeVersion          string                      `json:"privacyNoticeVersion,omitempty"`
	EvidenceAssetIDs              []string                    `json:"evidenceAssetIds"`
	ExternalEvidenceReferences    []ExternalEvidenceReference `json:"externalEvidenceReferences,omitempty"`
	EvidenceObjectKeys            []string                    `json:"evidenceObjectKeys,omitempty"`
	PrivacyNoticeReviewed         bool                        `json:"privacyNoticeReviewed"`
	OptOutProcessReviewed         bool                        `json:"optOutProcessReviewed"`
	SampleRecordsReviewed         bool                        `json:"sampleRecordsReviewed"`
	SampleReviewNotes             string                      `json:"sampleReviewNotes,omitempty"`
	PermittedCountries            []string                    `json:"permittedCountries"`
	PermittedMessageCategory      string                      `json:"permittedMessageCategory"`
	PermittedPartnerOrganisations []string                    `json:"permittedPartnerOrganisations"`
	Status                        ReviewStatus                `json:"status"`
	Outcome                       ReviewOutcome               `json:"outcome"`
	Restrictions                  string                      `json:"restrictions,omitempty"`
	CreatedBy                     string                      `json:"createdBy,omitempty"`
	SubmittedBy                   string                      `json:"submittedBy,omitempty"`
	ReviewedBy                    string                      `json:"reviewedBy,omitempty"`
	ReviewedAt                    *time.Time                  `json:"reviewedAt,omitempty"`
	ExpiresAt                     *time.Time                  `json:"expiresAt,omitempty"`
	NextReviewAt                  *time.Time                  `json:"nextReviewAt,omitempty"`
	ParentReviewID                string                      `json:"parentReviewId,omitempty"`
	SupersededByID                string                      `json:"supersededById,omitempty"`
	RevokedBy                     string                      `json:"revokedBy,omitempty"`
	RevokedAt                     *time.Time                  `json:"revokedAt,omitempty"`
	RevokeReason                  string                      `json:"revokeReason,omitempty"`
	LastTransitionReason          string                      `json:"-"`
	CreatedAt                     time.Time                   `json:"createdAt"`
	UpdatedAt                     time.Time                   `json:"updatedAt"`
	Version                       int64                       `json:"version"`
}
type CreateInput struct {
	OrganisationID                string                      `json:"organisationId"`
	Scope                         ReviewScope                 `json:"scope"`
	CampaignID                    string                      `json:"campaignId"`
	SourceSystem                  string                      `json:"sourceSystem"`
	Name                          string                      `json:"name"`
	PurposeDescription            string                      `json:"purposeDescription"`
	PurposeCode                   string                      `json:"purposeCode"`
	Channel                       string                      `json:"channel"`
	ConsentSource                 string                      `json:"consentSource"`
	CollectionMethod              string                      `json:"collectionMethod"`
	CollectionPeriodFrom          *time.Time                  `json:"collectionPeriodFrom"`
	CollectionPeriodTo            *time.Time                  `json:"collectionPeriodTo"`
	ControllerRole                string                      `json:"controllerRole"`
	WordingVersion                string                      `json:"wordingVersion"`
	ExactConsentWording           string                      `json:"exactConsentWording"`
	PrivacyNoticeVersion          string                      `json:"privacyNoticeVersion"`
	EvidenceAssetIDs              []string                    `json:"evidenceAssetIds"`
	ExternalEvidenceReferences    []ExternalEvidenceReference `json:"externalEvidenceReferences"`
	PrivacyNoticeReviewed         bool                        `json:"privacyNoticeReviewed"`
	OptOutProcessReviewed         bool                        `json:"optOutProcessReviewed"`
	SampleRecordsReviewed         bool                        `json:"sampleRecordsReviewed"`
	SampleReviewNotes             string                      `json:"sampleReviewNotes"`
	PermittedCountries            []string                    `json:"permittedCountries"`
	PermittedMessageCategory      string                      `json:"permittedMessageCategory"`
	PermittedPartnerOrganisations []string                    `json:"permittedPartnerOrganisations"`
	Restrictions                  string                      `json:"restrictions"`
	CreatedBy                     string                      `json:"-"`
	ParentReviewID                string                      `json:"parentReviewId,omitempty"`
	RevisionReason                string                      `json:"-"`
}
type SubmitInput struct {
	ActorID         string `json:"-"`
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason"`
}
type DecisionInput struct {
	ReviewerID               string       `json:"-"`
	Decision                 ReviewStatus `json:"decision"`
	ApprovedWithRestrictions bool         `json:"approvedWithRestrictions"`
	ExpiresAt                *time.Time   `json:"expiresAt"`
	NextReviewAt             *time.Time   `json:"nextReviewAt"`
	Reason                   string       `json:"reason"`
	ExpectedVersion          int64        `json:"expectedVersion"`
}
type RevokeInput struct {
	ActorID         string `json:"-"`
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason"`
}
type EvidenceAsset struct {
	ID        string
	ObjectKey string
}
type EvidenceResolver interface {
	ResolveConsentEvidence(context.Context, string) (EvidenceAsset, error)
}

func NewReview(input CreateInput, now time.Time) (Review, error) {
	input.OrganisationID = strings.TrimSpace(input.OrganisationID)
	input.Name = strings.TrimSpace(input.Name)
	input.Channel = strings.ToUpper(strings.TrimSpace(input.Channel))
	input.CreatedBy = strings.TrimSpace(input.CreatedBy)
	if input.OrganisationID == "" || input.Name == "" || input.CreatedBy == "" {
		return Review{}, errors.New("organisation, review name and creator are required")
	}
	if input.Channel != "WHATSAPP" {
		return Review{}, errors.New("initial release supports WHATSAPP consent reviews only")
	}
	scope := input.Scope
	if scope == "" {
		scope = ReviewScopeOrganisation
	}
	switch scope {
	case ReviewScopeOrganisation:
		if strings.TrimSpace(input.CampaignID) != "" {
			return Review{}, errors.New("organisation review cannot reference a campaign")
		}
	case ReviewScopeSource:
		if strings.TrimSpace(input.SourceSystem) == "" {
			return Review{}, errors.New("source review requires sourceSystem")
		}
	case ReviewScopeCampaign:
		if strings.TrimSpace(input.CampaignID) == "" {
			return Review{}, errors.New("campaign review requires campaignId")
		}
	default:
		return Review{}, errors.New("invalid consent-review scope")
	}
	if input.CollectionPeriodFrom != nil && input.CollectionPeriodTo != nil && input.CollectionPeriodTo.Before(*input.CollectionPeriodFrom) {
		return Review{}, errors.New("collection period end precedes start")
	}
	externalEvidence, err := normalizeExternalEvidence(input.ExternalEvidenceReferences)
	if err != nil {
		return Review{}, err
	}
	sampleNotes, err := normalizeSampleReviewNotes(input.SampleReviewNotes)
	if err != nil {
		return Review{}, err
	}
	identifier, err := id.New()
	if err != nil {
		return Review{}, err
	}
	now = now.UTC()
	return Review{ID: identifier, OrganisationID: input.OrganisationID, Scope: scope, CampaignID: strings.TrimSpace(input.CampaignID), SourceSystem: strings.TrimSpace(input.SourceSystem), Name: input.Name, PurposeDescription: strings.TrimSpace(input.PurposeDescription), PurposeCode: strings.TrimSpace(input.PurposeCode), Channel: "WHATSAPP", ConsentSource: strings.TrimSpace(input.ConsentSource), CollectionMethod: strings.TrimSpace(input.CollectionMethod), CollectionPeriodFrom: utcPtr(input.CollectionPeriodFrom), CollectionPeriodTo: utcPtr(input.CollectionPeriodTo), ControllerRole: strings.TrimSpace(input.ControllerRole), WordingVersion: strings.TrimSpace(input.WordingVersion), ExactConsentWording: strings.TrimSpace(input.ExactConsentWording), PrivacyNoticeVersion: strings.TrimSpace(input.PrivacyNoticeVersion), EvidenceAssetIDs: uniqueValues(input.EvidenceAssetIDs), ExternalEvidenceReferences: externalEvidence, PrivacyNoticeReviewed: input.PrivacyNoticeReviewed, OptOutProcessReviewed: input.OptOutProcessReviewed, SampleRecordsReviewed: input.SampleRecordsReviewed, SampleReviewNotes: sampleNotes, PermittedCountries: upperValues(input.PermittedCountries), PermittedMessageCategory: strings.TrimSpace(input.PermittedMessageCategory), PermittedPartnerOrganisations: uniqueValues(input.PermittedPartnerOrganisations), Status: StatusDraft, Outcome: OutcomePending, Restrictions: strings.TrimSpace(input.Restrictions), CreatedBy: input.CreatedBy, ParentReviewID: strings.TrimSpace(input.ParentReviewID), CreatedAt: now, UpdatedAt: now, Version: 1}, nil
}
func (r Review) ValidateForSubmission() error {
	if r.Status != StatusDraft {
		return errors.New("only draft reviews may be submitted")
	}
	required := []string{r.PurposeDescription, r.PurposeCode, r.ConsentSource, r.CollectionMethod, r.ControllerRole, r.WordingVersion, r.ExactConsentWording, r.PrivacyNoticeVersion, r.PermittedMessageCategory}
	for _, v := range required {
		if strings.TrimSpace(v) == "" {
			return errors.New("consent review is missing required provenance, wording, purpose or controller evidence")
		}
	}
	if len(r.EvidenceAssetIDs) == 0 && len(r.ExternalEvidenceReferences) == 0 {
		return errors.New("at least one clean evidence asset or checksummed external evidence reference is required")
	}
	if r.SampleRecordsReviewed && strings.TrimSpace(r.SampleReviewNotes) == "" {
		return errors.New("sample-record review requires minimised review notes")
	}
	if len(r.PermittedCountries) == 0 {
		return errors.New("at least one permitted country is required")
	}
	return nil
}
func (r Review) Submit(in SubmitInput, now time.Time) (Review, error) {
	if err := r.ValidateForSubmission(); err != nil {
		return Review{}, err
	}
	if in.ExpectedVersion != r.Version || strings.TrimSpace(in.ActorID) == "" || len(strings.TrimSpace(in.Reason)) < 5 {
		return Review{}, ErrConflict
	}
	r.Status = StatusPending
	r.SubmittedBy = strings.TrimSpace(in.ActorID)
	r.LastTransitionReason = strings.TrimSpace(in.Reason)
	r.UpdatedAt = now.UTC()
	r.Version++
	return r, nil
}
func (r Review) Decide(in DecisionInput, now time.Time) (Review, error) {
	if r.Status != StatusPending {
		return Review{}, errors.New("only pending reviews can be decided")
	}
	if in.Decision != StatusApproved && in.Decision != StatusRejected {
		return Review{}, errors.New("decision must be APPROVED or REJECTED")
	}
	if strings.TrimSpace(in.ReviewerID) == "" || in.ReviewerID == r.CreatedBy || in.ReviewerID == r.SubmittedBy {
		return Review{}, errors.New("decision requires an independent reviewer")
	}
	if in.ExpectedVersion != r.Version {
		return Review{}, ErrConflict
	}
	if len(strings.TrimSpace(in.Reason)) < 5 {
		return Review{}, errors.New("a meaningful decision reason is required")
	}
	if in.Decision == StatusApproved {
		if !r.PrivacyNoticeReviewed || !r.OptOutProcessReviewed || !r.SampleRecordsReviewed {
			return Review{}, errors.New("all mandatory review checks must be completed before approval")
		}
		if in.ExpiresAt == nil || !in.ExpiresAt.After(now) {
			return Review{}, errors.New("approved review requires a future expiry date")
		}
		if in.ApprovedWithRestrictions && strings.TrimSpace(r.Restrictions) == "" {
			return Review{}, errors.New("approved-with-restrictions requires explicit restrictions")
		}
	}
	decided := now.UTC()
	r.Status = in.Decision
	r.Outcome = OutcomeRejected
	if in.Decision == StatusApproved {
		r.Outcome = OutcomeApproved
		if in.ApprovedWithRestrictions {
			r.Outcome = OutcomeApprovedWithRestrictions
		}
	}
	r.ReviewedBy = strings.TrimSpace(in.ReviewerID)
	r.LastTransitionReason = strings.TrimSpace(in.Reason)
	r.ReviewedAt = &decided
	r.ExpiresAt = utcPtr(in.ExpiresAt)
	r.NextReviewAt = utcPtr(in.NextReviewAt)
	r.UpdatedAt = decided
	r.Version++
	return r, nil
}
func (r Review) Revoke(in RevokeInput, now time.Time) (Review, error) {
	if r.Status != StatusApproved {
		return Review{}, errors.New("only approved reviews may be revoked")
	}
	if in.ExpectedVersion != r.Version || strings.TrimSpace(in.ActorID) == "" || len(strings.TrimSpace(in.Reason)) < 8 {
		return Review{}, ErrConflict
	}
	at := now.UTC()
	r.Status = StatusRevoked
	r.RevokedBy = in.ActorID
	r.RevokedAt = &at
	r.RevokeReason = strings.TrimSpace(in.Reason)
	r.LastTransitionReason = r.RevokeReason
	r.UpdatedAt = at
	r.Version++
	return r, nil
}
func normalizeExternalEvidence(values []ExternalEvidenceReference) ([]ExternalEvidenceReference, error) {
	seen := map[string]struct{}{}
	out := make([]ExternalEvidenceReference, 0, len(values))
	for _, value := range values {
		reference := strings.TrimSpace(value.Reference)
		checksum := strings.ToLower(strings.TrimSpace(value.SHA256Checksum))
		if reference == "" && checksum == "" {
			continue
		}
		if reference == "" || len(reference) > 1000 || strings.ContainsAny(reference, "\r\n\x00") {
			return nil, errors.New("external evidence reference is invalid")
		}
		if !sha256Pattern.MatchString(checksum) {
			return nil, errors.New("external evidence reference requires a lowercase SHA-256 checksum")
		}
		key := reference + "\x00" + checksum
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ExternalEvidenceReference{Reference: reference, SHA256Checksum: checksum})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Reference == out[j].Reference {
			return out[i].SHA256Checksum < out[j].SHA256Checksum
		}
		return out[i].Reference < out[j].Reference
	})
	return out, nil
}

func normalizeSampleReviewNotes(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) > 2000 {
		return "", errors.New("sample review notes exceed 2000 characters")
	}
	if potentialEmailPII.MatchString(value) || potentialPhonePII.MatchString(value) {
		return "", errors.New("sample review notes must not contain email addresses or telephone numbers")
	}
	return value, nil
}

func utcPtr(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	x := v.UTC()
	return &x
}
func uniqueValues(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
func upperValues(values []string) []string {
	out := uniqueValues(values)
	for i := range out {
		out[i] = strings.ToUpper(out[i])
	}
	sort.Strings(out)
	return out
}

var (
	ErrNotFound  = errors.New("consent review not found")
	ErrConflict  = errors.New("consent review version conflict")
	ErrDuplicate = errors.New("consent review already exists")
)

type Repository interface {
	Create(context.Context, Review) error
	CompareAndSwap(context.Context, Review, int64) error
	List(context.Context, string) ([]Review, error)
	Get(context.Context, string) (Review, error)
	CreateRevision(context.Context, Review, Review, int64) error
}
type Service struct {
	repository    Repository
	organisations interface {
		Get(context.Context, string) (organisation.Organisation, error)
	}
	Evidence EvidenceResolver
	clock    func() time.Time
}

func NewService(r Repository) *Service { return &Service{repository: r, clock: time.Now} }
func (s *Service) WithOrganisationReader(reader interface {
	Get(context.Context, string) (organisation.Organisation, error)
}) *Service {
	s.organisations = reader
	return s
}
func (s *Service) resolveEvidence(ctx context.Context, r *Review) error {
	if len(r.EvidenceAssetIDs) == 0 {
		return nil
	}
	if s.Evidence == nil {
		return errors.New("trusted consent evidence resolver is required")
	}
	keys := make([]string, 0, len(r.EvidenceAssetIDs))
	for _, assetID := range r.EvidenceAssetIDs {
		asset, err := s.Evidence.ResolveConsentEvidence(ctx, assetID)
		if err != nil {
			return err
		}
		keys = append(keys, asset.ObjectKey)
	}
	r.EvidenceObjectKeys = keys
	return nil
}
func (s *Service) Create(ctx context.Context, input CreateInput) (Review, error) {
	if s.organisations != nil {
		org, err := s.organisations.Get(ctx, input.OrganisationID)
		if err != nil {
			return Review{}, err
		}
		if org.Status != organisation.StatusActive {
			return Review{}, organisation.ErrNotActive
		}
	}
	entity, err := NewReview(input, s.clock())
	if err != nil {
		return Review{}, err
	}
	if err = s.resolveEvidence(ctx, &entity); err != nil {
		return Review{}, err
	}
	if err = s.repository.Create(ctx, entity); err != nil {
		return Review{}, err
	}
	return entity, nil
}
func (s *Service) Submit(ctx context.Context, id string, input SubmitInput) (Review, error) {
	entity, err := s.repository.Get(ctx, id)
	if err != nil {
		return Review{}, err
	}
	if err = s.resolveEvidence(ctx, &entity); err != nil {
		return Review{}, err
	}
	original := entity.Version
	entity, err = entity.Submit(input, s.clock())
	if err != nil {
		return Review{}, err
	}
	if err = s.repository.CompareAndSwap(ctx, entity, original); err != nil {
		return Review{}, err
	}
	return entity, nil
}
func (s *Service) Decide(ctx context.Context, id string, input DecisionInput) (Review, error) {
	entity, err := s.repository.Get(ctx, id)
	if err != nil {
		return Review{}, err
	}
	if input.Decision == StatusApproved && s.organisations != nil {
		org, e := s.organisations.Get(ctx, entity.OrganisationID)
		if e != nil {
			return Review{}, e
		}
		if org.Status != organisation.StatusActive {
			return Review{}, organisation.ErrNotActive
		}
	}
	original := entity.Version
	entity, err = entity.Decide(input, s.clock())
	if err != nil {
		return Review{}, err
	}
	if err = s.repository.CompareAndSwap(ctx, entity, original); err != nil {
		return Review{}, err
	}
	return entity, nil
}
func (s *Service) Revoke(ctx context.Context, id string, input RevokeInput) (Review, error) {
	entity, err := s.repository.Get(ctx, id)
	if err != nil {
		return Review{}, err
	}
	original := entity.Version
	entity, err = entity.Revoke(input, s.clock())
	if err != nil {
		return Review{}, err
	}
	if err = s.repository.CompareAndSwap(ctx, entity, original); err != nil {
		return Review{}, err
	}
	return entity, nil
}
func (s *Service) CreateRevision(ctx context.Context, previousID string, expected int64, input CreateInput) (Review, error) {
	previous, err := s.repository.Get(ctx, previousID)
	if err != nil {
		return Review{}, err
	}
	if previous.Version != expected || previous.Status != StatusApproved {
		return Review{}, ErrConflict
	}
	input.OrganisationID = previous.OrganisationID
	input.ParentReviewID = previous.ID
	replacement, err := NewReview(input, s.clock())
	if err != nil {
		return Review{}, err
	}
	if err = s.resolveEvidence(ctx, &replacement); err != nil {
		return Review{}, err
	}
	reason := strings.TrimSpace(input.RevisionReason)
	if len(reason) < 8 {
		return Review{}, errors.New("a meaningful revision reason is required")
	}
	previous.Status = StatusSuperseded
	previous.SupersededByID = replacement.ID
	previous.LastTransitionReason = reason
	replacement.LastTransitionReason = reason
	previous.Version++
	previous.UpdatedAt = s.clock().UTC()
	if err = s.repository.CreateRevision(ctx, previous, replacement, expected); err != nil {
		return Review{}, err
	}
	return replacement, nil
}
func (s *Service) Get(ctx context.Context, id string) (Review, error) {
	return s.repository.Get(ctx, id)
}
func (s *Service) List(ctx context.Context, org string) ([]Review, error) {
	return s.repository.List(ctx, org)
}

type MemoryRepository struct {
	mu    sync.RWMutex
	items map[string]Review
}

func NewMemoryRepository() *MemoryRepository { return &MemoryRepository{items: map[string]Review{}} }
func (r *MemoryRepository) Create(_ context.Context, e Review) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[e.ID]; ok {
		return ErrDuplicate
	}
	r.items[e.ID] = e
	return nil
}
func (r *MemoryRepository) CompareAndSwap(_ context.Context, e Review, expected int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.items[e.ID]
	if !ok {
		return ErrNotFound
	}
	if cur.Version != expected || e.Version != expected+1 {
		return ErrConflict
	}
	r.items[e.ID] = e
	return nil
}
func (r *MemoryRepository) CreateRevision(_ context.Context, previous, replacement Review, expected int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.items[previous.ID]
	if !ok {
		return ErrNotFound
	}
	if cur.Version != expected || previous.Version != expected+1 {
		return ErrConflict
	}
	if _, ok := r.items[replacement.ID]; ok {
		return ErrDuplicate
	}
	r.items[previous.ID] = previous
	r.items[replacement.ID] = replacement
	return nil
}
func (r *MemoryRepository) List(_ context.Context, org string) ([]Review, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := []Review{}
	for _, e := range r.items {
		if org == "" || e.OrganisationID == org {
			items = append(items, e)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}
func (r *MemoryRepository) ListReviewPage(_ context.Context, org string, limit int, before *time.Time, beforeID string) ([]Review, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Review, 0, len(r.items))
	for _, v := range r.items {
		if org != "" && v.OrganisationID != org {
			continue
		}
		if before != nil && !(v.CreatedAt.Before(*before) || (v.CreatedAt.Equal(*before) && v.ID < beforeID)) {
			continue
		}
		out = append(out, v)
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

func (r *MemoryRepository) Get(_ context.Context, id string) (Review, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.items[id]
	if !ok {
		return Review{}, ErrNotFound
	}
	return e, nil
}
func (s *Service) ValidateCampaignReview(ctx context.Context, reviewID, campaignID, organisationID, channel string, asOf time.Time) error {
	if s == nil || s.repository == nil {
		return errors.New("consent review repository is required")
	}
	review, err := s.repository.Get(ctx, strings.TrimSpace(reviewID))
	if err != nil {
		return err
	}
	if review.OrganisationID != strings.TrimSpace(organisationID) {
		return errors.New("consent review does not belong to campaign organisation")
	}
	if review.Status != StatusApproved {
		return errors.New("consent review is not approved")
	}
	if review.ExpiresAt == nil || !review.ExpiresAt.After(asOf.UTC()) {
		return errors.New("consent review is expired")
	}
	if review.Channel != strings.ToUpper(strings.TrimSpace(channel)) {
		return errors.New("consent review does not authorise campaign channel")
	}
	if review.Scope == ReviewScopeCampaign {
		if strings.TrimSpace(review.CampaignID) == "" || review.CampaignID != strings.TrimSpace(campaignID) {
			return errors.New("campaign-scoped consent review does not authorise this campaign")
		}
	}
	return nil
}
