package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"campaign-platform/internal/storage"
)

func checksumHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestUploadSessionServiceCreateIsIdempotentAndConflictSafe(t *testing.T) {
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	repo := NewMemoryUploadSessionRepository()
	store, err := storage.NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := &UploadSessionService{
		Repository:  repo,
		Store:       store,
		PartSize:    5 << 20,
		MaxFileSize: 2 << 30,
		SessionTTL:  24 * time.Hour,
		Clock:       func() time.Time { return now },
	}

	input := validUploadSessionInput()
	input.ExpectedBytes = 6 << 20
	first, created, err := service.Create(context.Background(), input)
	if err != nil || !created {
		t.Fatalf("first create created=%v err=%v", created, err)
	}
	second, created, err := service.Create(context.Background(), input)
	if err != nil || created {
		t.Fatalf("exact replay created=%v err=%v", created, err)
	}
	if second.ID != first.ID || second.Version != first.Version {
		t.Fatalf("exact replay changed identity: first=%s v%d second=%s v%d", first.ID, first.Version, second.ID, second.Version)
	}

	conflict := input
	conflict.ExpectedBytes++
	if _, _, err := service.Create(context.Background(), conflict); !errors.Is(err, ErrUploadSessionReplayConflict) {
		t.Fatalf("expected replay conflict, got %v", err)
	}
}

