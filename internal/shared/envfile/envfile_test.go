package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLoadsFileAndRejectsAmbiguity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("secret-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_SECRET", "")
	t.Setenv("TEST_SECRET_FILE", path)
	if err := Resolve("production", "TEST_SECRET"); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TEST_SECRET"); got != "secret-value" {
		t.Fatalf("got %q", got)
	}

	t.Setenv("TEST_SECRET", "inline")
	if err := Resolve("production", "TEST_SECRET"); err == nil || !strings.Contains(err.Error(), "cannot both") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
}

func TestResolveRejectsUnsafeDeployedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_UNSAFE", "")
	t.Setenv("TEST_UNSAFE_FILE", path)
	if err := Resolve("production", "TEST_UNSAFE"); err == nil || !strings.Contains(err.Error(), "group or world") {
		t.Fatalf("expected permissions error, got %v", err)
	}
}
