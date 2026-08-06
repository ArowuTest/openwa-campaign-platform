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

// NormalizeE164 validates an already international telephone number and removes
// harmless formatting characters. Country-specific national-number conversion
// belongs to the audience import normalizer; gateway callbacks must provide an
// explicit international identity so they cannot be interpreted under an
// ambiguous default country.
func NormalizeE164(value string) (string, error) {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for i, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
			continue
		}
		if r == '+' && i == 0 {
			b.WriteRune(r)
			continue
		}
		if r == ' ' || r == '-' || r == '(' || r == ')' {
			continue
		}
		return "", errors.New("MSISDN contains unsupported characters")
	}
	normalized := b.String()
	if len(normalized) < 9 || len(normalized) > 16 || normalized[0] != '+' || normalized[1] == '0' {
		return "", errors.New("MSISDN must be a valid E.164 number")
	}
	return normalized, nil
}

func Mask(e164 string) string {
	if len(e164) <= 7 {
		return "***"
	}
	return e164[:4] + strings.Repeat("*", len(e164)-8) + e164[len(e164)-4:]
}
