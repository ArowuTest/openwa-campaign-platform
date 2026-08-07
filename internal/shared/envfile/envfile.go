package envfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Resolve loads NAME from NAME_FILE for the supplied names. It rejects ambiguous
// dual configuration, non-absolute deployed paths, unsafe ordinary-file permissions,
// writable runtime-secret mounts, empty files, and oversized values.
func Resolve(environment string, names ...string) error {
	deployed := environment == "staging" || environment == "production"
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		direct, directSet := os.LookupEnv(name)
		fileName := name + "_FILE"
		path, fileSet := os.LookupEnv(fileName)
		path = strings.TrimSpace(path)
		if directSet && strings.TrimSpace(direct) != "" && fileSet && path != "" {
			return fmt.Errorf("%s and %s cannot both be configured", name, fileName)
		}
		if !fileSet || path == "" {
			continue
		}
		if deployed && !filepath.IsAbs(path) {
			return fmt.Errorf("%s must be an absolute path in deployed environments", fileName)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", fileName, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s must reference a regular file", fileName)
		}
		if info.Size() > 1<<20 {
			return fmt.Errorf("%s exceeds the 1 MiB limit", fileName)
		}
		if deployed && info.Mode().Perm()&0o077 != 0 && !readOnlyRuntimeSecret(path) {
			return fmt.Errorf("%s must not be group or world accessible", fileName)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", fileName, err)
		}
		value := strings.TrimRight(string(raw), "\r\n")
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is empty", fileName)
		}
		if strings.IndexByte(value, 0) >= 0 {
			return errors.New(fileName + " contains a NUL byte")
		}
		if err := os.Setenv(name, value); err != nil {
			return fmt.Errorf("set %s from %s: %w", name, fileName, err)
		}
	}
	return nil
}

func readOnlyRuntimeSecret(path string) bool {
	clean := filepath.Clean(path)
	if filepath.Dir(clean) != "/run/secrets" {
		return false
	}
	file, err := os.OpenFile(clean, os.O_WRONLY, 0)
	if err == nil {
		_ = file.Close()
		return false
	}
	return errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EROFS)
}
