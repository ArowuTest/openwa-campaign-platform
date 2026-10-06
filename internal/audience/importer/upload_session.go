package importer

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	sharedid "campaign-platform/internal/shared/id"
)

const (
	DefaultUploadPartSize int64 = 8 << 20
	MinUploadPartSize     int64 = 5 << 20
	MaxUploadPartSize     int64 = 32 << 20
	MaximumUploadParts          = 1024
)

var (
	ErrUploadSessionNotFound   = errors.New("audience import upload session not found")
	ErrUploadSessionConflict   = errors.New("audience import upload session version conflict")
	ErrUploadSessionState      = errors.New("audience import upload session state does not permit the action")
	ErrUploadSessionExpired    = errors.New("audience import upload session has expired")
	ErrUploadPartConflict      = errors.New("audience import upload part conflicts with existing immutable evidence")
	ErrUploadPartIncomplete    = errors.New("audience import upload is incomplete")
	ErrUploadPartOutOfRange    = errors.New("audience import upload part number is outside the session range")
	ErrUploadPartInvalid       = errors.New("audience import upload part evidence is invalid")
	ErrUploadFinalisationLease = errors.New("audience import upload finalisation lease is invalid or stale")
)

type UploadSessionState string

const (
	UploadSessionCreated       UploadSessionState = "CREATED"
	UploadSessionUploading     UploadSessionState = "UPLOADING"
	UploadSessionUploaded      UploadSessionState = "UPLOADED"
	UploadSessionFinalising    UploadSessionState = "FINALISING"
	UploadSessionImportCreated UploadSessionState = "IMPORT_CREATED"
	UploadSessionAborted       UploadSessionState = "ABORTED"
	UploadSessionExpired       UploadSessionState = "EXPIRED"
	UploadSessionFailed        UploadSessionState = "FAILED"
)

type UploadPartState string

type FinalisationLease struct {
	Owner     string    `json:"owner"`
	Version   int64     `json:"version"`
	ExpiresAt time.Time `json:"expiresAt"`
}

const (
	UploadPartPending  UploadPartState = "PENDING"
	UploadPartUploaded UploadPartState = "UPLOADED"
)

type UploadSessionInput struct {
	OrganisationID      string
	ConsentReviewID     string
	PurposeID           string
	Channel             string
	WordingVersion      string
	SourceName          string
	SourceSystem        string
	DefaultCountryISO2  string
	OriginalFilename    string
	TemplateVersion     string
	MappingDefinitionID string
	Mapping             ColumnMapping
	UpdatePolicy        UpdatePolicy
	UploadedBy          string
	ClientRequestID     string
	ExpectedBytes       int64
}

type UploadPart struct {
	Number        int             `json:"number"`
	Offset        int64           `json:"offset"`
	ExpectedBytes int64           `json:"expectedBytes"`
	ObjectKey     string          `json:"-"`
	State         UploadPartState `json:"state"`
	SHA256        string          `json:"sha256,omitempty"`
	UploadedBytes int64           `json:"uploadedBytes,omitempty"`
	UploadedAt    *time.Time      `json:"uploadedAt,omitempty"`
}

type UploadSession struct {
	ID                      string             `json:"id"`
	OrganisationID          string             `json:"organisationId"`
	ConsentReviewID         string             `json:"consentReviewId"`
	PurposeID               string             `json:"purposeId"`
	Channel                 string             `json:"channel"`
	WordingVersion          string             `json:"wordingVersion"`
	SourceName              string             `json:"sourceName"`
	SourceSystem            string             `json:"sourceSystem,omitempty"`
	DefaultCountryISO2      string             `json:"defaultCountryIso2,omitempty"`
	OriginalFilename        string             `json:"originalFilename"`
	TemplateVersion         string             `json:"templateVersion"`
	MappingDefinitionID     string             `json:"mappingDefinitionId,omitempty"`
	Mapping                 ColumnMapping      `json:"mapping"`
	UpdatePolicy            UpdatePolicy       `json:"updatePolicy"`
	UploadedBy              string             `json:"uploadedBy"`
	ClientRequestID         string             `json:"-"`
	ExpectedBytes           int64              `json:"expectedBytes"`
	UploadedBytes           int64              `json:"uploadedBytes"`
	PartSize                int64              `json:"partSize"`
	PartCount               int                `json:"partCount"`
	UploadedParts           int                `json:"uploadedParts"`
	State                   UploadSessionState `json:"state"`
	Version                 int64              `json:"version"`
	FailureReason           string             `json:"failureReason,omitempty"`
	FinalSHA256             string             `json:"finalSha256,omitempty"`
	DetectedMediaType       string             `json:"detectedMediaType,omitempty"`
	LinkedImportID          string             `json:"linkedImportId,omitempty"`
	FinaliserLeaseOwner     string             `json:"-"`
	FinaliserLeaseVersion   int64              `json:"-"`
	FinaliserLeaseExpiresAt *time.Time         `json:"-"`
	ExpiresAt               time.Time          `json:"expiresAt"`
	CreatedAt               time.Time          `json:"createdAt"`
	UpdatedAt               time.Time          `json:"updatedAt"`
	Parts                   []UploadPart       `json:"parts"`
}

