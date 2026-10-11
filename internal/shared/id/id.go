package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// New returns a UUIDv4-compatible identifier without requiring an external dependency.
func New() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate identifier: %w", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	buf := make([]byte, 36)
	hex.Encode(buf[0:8], raw[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], raw[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], raw[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], raw[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], raw[10:16])
	return string(buf), nil
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsUUID accepts the hyphenated UUID spelling used by the public API.
func IsUUID(value string) bool { return uuidPattern.MatchString(value) }

// CanonicalUUID normalises only UUID-shaped identities. Opaque domain identifiers
// retain their exact spelling; this function does not validate them for an API.
func CanonicalUUID(value string) string {
	if IsUUID(value) {
		return strings.ToLower(value)
	}
	return value
}
