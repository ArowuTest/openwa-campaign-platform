package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"

	"campaign-platform/internal/storage"
)

type UploadCompositeSource struct {
	Store storage.ObjectStore
}

func (s *UploadCompositeSource) Open(ctx context.Context, session UploadSession) (io.ReadCloser, error) {
	if s == nil || s.Store == nil {
		return nil, errors.New("upload composite source object store is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	switch session.State {
	case UploadSessionUploaded, UploadSessionFinalising, UploadSessionImportCreated:
	default:
		return nil, ErrUploadSessionState
	}
	if session.PartCount <= 0 || len(session.Parts) != session.PartCount ||
		session.UploadedParts != session.PartCount ||
		session.ExpectedBytes <= 0 ||
		session.UploadedBytes != session.ExpectedBytes {
		return nil, ErrUploadPartIncomplete
	}
	var accounted int64
	for index, part := range session.Parts {
		if part.Number != index+1 ||
			part.Offset != accounted ||
			part.State != UploadPartUploaded ||
			part.ExpectedBytes <= 0 ||
			part.UploadedBytes != part.ExpectedBytes ||
			part.ObjectKey == "" ||
			!validSHA256(part.SHA256) {
			return nil, ErrUploadPartIncomplete
		}
		accounted += part.ExpectedBytes
	}
	if accounted != session.ExpectedBytes {
		return nil, ErrUploadPartIncomplete
	}
	return &uploadCompositeReader{
		ctx:      ctx,
		store:    s.Store,
		parts:    append([]UploadPart(nil), session.Parts...),
		expected: session.ExpectedBytes,
	}, nil
}

type uploadCompositeReader struct {
	ctx         context.Context
	store       storage.ObjectStore
	parts       []UploadPart
	index       int
	current     storage.ReadSeekCloser
	currentHash hash.Hash
	currentRead int64
	totalRead   int64
	expected    int64
	closed      bool
}

func (r *uploadCompositeReader) Read(p []byte) (int, error) {
	if r == nil || r.closed {
		return 0, errors.New("upload composite source is closed")
	}
	if len(p) == 0 {
		return 0, nil
	}
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		if r.current == nil {
			if r.index >= len(r.parts) {
				if r.totalRead != r.expected {
					return 0, fmt.Errorf("%w: read %d bytes, expected %d", ErrUploadPartIncomplete, r.totalRead, r.expected)
				}
				return 0, io.EOF
			}
			part := r.parts[r.index]
			object, metadata, err := r.store.Open(r.ctx, part.ObjectKey)
			if err != nil {
				return 0, err
			}
			if metadata.Size != part.ExpectedBytes || !equalUploadSHA(metadata.SHA256, part.SHA256) {
				_ = object.Close()
				return 0, ErrUploadPartConflict
			}
			r.current = object
			r.currentHash = sha256.New()
			r.currentRead = 0
		}

		n, err := r.current.Read(p)
		if n > 0 {
			_, _ = r.currentHash.Write(p[:n])
			r.currentRead += int64(n)
			r.totalRead += int64(n)
			if r.currentRead > r.parts[r.index].ExpectedBytes || r.totalRead > r.expected {
				_ = r.current.Close()
				r.current = nil
				return n, ErrUploadPartConflict
			}
		}
		if errors.Is(err, io.EOF) {
			part := r.parts[r.index]
			expectedPart := part.ExpectedBytes
			actualSHA := hex.EncodeToString(r.currentHash.Sum(nil))
			closeErr := r.current.Close()
			r.current = nil
			r.currentHash = nil
			if closeErr != nil {
				return n, closeErr
			}
			if r.currentRead != expectedPart || !equalUploadSHA(actualSHA, part.SHA256) {
				return n, ErrUploadPartConflict
			}
			r.index++
			r.currentRead = 0
			if n > 0 {
				return n, nil
			}
			continue
		}
		if err != nil {
			return n, err
		}
		return n, nil
	}
}

func (r *uploadCompositeReader) Close() error {
	if r == nil || r.closed {
		return nil
	}
	r.closed = true
	if r.current != nil {
		err := r.current.Close()
		r.current = nil
		return err
	}
	return nil
}

func equalUploadSHA(left, right string) bool {
	if len(left) != sha256HexLength || len(right) != sha256HexLength {
		return false
	}
	for index := 0; index < sha256HexLength; index++ {
		l := left[index]
		r := right[index]
		if l >= 'A' && l <= 'F' {
			l += 'a' - 'A'
		}
		if r >= 'A' && r <= 'F' {
			r += 'a' - 'A'
		}
		if l != r {
			return false
		}
	}
	return true
}
