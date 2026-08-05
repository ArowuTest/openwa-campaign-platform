package crypto

import (
	"bytes"
	"testing"
)

func TestMSISDNProtectionRoundTripAndDeterministicLookup(t *testing.T) {
	protector, err := NewMSISDNProtector(bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := protector.Encrypt("+2348012345678")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := protector.Decrypt(ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "+2348012345678" {
		t.Fatalf("unexpected plaintext %s", plain)
	}
	first := protector.LookupHMAC(plain)
	second := protector.LookupHMAC(plain)
	if !bytes.Equal(first, second) {
		t.Fatal("lookup HMAC must be deterministic")
	}
	if Mask(plain) == plain {
		t.Fatal("masked value exposed the MSISDN")
	}
}