func NewUploadSession(input UploadSessionInput, partSize, maxFileSize int64, expiresAt, now time.Time) (UploadSession, error) {
	now = now.UTC()
	expiresAt = expiresAt.UTC()
	if strings.TrimSpace(input.OrganisationID) == "" ||
		strings.TrimSpace(input.ConsentReviewID) == "" ||
		strings.TrimSpace(input.PurposeID) == "" ||
		strings.TrimSpace(input.Channel) == "" ||
		strings.TrimSpace(input.WordingVersion) == "" ||
		strings.TrimSpace(input.SourceName) == "" ||
		strings.TrimSpace(input.UploadedBy) == "" {
		return UploadSession{}, errors.New("governed import context and uploader are required")
	}
	if err := ValidateIdempotencyKey(input.ClientRequestID); err != nil {
		return UploadSession{}, err
	}
	filename, err := safeOriginalFilename(input.OriginalFilename)
	if err != nil {
		return UploadSession{}, err
	}
	if input.ExpectedBytes <= 0 {
		return UploadSession{}, errors.New("expected upload size must be positive")
	}
	if maxFileSize <= 0 || input.ExpectedBytes > maxFileSize {
		return UploadSession{}, fmt.Errorf("expected upload size exceeds configured maximum of %d bytes", maxFileSize)
	}
	if partSize < MinUploadPartSize || partSize > MaxUploadPartSize {
		return UploadSession{}, fmt.Errorf("upload part size must be between %d and %d bytes", MinUploadPartSize, MaxUploadPartSize)
	}
	if !expiresAt.After(now) {
		return UploadSession{}, errors.New("upload session expiry must be in the future")
	}
	if !validUpdatePolicy(input.UpdatePolicy) {
		return UploadSession{}, errors.New("valid update policy is required")
	}
	if strings.TrimSpace(input.Mapping.MSISDN) == "" {
		return UploadSession{}, errors.New("upload mapping must identify the MSISDN column")
	}

	partCount64 := (input.ExpectedBytes + partSize - 1) / partSize
	if partCount64 <= 0 || partCount64 > MaximumUploadParts {
		return UploadSession{}, fmt.Errorf("upload requires %d parts; maximum is %d", partCount64, MaximumUploadParts)
	}
	identifier, err := sharedid.New()
	if err != nil {
		return UploadSession{}, err
	}
	parts := make([]UploadPart, int(partCount64))
	for index := range parts {
		number := index + 1
		offset := int64(index) * partSize
		expected := partSize
		if remaining := input.ExpectedBytes - offset; remaining < expected {
			expected = remaining
		}
		parts[index] = UploadPart{
			Number: number, Offset: offset, ExpectedBytes: expected,
			ObjectKey: fmt.Sprintf("imports/uploads/%s/part-%06d.bin", identifier, number),
			State:     UploadPartPending,
		}
	}

	return UploadSession{
		ID:                  identifier,
		OrganisationID:      strings.TrimSpace(input.OrganisationID),
		ConsentReviewID:     strings.TrimSpace(input.ConsentReviewID),
		PurposeID:           strings.TrimSpace(input.PurposeID),
		Channel:             strings.ToUpper(strings.TrimSpace(input.Channel)),
		WordingVersion:      strings.TrimSpace(input.WordingVersion),
		SourceName:          strings.TrimSpace(input.SourceName),
		SourceSystem:        strings.TrimSpace(input.SourceSystem),
		DefaultCountryISO2:  strings.ToUpper(strings.TrimSpace(input.DefaultCountryISO2)),
		OriginalFilename:    filename,
		TemplateVersion:     strings.TrimSpace(input.TemplateVersion),
		MappingDefinitionID: strings.TrimSpace(input.MappingDefinitionID),
		Mapping:             input.Mapping,
		UpdatePolicy:        input.UpdatePolicy,
		UploadedBy:          strings.TrimSpace(input.UploadedBy),
		ClientRequestID:     strings.TrimSpace(input.ClientRequestID),
		ExpectedBytes:       input.ExpectedBytes,
		PartSize:            partSize,
		PartCount:           int(partCount64),
		State:               UploadSessionCreated,
		Version:             1,
		ExpiresAt:           expiresAt,
		CreatedAt:           now,
		UpdatedAt:           now,
		Parts:               parts,
	}, nil
}