func TestUploadSessionServicePutPartStoresOnlyBoundedPartAndConvergesReplay(t *testing.T) {
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	repo := NewMemoryUploadSessionRepository()
	store, err := storage.NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := &UploadSessionService{
		Repository:  repo,
		Store:       store,
		PartSize:    5 << 20,
		MaxFileSize: 2 << 30,
		SessionTTL:  24 * time.Hour,
		Clock:       func() time.Time { return now },
	}

	input := validUploadSessionInput()
	input.ExpectedBytes = (5 << 20) + 13
	session, _, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	part := bytes.Repeat([]byte("a"), 5<<20)
	checksum := checksumHex(part)
	updated, changed, err := service.PutPart(context.Background(), session.ID, 1, checksum, bytes.NewReader(part))
	if err != nil || !changed {
		t.Fatalf("put part changed=%v err=%v", changed, err)
	}
	if updated.UploadedBytes != int64(len(part)) || updated.UploadedParts != 1 {
		t.Fatalf("unexpected progress bytes=%d parts=%d", updated.UploadedBytes, updated.UploadedParts)
	}

	object, metadata, err := store.Open(context.Background(), updated.Parts[0].ObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	_ = object.Close()
	if metadata.Size != int64(len(part)) || metadata.SHA256 != checksum {
		t.Fatalf("stored metadata mismatch: %+v", metadata)
	}

	replayed, changed, err := service.PutPart(context.Background(), session.ID, 1, checksum, bytes.NewReader(part))
	if err != nil || changed {
		t.Fatalf("exact replay changed=%v err=%v", changed, err)
	}
	if replayed.Version != updated.Version {
		t.Fatalf("exact replay changed version: %d -> %d", updated.Version, replayed.Version)
	}

	conflicting := bytes.Repeat([]byte("b"), 5<<20)
	if _, _, err := service.PutPart(context.Background(), session.ID, 1, checksumHex(conflicting), bytes.NewReader(conflicting)); !errors.Is(err, storage.ErrKeyConflict) && !errors.Is(err, ErrUploadPartConflict) {
		t.Fatalf("expected immutable object/part conflict, got %v", err)
	}

	oversized := append(append([]byte{}, part...), 'x')
	if _, _, err := service.PutPart(context.Background(), session.ID, 1, checksumHex(oversized), bytes.NewReader(oversized)); err == nil {
		t.Fatal("oversized part must fail")
	}
}

func TestUploadSessionServiceRejectsChecksumMismatchBeforeRecordingPart(t *testing.T) {
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	repo := NewMemoryUploadSessionRepository()
	store, _ := storage.NewFileSystemStore(t.TempDir())
	service := &UploadSessionService{
		Repository:  repo,
		Store:       store,
		PartSize:    5 << 20,
		MaxFileSize: 2 << 30,
		SessionTTL:  24 * time.Hour,
		Clock:       func() time.Time { return now },
	}
	input := validUploadSessionInput()
	input.ExpectedBytes = 5 << 20
	session, _, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	part := bytes.Repeat([]byte("x"), 5<<20)
	if _, _, err := service.PutPart(context.Background(), session.ID, 1, strings.Repeat("0", 64), bytes.NewReader(part)); !errors.Is(err, ErrUploadPartConflict) {
		t.Fatalf("expected checksum conflict, got %v", err)
	}
	fresh, err := service.Get(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.UploadedParts != 0 || fresh.Parts[0].State != UploadPartPending {
		t.Fatalf("checksum mismatch mutated session: %+v", fresh)
	}
}

func TestUploadSessionServiceAllowsIndependentConcurrentPartUploads(t *testing.T) {
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	repo := NewMemoryUploadSessionRepository()
	store, _ := storage.NewFileSystemStore(t.TempDir())
	service := &UploadSessionService{
		Repository:  repo,
		Store:       store,
		PartSize:    5 << 20,
		MaxFileSize: 2 << 30,
		SessionTTL:  24 * time.Hour,
		Clock:       func() time.Time { return now },
	}
	input := validUploadSessionInput()
	input.ExpectedBytes = (5 << 20) + 1
	input.ClientRequestID = "large-import-concurrent-parts-0001"
	session, _, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	part1 := bytes.Repeat([]byte("a"), 5<<20)
	part2 := []byte("z")
	type result struct {
		number int
		err    error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for number, payload := range map[int][]byte{1: part1, 2: part2} {
		wg.Add(1)
		go func(number int, payload []byte) {
			defer wg.Done()
			_, _, err := service.PutPart(context.Background(), session.ID, number, checksumHex(payload), bytes.NewReader(payload))
			results <- result{number: number, err: err}
		}(number, payload)
	}
	wg.Wait()
	close(results)
	for result := range results {
		if result.err != nil {
			t.Fatalf("part %d upload failed: %v", result.number, result.err)
		}
	}
	fresh, err := service.Get(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.UploadedParts != 2 || fresh.UploadedBytes != input.ExpectedBytes {
		t.Fatalf("concurrent progress lost: bytes=%d parts=%d", fresh.UploadedBytes, fresh.UploadedParts)
	}
}

type directUploadTestStore struct {
	store    storage.ObjectStore
	enabled  bool
	lastKey  string
	lastSize int64
	lastSHA  string
}

func (s *directUploadTestStore) Put(ctx context.Context, key string, reader io.Reader, maxBytes int64) (storage.Metadata, error) {
	return s.store.Put(ctx, key, reader, maxBytes)
}
func (s *directUploadTestStore) Open(ctx context.Context, key string) (storage.ReadSeekCloser, storage.Metadata, error) {
	return s.store.Open(ctx, key)
}
func (s *directUploadTestStore) Stat(ctx context.Context, key string) (storage.Metadata, error) {
	return s.store.Stat(ctx, key)
}
func (s *directUploadTestStore) Delete(ctx context.Context, key string) error {
	return s.store.Delete(ctx, key)
}
func (s *directUploadTestStore) DirectUploadEnabled() bool { return s.enabled }
func (s *directUploadTestStore) CreateDirectUploadTarget(_ context.Context, key string, expectedBytes int64, checksum string, ttl time.Duration) (storage.DirectUploadTarget, error) {
	if !s.enabled {
		return storage.DirectUploadTarget{}, errors.New("direct upload disabled")
	}
	s.lastKey, s.lastSize, s.lastSHA = key, expectedBytes, checksum
	return storage.DirectUploadTarget{
		Method: "PUT", URL: "https://upload.example.test/opaque",
		Headers:       map[string]string{"X-Amz-Meta-Sha256": checksum},
		ExpectedBytes: expectedBytes, ExpiresAt: time.Now().Add(ttl),
	}, nil
}

func TestUploadSessionServiceDirectTargetAndConfirm(t *testing.T) {
	now := time.Date(2026, 10, 6, 13, 30, 0, 0, time.UTC)
	repo := NewMemoryUploadSessionRepository()
	files, err := storage.NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &directUploadTestStore{store: files, enabled: true}
	service := &UploadSessionService{
		Repository: repo, Store: store,
		PartSize: 5 << 20, MaxFileSize: 2 << 30, SessionTTL: 24 * time.Hour,
		Clock: func() time.Time { return now },
	}
	input := validUploadSessionInput()
	input.ExpectedBytes = 9
	input.ClientRequestID = "direct-upload-service-0001"
	session, _, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	transports := service.SupportedTransports()
	if len(transports) != 2 || transports[0] != "DIRECT_S3" || transports[1] != "RELAY" {
		t.Fatalf("transports=%v", transports)
	}
	payload := []byte("123456789")
	checksum := checksumHex(payload)
	target, alreadyUploaded, err := service.CreateDirectPartTarget(context.Background(), session.ID, 1, checksum, 5*time.Minute)
	if err != nil || alreadyUploaded {
		t.Fatalf("target alreadyUploaded=%v err=%v", alreadyUploaded, err)
	}
	if target.ExpectedBytes != 9 || store.lastSize != 9 || store.lastSHA != checksum || store.lastKey != session.Parts[0].ObjectKey {
		t.Fatalf("target/store mismatch: target=%+v key=%q size=%d sha=%q", target, store.lastKey, store.lastSize, store.lastSHA)
	}

	if _, err := files.Put(context.Background(), session.Parts[0].ObjectKey, bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	confirmed, changed, err := service.ConfirmDirectPart(context.Background(), session.ID, 1, checksum)
	if err != nil || !changed {
		t.Fatalf("confirm changed=%v err=%v", changed, err)
	}
	if confirmed.UploadedParts != 1 || confirmed.UploadedBytes != 9 {
		t.Fatalf("confirmed progress=%+v", confirmed)
	}
	replay, changed, err := service.ConfirmDirectPart(context.Background(), session.ID, 1, checksum)
	if err != nil || changed || replay.Version != confirmed.Version {
		t.Fatalf("confirm replay changed=%v err=%v version=%d want=%d", changed, err, replay.Version, confirmed.Version)
	}
	_, alreadyUploaded, err = service.CreateDirectPartTarget(context.Background(), session.ID, 1, checksum, 5*time.Minute)
	if err != nil || !alreadyUploaded {
		t.Fatalf("uploaded target replay alreadyUploaded=%v err=%v", alreadyUploaded, err)
	}
}

func TestUploadSessionServiceDirectUploadIsDisabledUnlessStoreExplicitlyAllowsIt(t *testing.T) {
	now := time.Date(2026, 10, 6, 13, 30, 0, 0, time.UTC)
	repo := NewMemoryUploadSessionRepository()
	files, _ := storage.NewFileSystemStore(t.TempDir())
	store := &directUploadTestStore{store: files, enabled: false}
	service := &UploadSessionService{
		Repository: repo, Store: store,
		PartSize: 5 << 20, MaxFileSize: 2 << 30, SessionTTL: 24 * time.Hour,
		Clock: func() time.Time { return now },
	}
	input := validUploadSessionInput()
	input.ExpectedBytes = 9
	input.ClientRequestID = "direct-upload-service-disabled-0001"
	session, _, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	transports := service.SupportedTransports()
	if len(transports) != 1 || transports[0] != "RELAY" {
		t.Fatalf("transports=%v", transports)
	}
	if _, _, err := service.CreateDirectPartTarget(context.Background(), session.ID, 1, strings.Repeat("a", 64), time.Minute); !errors.Is(err, ErrDirectUploadUnavailable) {
		t.Fatalf("expected direct upload unavailable, got %v", err)
	}
}

func TestUploadSessionServiceDirectConfirmRejectsServerMetadataMismatch(t *testing.T) {
	now := time.Date(2026, 10, 6, 13, 30, 0, 0, time.UTC)
	repo := NewMemoryUploadSessionRepository()
	files, _ := storage.NewFileSystemStore(t.TempDir())
	store := &directUploadTestStore{store: files, enabled: true}
	service := &UploadSessionService{
		Repository: repo, Store: store,
		PartSize: 5 << 20, MaxFileSize: 2 << 30, SessionTTL: 24 * time.Hour,
		Clock: func() time.Time { return now },
	}
	input := validUploadSessionInput()
	input.ExpectedBytes = 9
	input.ClientRequestID = "direct-upload-service-mismatch-0001"
	session, _, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	actual := []byte("abcdefghi")
	if _, err := files.Put(context.Background(), session.Parts[0].ObjectKey, bytes.NewReader(actual), int64(len(actual))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ConfirmDirectPart(context.Background(), session.ID, 1, strings.Repeat("0", 64)); !errors.Is(err, ErrUploadPartConflict) {
		t.Fatalf("expected metadata/checksum conflict, got %v", err)
	}
	fresh, _ := service.Get(context.Background(), session.ID)
	if fresh.UploadedParts != 0 {
		t.Fatalf("mismatched direct confirm recorded progress: %+v", fresh)
	}
}

func TestUploadSessionServiceCompleteAndAbortUseDurableOptimisticState(t *testing.T) {
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	repo := NewMemoryUploadSessionRepository()
	store, _ := storage.NewFileSystemStore(t.TempDir())
	service := &UploadSessionService{
		Repository:  repo,
		Store:       store,
		PartSize:    5 << 20,
		MaxFileSize: 2 << 30,
		SessionTTL:  24 * time.Hour,
		Clock:       func() time.Time { return now },
	}
	input := validUploadSessionInput()
	input.ExpectedBytes = 9
	input.ClientRequestID = "large-import-request-small-0001"
	// Domain part-size floor still applies; one short final part is permitted.
	session, _, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("123456789")
	session, _, err = service.PutPart(context.Background(), session.ID, 1, checksumHex(data), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	completed, err := service.Complete(context.Background(), session.ID, session.Version)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != UploadSessionUploaded {
		t.Fatalf("state=%s", completed.State)
	}
	if _, err := service.Abort(context.Background(), completed.ID, "wrong source after completion", completed.Version); err != nil {
		// UPLOADED remains abortable until finaliser/import ownership starts.
		t.Fatalf("abort uploaded session: %v", err)
	}
}
