package storage

import (
	"errors"
	"net/url"
	"testing"
	"time"
)

func TestSignedMediaURLRoundTripAndExpiry(t *testing.T) {
	now := time.Date(2026, 8, 5, 6, 0, 0, 0, time.UTC)
	secret := []byte("01234567890123456789012345678901")
	signer := URLSigner{BaseURL: "http://control-api:8080/api/v1/internal/media", Secret: secret, Clock: func() time.Time { return now }}
	raw, err := signer.Sign("media/campaign/file.pdf", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(raw)
	q := parsed.Query()
	if err := VerifySignedURL(secret, q.Get("key"), q.Get("expires"), q.Get("signature"), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := VerifySignedURL(secret, q.Get("key"), q.Get("expires"), q.Get("signature"), now.Add(11*time.Minute)); !errors.Is(err, ErrSignedURLExpired) {
		t.Fatalf("expected expiry, got %v", err)
	}
	if err := VerifySignedURL(secret, "other", q.Get("expires"), q.Get("signature"), now); !errors.Is(err, ErrSignedURLInvalid) {
		t.Fatalf("expected tamper rejection, got %v", err)
	}
}
