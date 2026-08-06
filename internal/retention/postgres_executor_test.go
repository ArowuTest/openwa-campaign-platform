package retention

import (
	"context"
	"errors"
	"io"
	"testing"

	"campaign-platform/internal/storage"
)

type deletionStore struct{ err error }

func (s deletionStore) Put(context.Context, string, io.Reader, int64) (storage.Metadata, error) {
	return storage.Metadata{}, errors.New("unused")
}
func (s deletionStore) Open(context.Context, string) (storage.ReadSeekCloser, storage.Metadata, error) {
	return nil, storage.Metadata{}, errors.New("unused")
}
func (s deletionStore) Stat(context.Context, string) (storage.Metadata, error) {
	return storage.Metadata{}, errors.New("unused")
}
func (s deletionStore) Delete(context.Context, string) error { return s.err }

func TestDeleteRetentionObjectIsRetrySafe(t *testing.T) {
	alreadyAbsent, err := deleteRetentionObject(context.Background(), deletionStore{err: storage.ErrNotFound}, "exports/report")
	if err != nil || !alreadyAbsent {
		t.Fatalf("missing object should be accepted for recovery: absent=%v err=%v", alreadyAbsent, err)
	}
	alreadyAbsent, err = deleteRetentionObject(context.Background(), deletionStore{}, "exports/report")
	if err != nil || alreadyAbsent {
		t.Fatalf("successful deletion result: absent=%v err=%v", alreadyAbsent, err)
	}
	expected := errors.New("storage unavailable")
	if _, err = deleteRetentionObject(context.Background(), deletionStore{err: expected}, "exports/report"); !errors.Is(err, expected) {
		t.Fatalf("unexpected error: %v", err)
	}
}
