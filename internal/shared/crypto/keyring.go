package crypto

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// SecretKeyring supports versioned application-encryption keys and controlled
// rotation. Ciphertext records retain the version used so older keys can be
// kept only for the bounded re-encryption window.
type SecretKeyring struct {
	active string
	boxes  map[string]*SecretBox
}

func NewSecretKeyring(active string, keys map[string][]byte) (*SecretKeyring, error) {
	active = strings.TrimSpace(active)
	if active == "" {
		return nil, errors.New("active key version is required")
	}
	if len(keys) == 0 {
		return nil, errors.New("at least one key is required")
	}
	boxes := make(map[string]*SecretBox, len(keys))
	for version, key := range keys {
		version = strings.TrimSpace(version)
		if version == "" {
			return nil, errors.New("key version is required")
		}
		box, err := NewSecretBox(key)
		if err != nil {
			return nil, fmt.Errorf("key %s: %w", version, err)
		}
		boxes[version] = box
	}
	if boxes[active] == nil {
		return nil, fmt.Errorf("active key version %q is not configured", active)
	}
	return &SecretKeyring{active: active, boxes: boxes}, nil
}

func NewSecretKeyringFromJSON(active, value string) (*SecretKeyring, error) {
	var encoded map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(value)), &encoded); err != nil {
		return nil, fmt.Errorf("decode keyring JSON: %w", err)
	}
	keys := make(map[string][]byte, len(encoded))
	for version, text := range encoded {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
		if err != nil {
			return nil, fmt.Errorf("decode key %s: %w", version, err)
		}
		keys[version] = key
	}
	return NewSecretKeyring(active, keys)
}

func (k *SecretKeyring) ActiveVersion() string {
	if k == nil {
		return ""
	}
	return k.active
}
func (k *SecretKeyring) Versions() []string {
	if k == nil {
		return nil
	}
	out := make([]string, 0, len(k.boxes))
	for v := range k.boxes {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
func (k *SecretKeyring) Seal(purpose, plaintext string) ([]byte, string, error) {
	if k == nil || k.boxes[k.active] == nil {
		return nil, "", errors.New("keyring is not configured")
	}
	cipher, err := k.boxes[k.active].Seal(purpose, plaintext)
	return cipher, k.active, err
}
func (k *SecretKeyring) Open(version, purpose string, ciphertext []byte) (string, error) {
	if k == nil {
		return "", errors.New("keyring is not configured")
	}
	box := k.boxes[strings.TrimSpace(version)]
	if box == nil {
		return "", fmt.Errorf("unknown key version %q", version)
	}
	return box.Open(purpose, ciphertext)
}
