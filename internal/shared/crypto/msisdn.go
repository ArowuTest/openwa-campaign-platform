package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

type MSISDNProtector struct {
	aead      cipher.AEAD
	lookupKey []byte
}

func NewMSISDNProtector(encryptionKey, lookupKey []byte) (*MSISDNProtector, error) {
	if len(encryptionKey) != 32 {
		return nil, errors.New("MSISDN encryption key must be exactly 32 bytes")
	}
	if len(lookupKey) < 32 {
		return nil, errors.New("MSISDN lookup key must be at least 32 bytes")
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM cipher: %w", err)
	}
	return &MSISDNProtector{aead: aead, lookupKey: append([]byte(nil), lookupKey...)}, nil
}

func NewMSISDNProtectorFromBase64(encryptionKey, lookupKey string) (*MSISDNProtector, error) {
	enc, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encryptionKey))
	if err != nil {
		return nil, fmt.Errorf("decode encryption key: %w", err)
	}
	lookup, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lookupKey))
	if err != nil {
		return nil, fmt.Errorf("decode lookup key: %w", err)
	}
	return NewMSISDNProtector(enc, lookup)
}

func (p *MSISDNProtector) Encrypt(e164 string) ([]byte, error) {
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return p.aead.Seal(nonce, nonce, []byte(e164), nil), nil
}

func (p *MSISDNProtector) Decrypt(ciphertext []byte) (string, error) {
	if len(ciphertext) < p.aead.NonceSize() {
		return "", errors.New("ciphertext is too short")
	}
	nonce, payload := ciphertext[:p.aead.NonceSize()], ciphertext[p.aead.NonceSize():]
	plain, err := p.aead.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt MSISDN: %w", err)
	}
	return string(plain), nil
}

func (p *MSISDNProtector) LookupHMAC(e164 string) []byte {
	mac := hmac.New(sha256.New, p.lookupKey)
	_, _ = mac.Write([]byte(e164))
	return mac.Sum(nil)
}

func Mask(e164 string) string {
	if len(e164) <= 7 {
		return "***"
	}
	return e164[:4] + strings.Repeat("*", len(e164)-8) + e164[len(e164)-4:]
}
