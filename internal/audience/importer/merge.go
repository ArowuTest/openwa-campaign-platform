package importer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrImportNotApproved       = errors.New("audience import is not approved for merge")
	ErrMakerChecker            = errors.New("audience import uploader cannot approve the same import")
	ErrConsentBasis            = errors.New("audience import consent basis is invalid or expired")
	ErrUnsupportedUpdatePolicy = errors.New("unsupported audience import update policy")
)

type MergeResult struct {
	InsertedContacts int `json:"insertedContacts"`
	UpdatedContacts  int `json:"updatedContacts"`
	ConsentGrants    int `json:"consentGrants"`
	SourceLinks      int `json:"sourceLinks"`
	ProfileHistory   int `json:"profileHistory"`
	Conflicts        int `json:"conflicts"`
}

type MergeRepository interface {
	Merge(context.Context, string, time.Time) (MergeResult, error)
}

type MergeService struct {
	Repository MergeRepository
	Clock      func() time.Time
}

func (s *MergeService) Merge(ctx context.Context, importID string) (MergeResult, error) {
	if s == nil || s.Repository == nil {
		return MergeResult{}, errors.New("merge repository is required")
	}
	if strings.TrimSpace(importID) == "" {
		return MergeResult{}, errors.New("import ID is required")
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	return s.Repository.Merge(ctx, importID, now)
}

type MemoryMergeImport struct {
	ID, OrganisationID, PurposeID, Channel, WordingVersion string
	SourceSystem                                           string
	UploadedBy, ApprovedBy                                 string
	Status                                                 string
	ReviewApproved                                         bool
	ReviewExpiresAt                                        time.Time
	EvidenceClean                                          bool
	UpdatePolicy                                           UpdatePolicy
	Candidates                                             []ContactCandidate
}

type memoryContact struct {
	Candidate    ContactCandidate
	SourceSystem string
	TrustLevel   int
	Sources      map[string]struct{}
	Consents     map[string]struct{}
	History      map[string]struct{}
}

type MemoryMergeRepository struct {
	mu          sync.Mutex
	imports     map[string]MemoryMergeImport
	contacts    map[string]*memoryContact
	results     map[string]MergeResult
	conflicts   *MemoryConflictRepository
	sourceTrust map[string]int
}

func NewMemoryMergeRepository() *MemoryMergeRepository {
	return &MemoryMergeRepository{imports: map[string]MemoryMergeImport{}, contacts: map[string]*memoryContact{}, results: map[string]MergeResult{}, conflicts: NewMemoryConflictRepository(), sourceTrust: map[string]int{}}
}
func (r *MemoryMergeRepository) Seed(input MemoryMergeImport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	input.Candidates = append([]ContactCandidate(nil), input.Candidates...)
	r.imports[input.ID] = input
}
func (r *MemoryMergeRepository) Merge(_ context.Context, importID string, now time.Time) (MergeResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	input, ok := r.imports[importID]
	if !ok || (input.Status != "APPROVED" && input.Status != "COMPLETED" && input.Status != "COMPLETED_WITH_EXCEPTIONS") {
		return MergeResult{}, ErrImportNotApproved
	}
	if strings.TrimSpace(input.UploadedBy) == "" || input.UploadedBy == input.ApprovedBy {
		return MergeResult{}, ErrMakerChecker
	}
	if !input.ReviewApproved || !input.EvidenceClean || !input.ReviewExpiresAt.After(now) || input.OrganisationID == "" || input.PurposeID == "" || input.Channel == "" || input.WordingVersion == "" {
		return MergeResult{}, ErrConsentBasis
	}
	if input.UpdatePolicy == "" {
		input.UpdatePolicy = UpdateNewestSource
	}
	if input.UpdatePolicy != UpdateInsertOnly && input.UpdatePolicy != UpdateNewestSource && input.UpdatePolicy != UpdateFillNull && input.UpdatePolicy != UpdateTrustedSource && input.UpdatePolicy != UpdateManualConflict {
		return MergeResult{}, ErrUnsupportedUpdatePolicy
	}
	if previous, exists := r.results[importID]; exists {
		return previous, nil
	}
	result := MergeResult{}
	for _, candidate := range input.Candidates {
		if candidate.E164 != "" || len(candidate.LookupHMAC) == 0 || len(candidate.EncryptedMSISDN) == 0 {
			return MergeResult{}, errors.New("merge accepts protected staged candidates only")
		}
		key := string(candidate.LookupHMAC)
		contact, exists := r.contacts[key]
		if !exists {
			contact = &memoryContact{Candidate: cloneCandidate(candidate), SourceSystem: input.SourceSystem, TrustLevel: r.trustLevel(input.OrganisationID, input.SourceSystem), Sources: map[string]struct{}{}, Consents: map[string]struct{}{}, History: map[string]struct{}{}}
			r.contacts[key] = contact
			result.InsertedContacts++
		} else {
			// The immutable history remains authoritative. Current profile fields
			// follow the explicitly approved import conflict policy.
			switch input.UpdatePolicy {
			case UpdateNewestSource:
				if !candidate.ProfileRecordedAt.Before(contact.Candidate.ProfileRecordedAt) {
					contact.Candidate = cloneCandidate(candidate)
				}
			case UpdateFillNull:
				mergeCandidateFillNull(&contact.Candidate, candidate)
			case UpdateTrustedSource:
				incomingTrust := r.trustLevel(input.OrganisationID, input.SourceSystem)
				if incomingTrust > contact.TrustLevel || (incomingTrust == contact.TrustLevel && !candidate.ProfileRecordedAt.Before(contact.Candidate.ProfileRecordedAt)) {
					contact.Candidate = cloneCandidate(candidate)
					contact.SourceSystem = input.SourceSystem
					contact.TrustLevel = incomingTrust
				}
			case UpdateManualConflict:
				result.Conflicts += r.captureManualConflicts(input.ID, contact.Candidate, candidate, now)
				mergeCandidateFillNull(&contact.Candidate, candidate)
			}
			if input.UpdatePolicy != UpdateInsertOnly {
				result.UpdatedContacts++
			}
		}
		sourceKey := input.OrganisationID + "\x1f" + importID + "\x1f" + candidate.SourceHash
		if _, exists := contact.Sources[sourceKey]; !exists {
			contact.Sources[sourceKey] = struct{}{}
			result.SourceLinks++
		}
		consentKey := input.PurposeID + "\x1f" + strings.ToUpper(input.Channel) + "\x1f" + importID
		if _, exists := contact.Consents[consentKey]; !exists {
			contact.Consents[consentKey] = struct{}{}
			result.ConsentGrants++
		}
		historyKey := importID + "\x1f" + candidate.SourceHash
		if _, exists := contact.History[historyKey]; !exists {
			contact.History[historyKey] = struct{}{}
			result.ProfileHistory++
		}
	}
	input.Status = "COMPLETED"
	r.imports[importID] = input
	r.results[importID] = result
	return result, nil
}

func mergeCandidateFillNull(current *ContactCandidate, incoming ContactCandidate) {
	if current == nil {
		return
	}
	if current.Country == "" {
		current.Country = incoming.Country
	}
	if current.State == "" {
		current.State = incoming.State
	}
	if current.LGA == "" {
		current.LGA = incoming.LGA
	}
	if current.ReportedAge == nil && incoming.ReportedAge != nil {
		value := *incoming.ReportedAge
		current.ReportedAge = &value
		current.AgeRecordedAt = incoming.AgeRecordedAt
	}
	if current.Gender == "" {
		current.Gender = incoming.Gender
	}
	if incoming.ProfileRecordedAt.After(current.ProfileRecordedAt) {
		current.ProfileRecordedAt = incoming.ProfileRecordedAt
	}
}

func (r *MemoryMergeRepository) captureManualConflicts(importID string, current, incoming ContactCandidate, now time.Time) int {
	if r.conflicts == nil {
		r.conflicts = NewMemoryConflictRepository()
	}
	type value struct{ field, existing, incoming string }
	values := []value{
		{"country_id", current.Country, incoming.Country},
		{"state_id", current.State, incoming.State},
		{"lga_id", current.LGA, incoming.LGA},
		{"gender_code", current.Gender, incoming.Gender},
	}
	if current.ReportedAge != nil && incoming.ReportedAge != nil {
		values = append(values, value{"reported_age", fmt.Sprint(*current.ReportedAge), fmt.Sprint(*incoming.ReportedAge)})
	}
	count := 0
	for _, item := range values {
		if strings.TrimSpace(item.existing) == "" || strings.TrimSpace(item.incoming) == "" || item.existing == item.incoming {
			continue
		}
		r.conflicts.Add(ProfileConflict{AudienceImportID: importID, MaskedMSISDN: incoming.MaskedMSISDN, Field: item.field, ExistingValue: item.existing, IncomingValue: item.incoming, CreatedAt: now})
		count++
	}
	return count
}

func (r *MemoryMergeRepository) ConflictRepository() *MemoryConflictRepository {
	if r.conflicts == nil {
		r.conflicts = NewMemoryConflictRepository()
	}
	return r.conflicts
}

func (r *MemoryMergeRepository) SetSourceTrust(organisationID, sourceSystem string, trustLevel int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sourceTrust == nil {
		r.sourceTrust = map[string]int{}
	}
	if trustLevel < 0 {
		trustLevel = 0
	}
	if trustLevel > 100 {
		trustLevel = 100
	}
	r.sourceTrust[strings.TrimSpace(organisationID)+"\x1f"+strings.ToUpper(strings.TrimSpace(sourceSystem))] = trustLevel
}

func (r *MemoryMergeRepository) trustLevel(organisationID, sourceSystem string) int {
	if r.sourceTrust == nil {
		return 0
	}
	return r.sourceTrust[strings.TrimSpace(organisationID)+"\x1f"+strings.ToUpper(strings.TrimSpace(sourceSystem))]
}
