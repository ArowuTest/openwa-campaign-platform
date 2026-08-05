package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrSignedURLSecret  = errors.New("media URL signing secret must contain at least 32 bytes")
	ErrSignedURLInvalid = errors.New("signed media URL is invalid")
	ErrSignedURLExpired = errors.New("signed media URL has expired")
)

type URLSigner struct {
	BaseURL string
	Secret  []byte
	Clock   func() time.Time
}

func (s URLSigner) Sign(key string, ttl time.Duration) (string, error) {
	if len(s.Secret) < 32 {
		return "", ErrSignedURLSecret
	}
	if ttl <= 0 || ttl > time.Hour {
		return "", ErrSignedURLInvalid
	}
	base, err := url.Parse(strings.TrimSpace(s.BaseURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", ErrSignedURLInvalid
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 1024 {
		return "", ErrSignedURLInvalid
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	expires := now.Add(ttl).Unix()
	query := base.Query()
	query.Set("key", key)
	query.Set("expires", strconv.FormatInt(expires, 10))
	query.Set("signature", signMedia(s.Secret, key, expires))
	base.RawQuery = query.Encode()
	return base.String(), nil
}

func VerifySignedURL(secret []byte, key, expiresText, signature string, now time.Time) error {
	if len(secret) < 32 {
		return ErrSignedURLSecret
	}
	key = strings.TrimSpace(key)
	signature = strings.TrimSpace(signature)
	if key == "" || len(key) > 1024 || signature == "" {
		return ErrSignedURLInvalid
	}
	expires, err := strconv.ParseInt(strings.TrimSpace(expiresText), 10, 64)
	if err != nil || expires <= 0 {
		return ErrSignedURLInvalid
	}
	if !time.Unix(expires, 0).After(now.UTC()) {
		return ErrSignedURLExpired
	}
	provided, err := hex.DecodeString(signature)
	if err != nil || len(provided) != sha256.Size {
		return ErrSignedURLInvalid
	}
	expected, _ := hex.DecodeString(signMedia(secret, key, expires))
	if !hmac.Equal(provided, expected) {
		return ErrSignedURLInvalid
	}
	return nil
}
func signMedia(secret []byte, key string, expires int64) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("GET\n"))
	_, _ = mac.Write([]byte(key))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(strconv.FormatInt(expires, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}
