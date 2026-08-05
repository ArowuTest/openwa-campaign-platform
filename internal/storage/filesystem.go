package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileSystemStore is suitable for a dedicated encrypted VPS volume. Object keys
// are always relative, writes are private and atomic, and an existing key can be
// replayed only with byte-identical content.
type FileSystemStore struct {
	Root string
	Now  func() time.Time
}

func NewFileSystemStore(root string) (*FileSystemStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("object store root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve object store root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create object store root: %w", err)
	}
	if err := os.Chmod(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("secure object store root: %w", err)
	}
	return &FileSystemStore{Root: absolute}, nil
}

func (s *FileSystemStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *FileSystemStore) Put(ctx context.Context, key string, reader io.Reader, maxBytes int64) (Metadata, error) {
	if s == nil || strings.TrimSpace(s.Root) == "" {
		return Metadata{}, errors.New("object store is not configured")
	}
	if reader == nil {
		return Metadata{}, errors.New("object reader is required")
	}
	if maxBytes <= 0 {
		return Metadata{}, errors.New("positive object size limit is required")
	}
	target, err := s.path(key)
	if err != nil {
		return Metadata{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return Metadata{}, fmt.Errorf("create object directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".upload-*")
	if err != nil {
		return Metadata{}, fmt.Errorf("create temporary object: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return Metadata{}, err
	}
	hash := sha256.New()
	written, copyErr := copyContext(ctx, io.MultiWriter(temporary, hash), reader, maxBytes)
	if copyErr != nil {
		temporary.Close()
		return Metadata{}, copyErr
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return Metadata{}, fmt.Errorf("sync object: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return Metadata{}, fmt.Errorf("close object: %w", err)
	}
	metadata := Metadata{Key: key, Size: written, SHA256: hex.EncodeToString(hash.Sum(nil)), CreatedAt: s.now()}

	// Hard-linking is an atomic create-if-absent operation on the same volume and
	// avoids os.Rename replacing an existing object on Unix.
	if err := os.Link(temporaryPath, target); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return Metadata{}, fmt.Errorf("publish object: %w", err)
		}
		existing, statErr := s.Stat(ctx, key)
		if statErr != nil {
			return Metadata{}, statErr
		}
		if existing.Size != metadata.Size || existing.SHA256 != metadata.SHA256 {
			return Metadata{}, ErrKeyConflict
		}
		return existing, nil
	}
	if directory, err := os.Open(filepath.Dir(target)); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return metadata, nil
}

func (s *FileSystemStore) Open(ctx context.Context, key string) (ReadSeekCloser, Metadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, Metadata{}, err
	}
	path, err := s.path(key)
	if err != nil {
		return nil, Metadata{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, Metadata{}, ErrNotFound
	}
	if err != nil {
		return nil, Metadata{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, Metadata{}, errors.New("object is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, Metadata{}, err
	}
	metadata, err := metadataForFile(key, file, info.ModTime())
	if err != nil {
		file.Close()
		return nil, Metadata{}, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, Metadata{}, err
	}
	return file, metadata, nil
}

func (s *FileSystemStore) Stat(ctx context.Context, key string) (Metadata, error) {
	object, metadata, err := s.Open(ctx, key)
	if err != nil {
		return Metadata{}, err
	}
	_ = object.Close()
	return metadata, nil
}

func (s *FileSystemStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return nil
}

func (s *FileSystemStore) path(key string) (string, error) {
	key = strings.TrimSpace(strings.ReplaceAll(key, "\\", "/"))
	if key == "" || strings.HasPrefix(key, "/") || strings.ContainsRune(key, '\x00') {
		return "", ErrInvalidKey
	}
	cleaned := filepath.ToSlash(filepath.Clean(key))
	if cleaned != key || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrInvalidKey
	}
	for _, segment := range strings.Split(cleaned, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", ErrInvalidKey
		}
	}
	path := filepath.Join(s.Root, filepath.FromSlash(cleaned))
	relative, err := filepath.Rel(s.Root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrInvalidKey
	}
	return path, nil
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader, maximum int64) (int64, error) {
	buffer := make([]byte, 128<<10)
	limited := io.LimitReader(source, maximum+1)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, readErr := limited.Read(buffer)
		if count > 0 {
			total += int64(count)
			if total > maximum {
				return total, ErrTooLarge
			}
			if _, err := destination.Write(buffer[:count]); err != nil {
				return total, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func metadataForFile(key string, file *os.File, createdAt time.Time) (Metadata, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Metadata{}, err
	}
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return Metadata{}, err
	}
	return Metadata{Key: key, Size: size, SHA256: hex.EncodeToString(hash.Sum(nil)), CreatedAt: createdAt.UTC()}, nil
}
