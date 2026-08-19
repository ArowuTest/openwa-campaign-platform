package metacloud

import (
	"errors"
	"strings"
	"testing"
)

func TestCredentialSetParsesAndResolvesByKey(t *testing.T) {
	raw := `[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"0123456789abcdef0123456789abcdef","verifyToken":"verify-token-123456"}]`
	set, err := ParseCredentialSet(raw)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := set.Resolve("meta-ng")
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken == "" || credential.AppSecret == "" || credential.VerifyToken == "" {
		t.Fatalf("credential fields were not loaded")
	}
	if _, err = set.Resolve("missing"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("missing credential error=%v", err)
	}
}

func TestCredentialSetRejectsDuplicateKeysWithoutLeakingSecrets(t *testing.T) {
	secret := "token-value-that-must-never-appear-in-errors"
	raw := `[{"key":"dup","accessToken":"` + secret + `","appSecret":"0123456789abcdef0123456789abcdef","verifyToken":"verify-token-123456"},{"key":"dup","accessToken":"another-token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"fedcba9876543210fedcba9876543210","verifyToken":"verify-token-654321"}]`
	_, err := ParseCredentialSet(raw)
	if err == nil {
		t.Fatal("duplicate credential key accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("credential secret leaked in error: %v", err)
	}
}