func (s UploadSession) RecordPart(number int, size int64, checksum string, expectedVersion int64, now time.Time) (UploadSession, bool, error) {
	now = now.UTC()
	if s.Version != expectedVersion {
		return UploadSession{}, false, ErrUploadSessionConflict
	}
	if !now.Before(s.ExpiresAt) {
		return UploadSession{}, false, ErrUploadSessionExpired
	}
	if s.State != UploadSessionCreated && s.State != UploadSessionUploading {
		return UploadSession{}, false, ErrUploadSessionState
	}
	if number < 1 || number > len(s.Parts) {
		return UploadSession{}, false, ErrUploadPartOutOfRange
	}
	checksum = strings.ToLower(strings.TrimSpace(checksum))
	if !validSHA256(checksum) {
		return UploadSession{}, false, ErrUploadPartInvalid
	}
	current := s.Parts[number-1]
	if size != current.ExpectedBytes {
		return UploadSession{}, false, fmt.Errorf("%w: part %d requires %d bytes, got %d", ErrUploadPartInvalid, number, current.ExpectedBytes, size)
	}
	if current.State == UploadPartUploaded {
		if current.UploadedBytes == size && strings.EqualFold(current.SHA256, checksum) {
			return cloneUploadSession(s), false, nil
		}
		return UploadSession{}, false, ErrUploadPartConflict
	}

	next := cloneUploadSession(s)
	uploadedAt := now
	next.Parts[number-1].State = UploadPartUploaded
	next.Parts[number-1].SHA256 = checksum
	next.Parts[number-1].UploadedBytes = size
	next.Parts[number-1].UploadedAt = &uploadedAt
	next.UploadedParts++
	next.UploadedBytes += size
	next.State = UploadSessionUploading
	next.Version++
	next.UpdatedAt = now
	return next, true, nil
}

func (s UploadSession) Complete(expectedVersion int64, now time.Time) (UploadSession, error) {
	now = now.UTC()
	if s.Version != expectedVersion {
		return UploadSession{}, ErrUploadSessionConflict
	}
	if !now.Before(s.ExpiresAt) {
		return UploadSession{}, ErrUploadSessionExpired
	}
	if s.State != UploadSessionCreated && s.State != UploadSessionUploading {
		return UploadSession{}, ErrUploadSessionState
	}
	var bytes int64
	var count int
	for _, part := range s.Parts {
		if part.State != UploadPartUploaded || part.UploadedBytes != part.ExpectedBytes || !validSHA256(part.SHA256) {
			return UploadSession{}, ErrUploadPartIncomplete
		}
		bytes += part.UploadedBytes
		count++
	}
	if bytes != s.ExpectedBytes || count != s.PartCount {
		return UploadSession{}, ErrUploadPartIncomplete
	}
	next := cloneUploadSession(s)
	next.UploadedBytes = bytes
	next.UploadedParts = count
	next.State = UploadSessionUploaded
	next.Version++
	next.UpdatedAt = now
	return next, nil
}

func (s UploadSession) Abort(reason string, expectedVersion int64, now time.Time) (UploadSession, error) {
	if s.Version != expectedVersion {
		return UploadSession{}, ErrUploadSessionConflict
	}
	switch s.State {
	case UploadSessionCreated, UploadSessionUploading, UploadSessionUploaded:
	default:
		return UploadSession{}, ErrUploadSessionState
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 8 {
		return UploadSession{}, errors.New("upload abort reason must contain at least 8 characters")
	}
	next := cloneUploadSession(s)
	next.State = UploadSessionAborted
	next.FailureReason = reason
	next.Version++
	next.UpdatedAt = now.UTC()
	return next, nil
}

func (s UploadSession) ClaimFinalisation(workerID string, leaseDuration time.Duration, now time.Time) (UploadSession, FinalisationLease, error) {
	now = now.UTC()
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return UploadSession{}, FinalisationLease{}, errors.New("finalisation worker ID is required")
	}
	if leaseDuration <= 0 || leaseDuration > 30*time.Minute {
		return UploadSession{}, FinalisationLease{}, errors.New("finalisation lease duration must be between 1ns and 30m")
	}
	switch s.State {
	case UploadSessionUploaded:
		// First finaliser claim.
	case UploadSessionFinalising:
		if s.FinaliserLeaseExpiresAt != nil && now.Before(s.FinaliserLeaseExpiresAt.UTC()) {
			return UploadSession{}, FinalisationLease{}, ErrUploadFinalisationLease
		}
	default:
		return UploadSession{}, FinalisationLease{}, ErrUploadSessionState
	}

	next := cloneUploadSession(s)
	next.State = UploadSessionFinalising
	next.FinaliserLeaseOwner = workerID
	next.FinaliserLeaseVersion++
	expiresAt := now.Add(leaseDuration)
	next.FinaliserLeaseExpiresAt = &expiresAt
	next.Version++
	next.UpdatedAt = now
	return next, FinalisationLease{Owner: workerID, Version: next.FinaliserLeaseVersion, ExpiresAt: expiresAt}, nil
}

