//go:build !cgo

package database

// PostgreSQL persistence is intentionally unavailable when CGO is disabled.
// database.Open fails closed with a clear unlinked-driver error rather than
// silently selecting memory persistence.
