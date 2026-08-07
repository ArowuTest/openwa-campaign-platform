package sender

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"

	sharedcrypto "campaign-platform/internal/shared/crypto"
)

type memorySessionProxyStore struct {
	cipher  []byte
	version int64
	action  string
	actor   string
	reason  string
}

func (m *memorySessionProxyStore) ConfigureSessionProxy(_ context.Context, id string, expected int64, ciphertext []byte, actor, reason string) (SessionProxyStatus, error) {
	if id != "session-1" || expected != m.version || len(ciphertext) == 0 {
		return SessionProxyStatus{}, ErrSenderConflict
	}
	m.cipher = append([]byte(nil), ciphertext...)
	m.version++
	m.action, m.actor, m.reason = "CONFIGURED", actor, reason
	return SessionProxyStatus{SessionID: id, Configured: true, Version: m.version}, nil
}
func (m *memorySessionProxyStore) ClearSessionProxy(_ context.Context, id string, expected int64, actor, reason string) (SessionProxyStatus, error) {
	if id != "session-1" || expected != m.version {
		return SessionProxyStatus{}, ErrSenderConflict
	}
	m.cipher = nil
	m.version++
	m.action, m.actor, m.reason = "CLEARED", actor, reason
	return SessionProxyStatus{SessionID: id, Configured: false, Version: m.version}, nil
}

func (m *memorySessionProxyStore) LoadSessionProxy(_ context.Context, id string) ([]byte, int64, error) {
	if id != "session-1" {
		return nil, 0, ErrSenderNotFound
	}
	return append([]byte(nil), m.cipher...), m.version, nil
}

func testProxyKeyring(t *testing.T) *sharedcrypto.SecretKeyring {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	keys, err := sharedcrypto.NewSecretKeyringFromJSON("v1", `{"v1":"`+key+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}
func TestSessionProxyAdministrationEncryptsAuditsAndResolves(t *testing.T) {
	store := &memorySessionProxyStore{version: 4}
	svc := &SessionProxyAdministration{Store: store, Keys: testProxyKeyring(t)}
	cfg := SessionProxyConfiguration{URL: "http://proxy-user:proxy-secret@proxy.example:8080", Type: ProxyHTTP}

	status, err := svc.Configure(context.Background(), "session-1", 4, cfg, "actor-1", "approved stable egress route")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Configured || status.Version != 5 {
		t.Fatalf("unexpected status %#v", status)
	}
	if bytes.Contains(store.cipher, []byte("proxy-secret")) || bytes.Contains(store.cipher, []byte("proxy.example")) {
		t.Fatal("proxy plaintext leaked into persisted ciphertext envelope")
	}
	if store.action != "CONFIGURED" || store.actor != "actor-1" || store.reason == "" {
		t.Fatalf("audit inputs not preserved: %#v", store)
	}

	resolved, err := svc.Resolve(context.Background(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || *resolved != cfg {
		t.Fatalf("resolved=%#v want=%#v", resolved, cfg)
	}
}
func TestSessionProxyAdministrationRejectsUnsafeShapeAndSupportsClear(t *testing.T) {
	store := &memorySessionProxyStore{version: 1}
	svc := &SessionProxyAdministration{Store: store, Keys: testProxyKeyring(t)}
	for _, cfg := range []SessionProxyConfiguration{
		{URL: "ftp://proxy.example:21", Type: ProxyHTTP},
		{URL: "http://proxy.example:8080", Type: "invalid"},
		{URL: "http://", Type: ProxyHTTP},
	} {
		if _, err := svc.Configure(context.Background(), "session-1", 1, cfg, "actor-1", "approved stable route"); !errors.Is(err, ErrSessionProxyInvalid) {
			t.Fatalf("cfg=%#v err=%v", cfg, err)
		}
	}
	if _, err := svc.Configure(context.Background(), "session-1", 1, SessionProxyConfiguration{URL: "socks5://proxy.example:1080", Type: ProxySOCKS5}, "actor-1", "approved stable route"); err != nil {
		t.Fatal(err)
	}
	status, err := svc.Clear(context.Background(), "session-1", 2, "actor-2", "stable route no longer required")
	if err != nil {
		t.Fatal(err)
	}
	if status.Configured || store.action != "CLEARED" {
		t.Fatalf("unexpected clear result %#v action=%s", status, store.action)
	}
	resolved, err := svc.Resolve(context.Background(), "session-1")
	if err != nil || resolved != nil {
		t.Fatalf("resolved=%#v err=%v", resolved, err)
	}
}