func (s UploadSession) RenewFinalisation(lease FinalisationLease, leaseDuration time.Duration, now time.Time) (UploadSession, FinalisationLease, error) {
	now = now.UTC()
	if s.State != UploadSessionFinalising {
		return UploadSession{}, FinalisationLease{}, ErrUploadSessionState
	}
	if leaseDuration <= 0 || leaseDuration > 30*time.Minute {
		return UploadSession{}, FinalisationLease{}, errors.New("finalisation lease duration must be between 1ns and 30m")
	}
	if err := s.validateFinalisationLease(lease, now); err != nil {
		return UploadSession{}, FinalisationLease{}, err
	}
	expiresAt := now.Add(leaseDuration)
	if s.FinaliserLeaseExpiresAt == nil || !expiresAt.After(s.FinaliserLeaseExpiresAt.UTC()) {
		return UploadSession{}, FinalisationLease{}, errors.New("finalisation renewal must extend the current lease")
	}
	next := cloneUploadSession(s)
	next.FinaliserLeaseExpiresAt = &expiresAt
	next.Version++
	next.UpdatedAt = now
	return next, FinalisationLease{Owner: lease.Owner, Version: lease.Version, ExpiresAt: expiresAt}, nil
}

func (s UploadSession) CompleteFinalisation(importID, wholeFileSHA256, mediaType string, lease FinalisationLease, now time.Time) (UploadSession, error) {
	now = now.UTC()
	importID = strings.TrimSpace(importID)
	wholeFileSHA256 = strings.ToLower(strings.TrimSpace(wholeFileSHA256))
	mediaType = strings.TrimSpace(mediaType)
	if s.State != UploadSessionFinalising {
		return UploadSession{}, ErrUploadSessionState
	}
	if importID == "" || !validSHA256(wholeFileSHA256) || mediaType == "" {
		return UploadSession{}, errors.New("finalised import identity, SHA-256 and media type are required")
	}
	if err := s.validateFinalisationLease(lease, now); err != nil {
		return UploadSession{}, err
	}

	next := cloneUploadSession(s)
	next.State = UploadSessionImportCreated
	next.LinkedImportID = importID
	next.FinalSHA256 = wholeFileSHA256
	next.DetectedMediaType = mediaType
	next.FailureReason = ""
	next.FinaliserLeaseOwner = ""
	next.FinaliserLeaseExpiresAt = nil
	next.Version++
	next.UpdatedAt = now
	return next, nil
}

func (s UploadSession) FailFinalisation(reason string, lease FinalisationLease, now time.Time) (UploadSession, error) {
	now = now.UTC()
	if s.State != UploadSessionFinalising {
		return UploadSession{}, ErrUploadSessionState
	}
	if err := s.validateFinalisationLease(lease, now); err != nil {
		return UploadSession{}, err
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 8 {
		return UploadSession{}, errors.New("finalisation failure reason must contain at least 8 characters")
	}

	next := cloneUploadSession(s)
	next.State = UploadSessionFailed
	next.FailureReason = reason
	next.FinaliserLeaseOwner = ""
	next.FinaliserLeaseExpiresAt = nil
	next.Version++
	next.UpdatedAt = now
	return next, nil
}

func (s UploadSession) validateFinalisationLease(lease FinalisationLease, now time.Time) error {
	if strings.TrimSpace(lease.Owner) == "" ||
		lease.Owner != s.FinaliserLeaseOwner ||
		lease.Version <= 0 ||
		lease.Version != s.FinaliserLeaseVersion ||
		s.FinaliserLeaseExpiresAt == nil ||
		!now.Before(s.FinaliserLeaseExpiresAt.UTC()) ||
		!lease.ExpiresAt.Equal(s.FinaliserLeaseExpiresAt.UTC()) {
		return ErrUploadFinalisationLease
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256HexLength {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

const sha256HexLength = 64

func cloneUploadSession(value UploadSession) UploadSession {
	if value.FinaliserLeaseExpiresAt != nil {
		copied := value.FinaliserLeaseExpiresAt.UTC()
		value.FinaliserLeaseExpiresAt = &copied
	}
	value.Parts = append([]UploadPart(nil), value.Parts...)
	for index := range value.Parts {
		if value.Parts[index].UploadedAt != nil {
			copied := *value.Parts[index].UploadedAt
			value.Parts[index].UploadedAt = &copied
		}
	}
	return value
}
