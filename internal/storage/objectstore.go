package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	ErrInvalidKey  = errors.New("invalid object key")
	ErrNotFound    = errors.New("object not found")
	ErrKeyConflict = errors.New("object key already contains different content")
	ErrTooLarge    = errors.New("object exceeds configured size limit")
)

type Metadata struct {
	Key       string    `json:"key"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"createdAt"`
}

type ReadSeekCloser interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Closer
}

type ObjectStore interface {
	Put(context.Context, string, io.Reader, int64) (Metadata, error)
	Open(context.Context, string) (ReadSeekCloser, Metadata, error)
	Stat(context.Context, string) (Metadata, error)
	Delete(context.Context, string) error
}
