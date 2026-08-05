package identity

import (
	"crypto/hmac"
	"crypto/sha1" // TOTP interoperability requires HMAC-SHA1 unless a different algorithm is provisioned.
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

func VerifyTOTP(secret, code string, now time.Time) bool {
	normalised := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalised)
	if err != nil || len(key) < 10 || len(code) != 6 {
		return false
	}
	counter := now.UTC().Unix() / 30
	for delta := int64(-1); delta <= 1; delta++ {
		if hmac.Equal([]byte(totpCode(key, counter+delta)), []byte(code)) {
			return true
		}
	}
	return false
}

func totpCode(key []byte, counter int64) string {
	var payload [8]byte
	binary.BigEndian.PutUint64(payload[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(payload[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	value := (uint32(digest[offset])&0x7f)<<24 |
		uint32(digest[offset+1])<<16 |
		uint32(digest[offset+2])<<8 |
		uint32(digest[offset+3])
	return fmt.Sprintf("%06d", value%1_000_000)
}
