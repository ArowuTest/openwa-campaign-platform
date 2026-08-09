package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/organisation"
	"campaign-platform/internal/shared/id"
)

type ImportStatus string

const (
	ImportUploaded                ImportStatus = "UPLOADED"
	ImportScanning                ImportStatus = "SCANNING"
	ImportQuarantined             ImportStatus = "QUARANTINED"
	ImportValidating              ImportStatus = "VALIDATING"
	ImportPreviewReady            ImportStatus = "PREVIEW_READY"
	ImportApproved                ImportStatus = "APPROVED"
	ImportImporting               ImportStatus = "IMPORTING"
	ImportCompleted               ImportStatus = "COMPLETED"
	ImportCompletedWithExceptions ImportStatus = "COMPLETED_WITH_EXCEPTIONS"
	ImportRejected                ImportStatus = "REJECTED"
	ImportFailed                  ImportStatus = "FAILED"
	ImportCancelled               ImportStatus = "CANCELLED"
	ImportRolledBack              ImportStatus = "ROLLED_BACK"
)

type MalwareStatus string

const (
	MalwarePending  MalwareStatus = "PENDING"
	MalwareClean    MalwareStatus = "CLEAN"
	MalwareInfected MalwareStatus = "INFECTED"
	MalwareFailed   MalwareStatus = "FAILED"
)

type UpdatePolicy string

const (
	UpdateInsertOnly     UpdatePolicy = "INSERT_ONLY"
	UpdateFillNull       UpdatePolicy = "FILL_NULL"
	UpdateNewestSource   UpdatePolicy = "NEWEST_SOURCE"
	UpdateTrustedSource  UpdatePolicy = "TRUSTED_SOURCE"
	UpdateManualConflict UpdatePolicy = "MANUAL_CONFLICT"
)

type ImportBatch struct {
	ID                    string          `json:"id"`
	OrganisationID        string          `json:"organisationId"`
	ConsentReviewID       string          `json:"consentReviewId"`
	PurposeID             string          `json:"purposeId"`
	Channel               string          `json:"channel"`
	WordingVersion        string          `json:"wordingVersion"`
	SourceName            string          `json:"sourceName"`
	SourceSystem          string          `json:"sourceSystem,omitempty"`
	DefaultCountryISO2    string          `json:"defaultCountryIso2,omitempty"`
	ObjectKey             string          `json:"objectKey"`
	OriginalFilename      string          `json:"originalFilename"`
	DetectedMediaType     string          `json:"detectedMediaType"`
	FileSHA256            string          `json:"fileSha256"`
	ByteSize              int64           `json:"byteSize"`
	TemplateVersion       string          `json:"templateVersion"`
	MappingDefinitionID   string          `json:"mappingDefinitionId,omitempty"`
	Mapping               json.RawMessage `json:"mapping"`
	UpdatePolicy          UpdatePolicy    `json:"updatePolicy"`
	Status                ImportStatus    `json:"status"`
	MalwareStatus         MalwareStatus   `json:"malwareStatus"`
	ContentSignatureValid bool            `json:"contentSignatureValid"`
	ClientRequestID       string          `json:"-"`
	UploadedRows          int64           `json:"uploadedRows"`
	ValidRows             int64           `json:"validRows"`
	InvalidRows           int64           `json:"invalidRows"`
	DuplicateRows         int64           `json:"duplicateRows"`
	SuppressedRows        int64           `json:"suppressedRows"`
	InsertedContacts      int64           `json:"insertedContacts"`
	UpdatedContacts       int64           `json:"updatedContacts"`
	UploadedBy            string          `json:"uploadedBy"`
	ApprovedBy            string          `json:"approvedBy,omitempty"`
	ApprovedAt            *time.Time      `json:"approvedAt,omitempty"`
	FailureReason         string          `json:"failureReason,omitempty"`
	SourceExpiresAt       *time.Time      `json:"sourceExpiresAt,omitempty"`
	SourceDeletedAt       *time.Time      `json:"sourceDeletedAt,omitempty"`
	RolledBackAt          *time.Time      `json:"rolledBackAt,omitempty"`
	RolledBackBy          string          `json:"rolledBackBy,omitempty"`
	RollbackReason        string          `json:"rollbackReason,omitempty"`
	Version               int64           `json:"version"`
	CreatedAt             time.Time       `json:"createdAt"`
	UpdatedAt             time.Time       `json:"updatedAt"`
}

