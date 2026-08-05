package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// SecretBox protects short application secrets with AES-256-GCM. Purpose is
// authenticated additional data, preventing ciphertext substitution between
// users, fields or domains.
type SecretBox struct{ aead cipher.AEAD }

func NewSecretBox(key []byte) (*SecretBox, error) {
	if len(key) != 32 {
		return nil, errors.New("secret-box key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create secret-box cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create secret-box GCM: %w", err)
	}
	return &SecretBox{aead: aead}, nil
}
func NewSecretBoxFromBase64(value string) (*SecretBox, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("decode secret-box key: %w", err)
	}
	return NewSecretBox(key)
}
func (b *SecretBox) Seal(purpose, plaintext string) ([]byte, error) {
	if b == nil || b.aead == nil {
		return nil, errors.New("secret box is not configured")
	}
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		return nil, errors.New("secret-box purpose is required")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate secret-box nonce: %w", err)
	}
	return b.aead.Seal(nonce, nonce, []byte(plaintext), []byte(purpose)), nil
}
func (b *SecretBox) Open(purpose string, ciphertext []byte) (string, error) {
	if b == nil || b.aead == nil {
		return "", errors.New("secret box is not configured")
	}
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		return "", errors.New("secret-box purpose is required")
	}
	if len(ciphertext) < b.aead.NonceSize() {
		return "", errors.New("secret-box ciphertext is too short")
	}
	nonce, payload := ciphertext[:b.aead.NonceSize()], ciphertext[b.aead.NonceSize():]
	plain, err := b.aead.Open(nil, nonce, payload, []byte(purpose))
	if err != nil {
		return "", fmt.Errorf("open secret-box ciphertext: %w", err)
	}
	return string(plain), nil
}
