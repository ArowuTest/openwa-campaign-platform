package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFileSystemStoreIsAtomicAndReplaySafe(t *testing.T) {
	store, err := NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Put(context.Background(), "imports/aa/object.bin", strings.NewReader("payload"), 100)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := store.Put(context.Background(), "imports/aa/object.bin", strings.NewReader("payload"), 100)
	if err != nil || replay.SHA256 != first.SHA256 || replay.Size != first.Size {
		t.Fatalf("exact object replay changed evidence: first=%+v replay=%+v err=%v", first, replay, err)
	}
	if _, err := store.Put(context.Background(), "imports/aa/object.bin", strings.NewReader("different"), 100); !errors.Is(err, ErrKeyConflict) {
		t.Fatalf("expected key conflict, got %v", err)
	}
	object, metadata, err := store.Open(context.Background(), first.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer object.Close()
	if metadata.SHA256 != first.SHA256 {
		t.Fatalf("stored checksum changed: %+v", metadata)
	}
}

func TestFileSystemStoreRejectsTraversalAndOversize(t *testing.T) {
	store, err := NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../secret", "/absolute", "a/../../b", "a\\..\\b"} {
		if _, err := store.Put(context.Background(), key, strings.NewReader("x"), 10); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("key %q was not rejected: %v", key, err)
		}
	}
	if _, err := store.Put(context.Background(), "large.bin", strings.NewReader("123456"), 5); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected size rejection, got %v", err)
	}
}
