package importer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrImportNotApproved       = errors.New("audience import is not approved for merge")
	ErrMakerChecker            = errors.New("audience import uploader cannot approve the same import")
	ErrConsentBasis            = errors.New("audience import consent basis is invalid or expired")
	ErrUnsupportedUpdatePolicy = errors.New("audience import update policy is not implemented for durable merge")
)

type MergeResult struct {
	InsertedContacts int `json:"insertedContacts"`
	UpdatedContacts  int `json:"updatedContacts"`
	ConsentGrants    int `json:"consentGrants"`
	SourceLinks      int `json:"sourceLinks"`
	ProfileHistory   int `json:"profileHistory"`
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
	UploadedBy, ApprovedBy                                 string
	Status                                                 string
	ReviewApproved                                         bool
	ReviewExpiresAt                                        time.Time
	EvidenceClean                                          bool
	UpdatePolicy                                           UpdatePolicy
	Candidates                                             []ContactCandidate
}

type memoryContact struct {
	Candidate ContactCandidate
	Sources   map[string]struct{}
	Consents  map[string]struct{}
	History   map[string]struct{}
}

type MemoryMergeRepository struct {
	mu       sync.Mutex
	imports  map[string]MemoryMergeImport
	contacts map[string]*memoryContact
	results  map[string]MergeResult
}

func NewMemoryMergeRepository() *MemoryMergeRepository {
	return &MemoryMergeRepository{imports: map[string]MemoryMergeImport{}, contacts: map[string]*memoryContact{}, results: map[string]MergeResult{}}
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
	if input.UpdatePolicy != UpdateNewestSource && input.UpdatePolicy != UpdateFillNull {
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
			contact = &memoryContact{Candidate: cloneCandidate(candidate), Sources: map[string]struct{}{}, Consents: map[string]struct{}{}, History: map[string]struct{}{}}
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
			}
			result.UpdatedContacts++
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
