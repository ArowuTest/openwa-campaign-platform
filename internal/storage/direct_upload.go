package storage

import (
	"context"
	"time"
)

// DirectUploadTarget is a short-lived capability to write exactly one
// server-derived object key. Headers are part of the signed request contract and
// must be sent exactly as returned. The target itself is ephemeral and must
// never be persisted in audit evidence or logs.
type DirectUploadTarget struct {
	Method        string            `json:"method"`
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers"`
	ExpectedBytes int64             `json:"expectedBytes"`
	ExpiresAt     time.Time         `json:"expiresAt"`
}

// DirectUploadTargeter is implemented only by object stores that can delegate a
// bounded one-object write without exposing reusable storage credentials.
type DirectUploadTargeter interface {
	DirectUploadEnabled() bool
	CreateDirectUploadTarget(context.Context, string, int64, string, time.Duration) (DirectUploadTarget, error)
}