type CreateImportInput struct {
	OrganisationID        string
	ConsentReviewID       string
	PurposeID             string
	Channel               string
	WordingVersion        string
	SourceName            string
	SourceSystem          string
	DefaultCountryISO2    string
	ObjectKey             string
	OriginalFilename      string
	DetectedMediaType     string
	FileSHA256            string
	ByteSize              int64
	TemplateVersion       string
	MappingDefinitionID   string
	Mapping               any
	SourceExpiresAt       *time.Time
	UpdatePolicy          UpdatePolicy
	UploadedBy            string
	ClientRequestID       string
	ContentSignatureValid bool
}

var (
	ErrImportNotFound         = errors.New("audience import not found")
	ErrImportConflict         = errors.New("audience import version conflict")
	ErrImportReplayConflict   = errors.New("audience import idempotency key reused with different file or metadata")
	ErrDuplicateImportFile    = errors.New("identical audience file already exists for organisation")
	ErrImportTransition       = errors.New("audience import transition is not allowed")
	ErrImportScanRequired     = errors.New("clean malware scan and valid content signature are required")
	ErrImportApprovalEvidence = errors.New("approved consent review and consent purpose are required")
)

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func NewImportBatch(input CreateImportInput, now time.Time) (ImportBatch, error) {
	if strings.TrimSpace(input.OrganisationID) == "" || strings.TrimSpace(input.ConsentReviewID) == "" || strings.TrimSpace(input.PurposeID) == "" {
		return ImportBatch{}, errors.New("organisation, consent review and purpose are required")
	}
	if strings.ToUpper(strings.TrimSpace(input.Channel)) != "WHATSAPP" {
		return ImportBatch{}, errors.New("initial import channel must be WHATSAPP")
	}
	if strings.TrimSpace(input.WordingVersion) == "" || strings.TrimSpace(input.SourceName) == "" || strings.TrimSpace(input.ObjectKey) == "" {
		return ImportBatch{}, errors.New("wording version, source name and object key are required")
	}
	if strings.TrimSpace(input.OriginalFilename) == "" || strings.TrimSpace(input.DetectedMediaType) == "" || input.ByteSize <= 0 {
		return ImportBatch{}, errors.New("file name, detected media type and positive byte size are required")
	}
	checksum := strings.ToLower(strings.TrimSpace(input.FileSHA256))
	if !sha256Pattern.MatchString(checksum) {
		return ImportBatch{}, errors.New("file SHA-256 must be a 64-character lowercase hexadecimal digest")
	}
	if err := ValidateIdempotencyKey(input.ClientRequestID); err != nil {
		return ImportBatch{}, err
	}
	if strings.TrimSpace(input.UploadedBy) == "" {
		return ImportBatch{}, errors.New("uploader is required")
	}
	if strings.TrimSpace(input.TemplateVersion) == "" {
		return ImportBatch{}, errors.New("template version is required")
	}
	if input.UpdatePolicy == "" {
		input.UpdatePolicy = UpdateNewestSource
	}
	if !validUpdatePolicy(input.UpdatePolicy) {
		return ImportBatch{}, errors.New("unsupported import update policy")
	}
	mapping, err := json.Marshal(input.Mapping)
	if err != nil {
		return ImportBatch{}, fmt.Errorf("marshal import mapping: %w", err)
	}
	if len(mapping) == 0 || len(mapping) > 64<<10 {
		return ImportBatch{}, errors.New("import mapping is empty or exceeds 64 KiB")
	}
	identifier, err := id.New()
	if err != nil {
		return ImportBatch{}, err
	}
	now = now.UTC()
	return ImportBatch{
		ID: identifier, OrganisationID: strings.TrimSpace(input.OrganisationID), ConsentReviewID: strings.TrimSpace(input.ConsentReviewID),
		PurposeID: strings.TrimSpace(input.PurposeID), Channel: "WHATSAPP", WordingVersion: strings.TrimSpace(input.WordingVersion),
		SourceName: strings.TrimSpace(input.SourceName), SourceSystem: strings.TrimSpace(input.SourceSystem), DefaultCountryISO2: strings.ToUpper(strings.TrimSpace(input.DefaultCountryISO2)),
		ObjectKey: strings.TrimSpace(input.ObjectKey), OriginalFilename: strings.TrimSpace(input.OriginalFilename), DetectedMediaType: strings.TrimSpace(input.DetectedMediaType),
		FileSHA256: checksum, ByteSize: input.ByteSize, TemplateVersion: strings.TrimSpace(input.TemplateVersion), MappingDefinitionID: strings.TrimSpace(input.MappingDefinitionID), Mapping: mapping, SourceExpiresAt: input.SourceExpiresAt,
		UpdatePolicy: input.UpdatePolicy, Status: ImportUploaded, MalwareStatus: MalwarePending, ContentSignatureValid: input.ContentSignatureValid,
		ClientRequestID: strings.TrimSpace(input.ClientRequestID), UploadedBy: strings.TrimSpace(input.UploadedBy), Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func ValidateIdempotencyKey(value string) error {
	value = strings.TrimSpace(value)
	if len(value) < 16 || len(value) > 128 {
		return errors.New("idempotency key must contain 16 to 128 characters")
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("-_.:", character) {
			continue
		}
		return errors.New("idempotency key contains unsafe characters")
	}
	return nil
}

func validUpdatePolicy(value UpdatePolicy) bool {
	switch value {
	case UpdateInsertOnly, UpdateFillNull, UpdateNewestSource, UpdateTrustedSource, UpdateManualConflict:
		return true
	default:
		return false
	}
}

func (b ImportBatch) Scan(status MalwareStatus, signatureValid bool, reason string, expectedVersion int64, now time.Time) (ImportBatch, error) {
	if b.Version != expectedVersion {
		return ImportBatch{}, ErrImportConflict
	}
	if b.Status != ImportUploaded && b.Status != ImportScanning && b.Status != ImportFailed {
		return ImportBatch{}, ErrImportTransition
	}
	if status != MalwareClean && status != MalwareInfected && status != MalwareFailed {
		return ImportBatch{}, errors.New("terminal malware scan status is required")
	}
	b.MalwareStatus = status
	b.ContentSignatureValid = signatureValid
	b.FailureReason = strings.TrimSpace(reason)
	if status == MalwareClean && signatureValid {
		b.Status = ImportValidating
		b.FailureReason = ""
	} else {
		b.Status = ImportQuarantined
	}
	b.Version++
	b.UpdatedAt = now.UTC()
	return b, nil
}

func (b ImportBatch) Approve(actorID string, expectedVersion int64, consentEvidenceValid bool, now time.Time) (ImportBatch, error) {
	if b.Version != expectedVersion {
		return ImportBatch{}, ErrImportConflict
	}
	if b.Status != ImportPreviewReady {
		return ImportBatch{}, ErrImportTransition
	}
	if b.MalwareStatus != MalwareClean || !b.ContentSignatureValid {
		return ImportBatch{}, ErrImportScanRequired
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" || actorID == b.UploadedBy {
		return ImportBatch{}, ErrMakerChecker
	}
	if !consentEvidenceValid {
		return ImportBatch{}, ErrImportApprovalEvidence
	}
	approvedAt := now.UTC()
	b.Status = ImportApproved
	b.ApprovedBy = actorID
	b.ApprovedAt = &approvedAt
	b.Version++
	b.UpdatedAt = approvedAt
	return b, nil
}

func (b ImportBatch) Cancel(actorID, reason string, expectedVersion int64, now time.Time) (ImportBatch, error) {
	if b.Version != expectedVersion {
		return ImportBatch{}, ErrImportConflict
	}
	if b.Status != ImportPreviewReady {
		return ImportBatch{}, ErrImportTransition
	}
	actorID, reason = strings.TrimSpace(actorID), strings.TrimSpace(reason)
	if actorID == "" || len(reason) < 8 {
		return ImportBatch{}, ErrImportTransition
	}
	b.Status, b.FailureReason = ImportCancelled, reason
	b.Version++
	b.UpdatedAt = now.UTC()
	return b, nil
}

func isExactScanReplay(current ImportBatch, status MalwareStatus, signatureValid bool, reason string, expectedVersion int64) bool {
	if current.Version != expectedVersion+1 || current.MalwareStatus != status || current.ContentSignatureValid != signatureValid {
		return false
	}
	targetStatus := ImportQuarantined
	targetReason := strings.TrimSpace(reason)
	if status == MalwareClean && signatureValid {
		targetStatus = ImportValidating
		targetReason = ""
	}
	return current.Status == targetStatus && current.FailureReason == targetReason
}

func isExactApprovalReplay(current ImportBatch, actorID string, expectedVersion int64) bool {
	return current.Version == expectedVersion+1 && current.Status == ImportApproved &&
		current.ApprovedBy == strings.TrimSpace(actorID) && current.ApprovedAt != nil
}

func (b ImportBatch) RequestFingerprint() string {
	mapping := canonicalImportJSON(b.Mapping)
	payload, _ := json.Marshal(struct {
		OrganisationID, ConsentReviewID, PurposeID, Channel, WordingVersion      string
		SourceName, SourceSystem, ObjectKey, OriginalFilename, DetectedMediaType string
		FileSHA256, TemplateVersion, MappingDefinitionID                         string
		ByteSize                                                                 int64
		Mapping                                                                  json.RawMessage
		UpdatePolicy                                                             UpdatePolicy
		UploadedBy                                                               string
	}{b.OrganisationID, b.ConsentReviewID, b.PurposeID, b.Channel, b.WordingVersion, b.SourceName, b.SourceSystem, b.ObjectKey, b.OriginalFilename, b.DetectedMediaType, b.FileSHA256, b.TemplateVersion, b.MappingDefinitionID, b.ByteSize, mapping, b.UpdatePolicy, b.UploadedBy})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func canonicalImportJSON(raw json.RawMessage) json.RawMessage {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return normalized
}

func sameImportRequest(left, right ImportBatch) bool {
	return left.ClientRequestID == right.ClientRequestID && left.RequestFingerprint() == right.RequestFingerprint()
}

type ImportRepository interface {
	Create(context.Context, ImportBatch) (ImportBatch, bool, error)
	Get(context.Context, string) (ImportBatch, error)
	RecordScan(context.Context, string, MalwareStatus, bool, string, int64, time.Time) (ImportBatch, error)
	Approve(context.Context, string, string, int64, time.Time) (ImportBatch, error)
}

type ImportService struct {
	Repository    ImportRepository
	Organisations interface {
		Get(context.Context, string) (organisation.Organisation, error)
	}
	Clock func() time.Time
}

func (s *ImportService) Create(ctx context.Context, input CreateImportInput) (ImportBatch, bool, error) {
	if s == nil || s.Repository == nil {
		return ImportBatch{}, false, errors.New("import repository is required")
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	if s.Organisations != nil {
		org, err := s.Organisations.Get(ctx, strings.TrimSpace(input.OrganisationID))
		if err != nil {
			return ImportBatch{}, false, err
		}
		if org.Status != organisation.StatusActive {
			return ImportBatch{}, false, organisation.ErrNotActive
		}
	}
	batch, err := NewImportBatch(input, now)
	if err != nil {
		return ImportBatch{}, false, err
	}
	return s.Repository.Create(ctx, batch)
}
func (s *ImportService) Get(ctx context.Context, identifier string) (ImportBatch, error) {
	if s == nil || s.Repository == nil {
		return ImportBatch{}, errors.New("import repository is required")
	}
	return s.Repository.Get(ctx, identifier)
}
func (s *ImportService) RecordScan(ctx context.Context, identifier string, status MalwareStatus, signatureValid bool, reason string, expectedVersion int64) (ImportBatch, error) {
	if s == nil || s.Repository == nil {
		return ImportBatch{}, errors.New("import repository is required")
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	return s.Repository.RecordScan(ctx, identifier, status, signatureValid, reason, expectedVersion, now)
}
func (s *ImportService) Approve(ctx context.Context, identifier, actorID string, expectedVersion int64) (ImportBatch, error) {
	if s == nil || s.Repository == nil {
		return ImportBatch{}, errors.New("import repository is required")
	}
	if s.Organisations != nil {
		batch, err := s.Repository.Get(ctx, identifier)
		if err != nil {
			return ImportBatch{}, err
		}
		org, err := s.Organisations.Get(ctx, batch.OrganisationID)
		if err != nil {
			return ImportBatch{}, err
		}
		if org.Status != organisation.StatusActive {
			return ImportBatch{}, organisation.ErrNotActive
		}
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	return s.Repository.Approve(ctx, identifier, actorID, expectedVersion, now)
}

func (s *ImportService) Cancel(ctx context.Context, identifier, actorID, reason string, expectedVersion int64) (ImportBatch, error) {
	if s == nil || s.Repository == nil {
		return ImportBatch{}, errors.New("import repository is required")
	}
	repo, ok := s.Repository.(interface {
		Cancel(context.Context, string, string, string, int64, time.Time) (ImportBatch, error)
	})
	if !ok {
		return ImportBatch{}, errors.New("import cancellation is unavailable")
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	return repo.Cancel(ctx, identifier, actorID, reason, expectedVersion, now)
}

type MemoryImportRepository struct {
	mu              sync.Mutex
	items           map[string]ImportBatch
	byRequest       map[string]string
	byFile          map[string]string
	consentEligible map[string]bool
}

func NewMemoryImportRepository() *MemoryImportRepository {
	return &MemoryImportRepository{items: map[string]ImportBatch{}, byRequest: map[string]string{}, byFile: map[string]string{}, consentEligible: map[string]bool{}}
}
func (r *MemoryImportRepository) SetConsentEligible(consentReviewID string, eligible bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.consentEligible[consentReviewID] = eligible
}
func (r *MemoryImportRepository) Create(_ context.Context, batch ImportBatch) (ImportBatch, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	requestKey := batch.OrganisationID + "\x1f" + batch.ClientRequestID
	if identifier, exists := r.byRequest[requestKey]; exists {
		existing := r.items[identifier]
		if !sameImportRequest(existing, batch) {
			return ImportBatch{}, false, ErrImportReplayConflict
		}
		return cloneImport(existing), false, nil
	}
	fileKey := batch.OrganisationID + "\x1f" + batch.FileSHA256
	if identifier, exists := r.byFile[fileKey]; exists {
		return cloneImport(r.items[identifier]), false, ErrDuplicateImportFile
	}
	r.items[batch.ID] = cloneImport(batch)
	r.byRequest[requestKey] = batch.ID
	r.byFile[fileKey] = batch.ID
	return cloneImport(batch), true, nil
}
func (r *MemoryImportRepository) Get(_ context.Context, identifier string) (ImportBatch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.items[identifier]
	if !ok {
		return ImportBatch{}, ErrImportNotFound
	}
	return cloneImport(value), nil
}
func (r *MemoryImportRepository) RecordScan(_ context.Context, identifier string, status MalwareStatus, signatureValid bool, reason string, expectedVersion int64, now time.Time) (ImportBatch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.items[identifier]
	if !ok {
		return ImportBatch{}, ErrImportNotFound
	}
	if isExactScanReplay(current, status, signatureValid, reason, expectedVersion) {
		return cloneImport(current), nil
	}
	next, err := current.Scan(status, signatureValid, reason, expectedVersion, now)
	if err != nil {
		return ImportBatch{}, err
	}
	r.items[identifier] = cloneImport(next)
	return cloneImport(next), nil
}
func (r *MemoryImportRepository) Approve(_ context.Context, identifier, actorID string, expectedVersion int64, now time.Time) (ImportBatch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.items[identifier]
	if !ok {
		return ImportBatch{}, ErrImportNotFound
	}
	if isExactApprovalReplay(current, actorID, expectedVersion) {
		return cloneImport(current), nil
	}
	eligible := r.consentEligible[current.ConsentReviewID]
	next, err := current.Approve(actorID, expectedVersion, eligible, now)
	if err != nil {
		return ImportBatch{}, err
	}
	r.items[identifier] = cloneImport(next)
	return cloneImport(next), nil
}
func (r *MemoryImportRepository) Cancel(_ context.Context, identifier, actorID, reason string, expectedVersion int64, now time.Time) (ImportBatch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.items[identifier]
	if !ok {
		return ImportBatch{}, ErrImportNotFound
	}
	next, err := current.Cancel(actorID, reason, expectedVersion, now)
	if err != nil {
		return ImportBatch{}, err
	}
	r.items[identifier] = cloneImport(next)
	return cloneImport(next), nil
}
func (r *MemoryImportRepository) SetPreviewReady(identifier string, result PreviewResult, expectedVersion int64, now time.Time) (ImportBatch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.items[identifier]
	if !ok {
		return ImportBatch{}, ErrImportNotFound
	}
	if current.Version != expectedVersion {
		return ImportBatch{}, ErrImportConflict
	}
	if current.Status != ImportValidating {
		return ImportBatch{}, ErrImportTransition
	}
	current.Status = ImportPreviewReady
	current.UploadedRows = int64(result.UploadedRows)
	current.ValidRows = int64(result.ValidRows)
	current.InvalidRows = int64(result.InvalidRows)
	current.DuplicateRows = int64(result.DuplicateRows)
	current.Version++
	current.UpdatedAt = now.UTC()
	r.items[identifier] = cloneImport(current)
	return cloneImport(current), nil
}
func cloneImport(value ImportBatch) ImportBatch {
	value.Mapping = append(json.RawMessage(nil), value.Mapping...)
	if value.ApprovedAt != nil {
		v := *value.ApprovedAt
		value.ApprovedAt = &v
	}
	return value
}
