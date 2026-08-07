package envfile

import (
	"os"
	"strings"
	"testing"
)

func TestResolveAcceptsReadOnlyRuntimeSecretMount(t *testing.T) {
	path := strings.TrimSpace(os.Getenv("TEST_READONLY_RUNTIME_SECRET_FILE"))
	if path == "" {
		t.Skip("requires a read-only /run/secrets mount")
	}
	if !strings.HasPrefix(path, "/run/secrets/") {
		t.Fatalf("test fixture must be under /run/secrets, got %q", path)
	}
	if file, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
		file.Close()
		t.Fatal("test fixture is writable; expected a read-only runtime secret mount")
	}
	t.Setenv("TEST_RUNTIME_SECRET", "")
	t.Setenv("TEST_RUNTIME_SECRET_FILE", path)
	if err := Resolve("production", "TEST_RUNTIME_SECRET"); err != nil {
		t.Fatalf("read-only runtime secret should be accepted: %v", err)
	}
	if got := os.Getenv("TEST_RUNTIME_SECRET"); got != "runtime-secret-value" {
		t.Fatalf("unexpected resolved secret %q", got)
	}
}
