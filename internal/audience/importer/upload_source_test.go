package importer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/storage"
)

func TestUploadCompositeSourceStreamsPartsInOrderAndVerifiesManifest(t *testing.T) {
	store, err := storage.NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first := []byte("msisdn,country\n+2348011111111,NG\n")
	second := []byte("+2348022222222,NG\n")
	meta1, err := store.Put(ctx, "imports/uploads/test/part-000001.bin", strings.NewReader(string(first)), int64(len(first)))
	if err != nil {
		t.Fatal(err)
	}
	meta2, err := store.Put(ctx, "imports/uploads/test/part-000002.bin", strings.NewReader(string(second)), int64(len(second)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	session := UploadSession{
		ID:            "11111111-1111-4111-8111-111111111111",
		State:         UploadSessionFinalising,
		ExpectedBytes: int64(len(first) + len(second)),
		UploadedBytes: int64(len(first) + len(second)),
		PartCount:     2,
		UploadedParts: 2,
		Parts: []UploadPart{
			{Number: 1, Offset: 0, ExpectedBytes: meta1.Size, UploadedBytes: meta1.Size, ObjectKey: meta1.Key, SHA256: meta1.SHA256, State: UploadPartUploaded, UploadedAt: &now},
			{Number: 2, Offset: meta1.Size, ExpectedBytes: meta2.Size, UploadedBytes: meta2.Size, ObjectKey: meta2.Key, SHA256: meta2.SHA256, State: UploadPartUploaded, UploadedAt: &now},
		},
	}
	source := &UploadCompositeSource{Store: store}
	reader, err := source.Open(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("read=%v close=%v", err, closeErr)
	}
	want := string(first) + string(second)
	if string(payload) != want {
		t.Fatalf("payload mismatch: %q", string(payload))
	}
}

func TestUploadCompositeSourceRejectsTamperedPartEvidence(t *testing.T) {
	store, _ := storage.NewFileSystemStore(t.TempDir())
	ctx := context.Background()
	data := []byte("abc")
	meta, err := store.Put(ctx, "imports/uploads/test/part-000001.bin", strings.NewReader("abc"), 3)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	session := UploadSession{
		ID:            "11111111-1111-4111-8111-111111111111",
		State:         UploadSessionUploaded,
		ExpectedBytes: 3,
		UploadedBytes: 3,
		PartCount:     1,
		UploadedParts: 1,
		Parts: []UploadPart{{
			Number: 1, Offset: 0, ExpectedBytes: 3, UploadedBytes: 3,
			ObjectKey: meta.Key, SHA256: strings.Repeat("f", 64), State: UploadPartUploaded, UploadedAt: &now,
		}},
	}
	_ = data
	reader, err := (&UploadCompositeSource{Store: store}).Open(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if !errors.Is(readErr, ErrUploadPartConflict) {
		t.Fatalf("expected part evidence conflict, got %v", readErr)
	}
}

type lyingUploadObjectStore struct {
	body     []byte
	metadata storage.Metadata
}

type uploadBytesReadSeekCloser struct{ *bytes.Reader }

func (r *uploadBytesReadSeekCloser) Close() error { return nil }

func (s *lyingUploadObjectStore) Put(context.Context, string, io.Reader, int64) (storage.Metadata, error) {
	return storage.Metadata{}, errors.New("not implemented")
}
func (s *lyingUploadObjectStore) Open(context.Context, string) (storage.ReadSeekCloser, storage.Metadata, error) {
	return &uploadBytesReadSeekCloser{Reader: bytes.NewReader(s.body)}, s.metadata, nil
}
func (s *lyingUploadObjectStore) Stat(context.Context, string) (storage.Metadata, error) {
	return s.metadata, nil
}
func (s *lyingUploadObjectStore) Delete(context.Context, string) error { return nil }

func TestUploadCompositeSourceRejectsBodyWhoseBytesDoNotMatchManifestEvenWhenMetadataLies(t *testing.T) {
	expected := []byte("abc")
	actual := []byte("xyz")
	expectedSHA := checksumHex(expected)
	now := time.Now().UTC()
	store := &lyingUploadObjectStore{
		body: actual,
		metadata: storage.Metadata{
			Key: "imports/uploads/test/part-000001.bin", Size: 3, SHA256: expectedSHA, CreatedAt: now,
		},
	}
	session := UploadSession{
		ID: "11111111-1111-4111-8111-111111111111", State: UploadSessionUploaded,
		ExpectedBytes: 3, UploadedBytes: 3, PartCount: 1, UploadedParts: 1,
		Parts: []UploadPart{{
			Number: 1, Offset: 0, ExpectedBytes: 3, UploadedBytes: 3,
			ObjectKey: store.metadata.Key, SHA256: expectedSHA, State: UploadPartUploaded, UploadedAt: &now,
		}},
	}
	reader, err := (&UploadCompositeSource{Store: store}).Open(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if !errors.Is(readErr, ErrUploadPartConflict) {
		t.Fatalf("expected byte-level SHA conflict, got %v", readErr)
	}
}

func TestUploadCompositeSourceRejectsIncompleteManifestBeforeReading(t *testing.T) {
	store, _ := storage.NewFileSystemStore(t.TempDir())
	session := UploadSession{
		ID:            "11111111-1111-4111-8111-111111111111",
		State:         UploadSessionUploaded,
		ExpectedBytes: 10,
		UploadedBytes: 5,
		PartCount:     2,
		UploadedParts: 1,
		Parts:         []UploadPart{{Number: 1, ExpectedBytes: 5, UploadedBytes: 5, State: UploadPartUploaded, SHA256: strings.Repeat("a", 64), ObjectKey: "one"}},
	}
	if _, err := (&UploadCompositeSource{Store: store}).Open(context.Background(), session); !errors.Is(err, ErrUploadPartIncomplete) {
		t.Fatalf("expected incomplete manifest, got %v", err)
	}
}
