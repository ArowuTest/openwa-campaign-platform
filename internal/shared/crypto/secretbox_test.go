package crypto

import (
	"bytes"
	"testing"
)

func TestSecretBoxRoundTripAndPurposeBinding(t *testing.T) {
	box, err := NewSecretBox(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal("identity:totp:user-1", "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := box.Open("identity:totp:user-1", sealed)
	if err != nil || plain != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("plain=%q err=%v", plain, err)
	}
	if _, err = box.Open("identity:totp:user-2", sealed); err == nil {
		t.Fatal("ciphertext accepted under different purpose")
	}
}
