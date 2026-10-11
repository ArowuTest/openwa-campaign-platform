package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/storage"
)

var (
	ErrUploadSessionReplayConflict = errors.New("audience import upload session idempotency key was reused with different content")
	ErrDirectUploadUnavailable     = errors.New("direct audience upload is unavailable")
)

type UploadSessionRepository interface {
	Create(context.Context, UploadSession) (UploadSession, bool, error)
	Get(context.Context, string) (UploadSession, error)
	RecordPart(context.Context, string, int, int64, string, time.Time) (UploadSession, bool, error)
	Complete(context.Context, string, int64, time.Time) (UploadSession, error)
	Abort(context.Context, string, string, int64, time.Time) (UploadSession, error)
}

type UploadSessionService struct {
	Repository  UploadSessionRepository
	Store       storage.ObjectStore
	PartSize    int64
	MaxFileSize int64
	SessionTTL  time.Duration
	Clock       func() time.Time
}

func (s *UploadSessionService) now() time.Time {
	if s != nil && s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func (s *UploadSessionService) limits() (partSize, maximum int64, ttl time.Duration) {
	partSize = s.PartSize
	if partSize <= 0 {
		partSize = DefaultUploadPartSize
	}
	maximum = s.MaxFileSize
	if maximum <= 0 {
		maximum = 512 << 20
	}
	ttl = s.SessionTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return
}

func (s *UploadSessionService) Create(ctx context.Context, input UploadSessionInput) (UploadSession, bool, error) {
	if s == nil || s.Repository == nil {
		return UploadSession{}, false, errors.New("upload session repository is required")
	}
	partSize, maximum, ttl := s.limits()
	now := s.now()
	session, err := NewUploadSession(input, partSize, maximum, now.Add(ttl), now)
	if err != nil {
		return UploadSession{}, false, err
	}
	return s.Repository.Create(ctx, session)
}

func (s *UploadSessionService) Get(ctx context.Context, identifier string) (UploadSession, error) {
	if s == nil || s.Repository == nil {
		return UploadSession{}, errors.New("upload session repository is required")
	}
	return s.Repository.Get(ctx, strings.TrimSpace(identifier))
}

func (s *UploadSessionService) SupportedTransports() []string {
	if s != nil && s.Store != nil {
		if targeter, ok := s.Store.(storage.DirectUploadTargeter); ok && targeter.DirectUploadEnabled() {
			return []string{"DIRECT_S3", "RELAY"}
		}
	}
	return []string{"RELAY"}
}

func (s *UploadSessionService) CreateDirectPartTarget(ctx context.Context, identifier string, number int, checksum string, ttl time.Duration) (storage.DirectUploadTarget, bool, error) {
	if s == nil || s.Repository == nil || s.Store == nil {
		return storage.DirectUploadTarget{}, false, errors.New("upload session repository and object store are required")
	}
	targeter, ok := s.Store.(storage.DirectUploadTargeter)
	if !ok || !targeter.DirectUploadEnabled() {
		return storage.DirectUploadTarget{}, false, ErrDirectUploadUnavailable
	}
	current, err := s.Repository.Get(ctx, strings.TrimSpace(identifier))
	if err != nil {
		return storage.DirectUploadTarget{}, false, err
	}
	if number < 1 || number > len(current.Parts) {
		return storage.DirectUploadTarget{}, false, ErrUploadPartOutOfRange
	}
	checksum = strings.ToLower(strings.TrimSpace(checksum))
	if !validSHA256(checksum) {
		return storage.DirectUploadTarget{}, false, ErrUploadPartInvalid
	}
	now := s.now()
	if !now.Before(current.ExpiresAt) {
		return storage.DirectUploadTarget{}, false, ErrUploadSessionExpired
	}
	if current.State != UploadSessionCreated && current.State != UploadSessionUploading {
		return storage.DirectUploadTarget{}, false, ErrUploadSessionState
	}
	part := current.Parts[number-1]
	if part.State == UploadPartUploaded {
		if part.UploadedBytes == part.ExpectedBytes && strings.EqualFold(part.SHA256, checksum) {
			return storage.DirectUploadTarget{}, true, nil
		}
		return storage.DirectUploadTarget{}, false, ErrUploadPartConflict
	}
	target, err := targeter.CreateDirectUploadTarget(ctx, part.ObjectKey, part.ExpectedBytes, checksum, ttl)
	if err != nil {
		return storage.DirectUploadTarget{}, false, err
	}
	if target.ExpectedBytes != part.ExpectedBytes || target.Method == "" || strings.TrimSpace(target.URL) == "" || !target.ExpiresAt.After(now) {
		return storage.DirectUploadTarget{}, false, errors.New("direct upload target is incomplete or inconsistent")
	}
	return target, false, nil
}

func (s *UploadSessionService) ConfirmDirectPart(ctx context.Context, identifier string, number int, checksum string) (UploadSession, bool, error) {
	if s == nil || s.Repository == nil || s.Store == nil {
		return UploadSession{}, false, errors.New("upload session repository and object store are required")
	}
	targeter, ok := s.Store.(storage.DirectUploadTargeter)
	if !ok || !targeter.DirectUploadEnabled() {
		return UploadSession{}, false, ErrDirectUploadUnavailable
	}
	current, err := s.Repository.Get(ctx, strings.TrimSpace(identifier))
	if err != nil {
		return UploadSession{}, false, err
	}
	if number < 1 || number > len(current.Parts) {
		return UploadSession{}, false, ErrUploadPartOutOfRange
	}
	checksum = strings.ToLower(strings.TrimSpace(checksum))
	if !validSHA256(checksum) {
		return UploadSession{}, false, ErrUploadPartInvalid
	}
	part := current.Parts[number-1]
	if part.State == UploadPartUploaded {
		if part.UploadedBytes == part.ExpectedBytes && strings.EqualFold(part.SHA256, checksum) {
			return current, false, nil
		}
		return UploadSession{}, false, ErrUploadPartConflict
	}
	now := s.now()
	if !now.Before(current.ExpiresAt) {
		return UploadSession{}, false, ErrUploadSessionExpired
	}
	if current.State != UploadSessionCreated && current.State != UploadSessionUploading {
		return UploadSession{}, false, ErrUploadSessionState
	}
	metadata, err := s.Store.Stat(ctx, part.ObjectKey)
	if err != nil {
		return UploadSession{}, false, err
	}
	if metadata.Size != part.ExpectedBytes || !strings.EqualFold(metadata.SHA256, checksum) {
		return UploadSession{}, false, ErrUploadPartConflict
	}
	return s.Repository.RecordPart(ctx, current.ID, number, metadata.Size, checksum, now)
}

// PutPart is the bounded relay fallback for an upload-session part. It reads at
// most one configured part into memory, verifies the caller-provided SHA-256,
// then publishes immutable bytes to quarantine storage and records durable part
// evidence. Direct-to-object-store adapters converge on the same RecordPart
// repository transition without using this relay.
func (s *UploadSessionService) PutPart(ctx context.Context, identifier string, number int, expectedSHA256 string, source io.Reader) (UploadSession, bool, error) {
	if s == nil || s.Repository == nil || s.Store == nil {
		return UploadSession{}, false, errors.New("upload session repository and object store are required")
	}
	if source == nil {
		return UploadSession{}, false, errors.New("upload part source is required")
	}
	current, err := s.Repository.Get(ctx, strings.TrimSpace(identifier))
	if err != nil {
		return UploadSession{}, false, err
	}
	if number < 1 || number > len(current.Parts) {
		return UploadSession{}, false, ErrUploadPartOutOfRange
	}
	expectedSHA256 = strings.ToLower(strings.TrimSpace(expectedSHA256))
	if !validSHA256(expectedSHA256) {
		return UploadSession{}, false, ErrUploadPartInvalid
	}
	part := current.Parts[number-1]
	if part.State == UploadPartUploaded {
		if part.UploadedBytes == part.ExpectedBytes && strings.EqualFold(part.SHA256, expectedSHA256) {
			return current, false, nil
		}
		return UploadSession{}, false, ErrUploadPartConflict
	}
	now := s.now()
	if !now.Before(current.ExpiresAt) {
		return UploadSession{}, false, ErrUploadSessionExpired
	}
	if current.State != UploadSessionCreated && current.State != UploadSessionUploading {
		return UploadSession{}, false, ErrUploadSessionState
	}

	// The relay is intentionally bounded to one part. This prevents a large
	// source from becoming a control-api memory or temporary-disk obligation.
	payload, err := io.ReadAll(io.LimitReader(source, part.ExpectedBytes+1))
	if err != nil {
		return UploadSession{}, false, err
	}
	if int64(len(payload)) != part.ExpectedBytes {
		return UploadSession{}, false, fmt.Errorf("%w: part %d requires exactly %d bytes, got %d", ErrUploadPartInvalid, number, part.ExpectedBytes, len(payload))
	}
	sum := sha256.Sum256(payload)
	actualSHA256 := hex.EncodeToString(sum[:])
	if actualSHA256 != expectedSHA256 {
		return UploadSession{}, false, ErrUploadPartConflict
	}
	metadata, err := s.Store.Put(ctx, part.ObjectKey, bytes.NewReader(payload), part.ExpectedBytes)
	if err != nil {
		return UploadSession{}, false, err
	}
	if metadata.Size != part.ExpectedBytes || !strings.EqualFold(metadata.SHA256, expectedSHA256) {
		return UploadSession{}, false, errors.New("stored upload part metadata does not match verified part evidence")
	}
	return s.Repository.RecordPart(ctx, current.ID, number, metadata.Size, metadata.SHA256, now)
}

func (s *UploadSessionService) Complete(ctx context.Context, identifier string, expectedVersion int64) (UploadSession, error) {
	if s == nil || s.Repository == nil {
		return UploadSession{}, errors.New("upload session repository is required")
	}
	return s.Repository.Complete(ctx, strings.TrimSpace(identifier), expectedVersion, s.now())
}

func (s *UploadSessionService) Abort(ctx context.Context, identifier, reason string, expectedVersion int64) (UploadSession, error) {
	if s == nil || s.Repository == nil {
		return UploadSession{}, errors.New("upload session repository is required")
	}
	return s.Repository.Abort(ctx, strings.TrimSpace(identifier), reason, expectedVersion, s.now())
}

type MemoryUploadSessionRepository struct {
	mu        sync.Mutex
	items     map[string]UploadSession
	byRequest map[string]string
}

func NewMemoryUploadSessionRepository() *MemoryUploadSessionRepository {
	return &MemoryUploadSessionRepository{items: map[string]UploadSession{}, byRequest: map[string]string{}}
}

func (r *MemoryUploadSessionRepository) Create(_ context.Context, session UploadSession) (UploadSession, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := session.OrganisationID + "\x1f" + session.ClientRequestID
	if identifier, exists := r.byRequest[key]; exists {
		existing := r.items[identifier]
		if existing.RequestFingerprint() != session.RequestFingerprint() {
			return UploadSession{}, false, ErrUploadSessionReplayConflict
		}
		return cloneUploadSession(existing), false, nil
	}
	r.items[session.ID] = cloneUploadSession(session)
	r.byRequest[key] = session.ID
	return cloneUploadSession(session), true, nil
}

func (r *MemoryUploadSessionRepository) Get(_ context.Context, identifier string) (UploadSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, exists := r.items[identifier]
	if !exists {
		return UploadSession{}, ErrUploadSessionNotFound
	}
	return cloneUploadSession(value), nil
}

func (r *MemoryUploadSessionRepository) RecordPart(_ context.Context, identifier string, number int, size int64, checksum string, now time.Time) (UploadSession, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.items[identifier]
	if !exists {
		return UploadSession{}, false, ErrImportNotFound
	}
	next, changed, err := current.RecordPart(number, size, checksum, current.Version, now)
	if err != nil {
		return UploadSession{}, false, err
	}
	if changed {
		r.items[identifier] = cloneUploadSession(next)
	}
	return cloneUploadSession(next), changed, nil
}

func (r *MemoryUploadSessionRepository) Complete(_ context.Context, identifier string, expectedVersion int64, now time.Time) (UploadSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.items[identifier]
	if !exists {
		return UploadSession{}, ErrUploadSessionNotFound
	}
	next, err := current.Complete(expectedVersion, now)
	if err != nil {
		return UploadSession{}, err
	}
	r.items[identifier] = cloneUploadSession(next)
	return cloneUploadSession(next), nil
}

func (r *MemoryUploadSessionRepository) Abort(_ context.Context, identifier, reason string, expectedVersion int64, now time.Time) (UploadSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.items[identifier]
	if !exists {
		return UploadSession{}, ErrUploadSessionNotFound
	}
	next, err := current.Abort(reason, expectedVersion, now)
	if err != nil {
		return UploadSession{}, err
	}
	r.items[identifier] = cloneUploadSession(next)
	return cloneUploadSession(next), nil
}

func (s UploadSession) RequestFingerprint() string {
	mapping, _ := json.Marshal(s.Mapping)
	payload, _ := json.Marshal(struct {
		OrganisationID, ConsentReviewID, PurposeID, Channel, WordingVersion string
		SourceName, SourceSystem, DefaultCountryISO2, OriginalFilename      string
		TemplateVersion, MappingDefinitionID, UploadedBy, ClientRequestID   string
		ExpectedBytes, PartSize                                             int64
		PartCount                                                           int
		UpdatePolicy                                                        UpdatePolicy
		Mapping                                                             json.RawMessage
	}{
		s.OrganisationID, s.ConsentReviewID, s.PurposeID, s.Channel, s.WordingVersion,
		s.SourceName, s.SourceSystem, s.DefaultCountryISO2, s.OriginalFilename,
		s.TemplateVersion, s.MappingDefinitionID, s.UploadedBy, s.ClientRequestID,
		s.ExpectedBytes, s.PartSize, s.PartCount, s.UpdatePolicy, mapping,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
