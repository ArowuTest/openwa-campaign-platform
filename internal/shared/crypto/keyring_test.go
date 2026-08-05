package crypto

import "testing"

func TestSecretKeyringRotation(t *testing.T) {
	old := make([]byte, 32)
	old[0] = 1
	current := make([]byte, 32)
	current[0] = 2
	ring, err := NewSecretKeyring("v2", map[string][]byte{"v1": old, "v2": current})
	if err != nil {
		t.Fatal(err)
	}
	cipher, version, err := ring.Seal("record:1", "secret")
	if err != nil || version != "v2" {
		t.Fatalf("version=%s err=%v", version, err)
	}
	plain, err := ring.Open(version, "record:1", cipher)
	if err != nil || plain != "secret" {
		t.Fatalf("plain=%q err=%v", plain, err)
	}
	if _, err := ring.Open("missing", "record:1", cipher); err == nil {
		t.Fatal("expected missing key error")
	}
}
