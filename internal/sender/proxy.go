package sender

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	sharedcrypto "campaign-platform/internal/shared/crypto"
)

type ProxyType string

const (
	ProxyHTTP   ProxyType = "http"
	ProxyHTTPS  ProxyType = "https"
	ProxySOCKS4 ProxyType = "socks4"
	ProxySOCKS5 ProxyType = "socks5"
)

var ErrSessionProxyInvalid = errors.New("sender session proxy configuration is invalid")

type SessionProxyConfiguration struct {
	URL  string    `json:"url"`
	Type ProxyType `json:"type"`
}
type SessionProxyStatus struct {
	SessionID  string `json:"sessionId"`
	Configured bool   `json:"configured"`
	Version    int64  `json:"version"`
}

type SessionProxyStore interface {
	ConfigureSessionProxy(context.Context, string, int64, []byte, string, string) (SessionProxyStatus, error)
	ClearSessionProxy(context.Context, string, int64, string, string) (SessionProxyStatus, error)
	LoadSessionProxy(context.Context, string) ([]byte, int64, error)
}

type SessionProxyAdministration struct {
	Store SessionProxyStore
	Keys  *sharedcrypto.SecretKeyring
}

type sessionProxyEnvelope struct {
	SchemaVersion int    `json:"schemaVersion"`
	KeyVersion    string `json:"keyVersion"`
	Ciphertext    string `json:"ciphertext"`
}

func proxyPurpose(sessionID string) string {
	return "sender-session-proxy:" + strings.TrimSpace(sessionID)
}
func normaliseSessionProxy(value SessionProxyConfiguration) (SessionProxyConfiguration, error) {
	value.URL = strings.TrimSpace(value.URL)
	value.Type = ProxyType(strings.ToLower(strings.TrimSpace(string(value.Type))))
	if value.URL == "" {
		return SessionProxyConfiguration{}, ErrSessionProxyInvalid
	}
	parsed, err := url.Parse(value.URL)
	if err != nil || parsed.Host == "" || parsed.Scheme == "" {
		return SessionProxyConfiguration{}, ErrSessionProxyInvalid
	}
	scheme := strings.ToLower(parsed.Scheme)
	if value.Type == "" {
		value.Type = ProxyType(scheme)
	}
	switch value.Type {
	case ProxyHTTP, ProxyHTTPS, ProxySOCKS4, ProxySOCKS5:
	default:
		return SessionProxyConfiguration{}, ErrSessionProxyInvalid
	}
	if scheme != string(value.Type) {
		return SessionProxyConfiguration{}, ErrSessionProxyInvalid
	}
	if parsed.Fragment != "" {
		return SessionProxyConfiguration{}, ErrSessionProxyInvalid
	}
	return value, nil
}
func (s *SessionProxyAdministration) Configure(ctx context.Context, sessionID string, expected int64, value SessionProxyConfiguration, actor, reason string) (SessionProxyStatus, error) {
	if s == nil || s.Store == nil || s.Keys == nil || strings.TrimSpace(sessionID) == "" || expected <= 0 || strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 8 {
		return SessionProxyStatus{}, ErrSessionProxyInvalid
	}
	value, err := normaliseSessionProxy(value)
	if err != nil {
		return SessionProxyStatus{}, err
	}
	plain, err := json.Marshal(value)
	if err != nil {
		return SessionProxyStatus{}, err
	}
	cipher, keyVersion, err := s.Keys.Seal(proxyPurpose(sessionID), string(plain))
	if err != nil {
		return SessionProxyStatus{}, err
	}
	envelope, err := json.Marshal(sessionProxyEnvelope{SchemaVersion: 1, KeyVersion: keyVersion, Ciphertext: base64.StdEncoding.EncodeToString(cipher)})
	if err != nil {
		return SessionProxyStatus{}, err
	}
	return s.Store.ConfigureSessionProxy(ctx, strings.TrimSpace(sessionID), expected, envelope, strings.TrimSpace(actor), strings.TrimSpace(reason))
}

func (s *SessionProxyAdministration) Clear(ctx context.Context, sessionID string, expected int64, actor, reason string) (SessionProxyStatus, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(sessionID) == "" || expected <= 0 || strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 8 {
		return SessionProxyStatus{}, ErrSessionProxyInvalid
	}
	return s.Store.ClearSessionProxy(ctx, strings.TrimSpace(sessionID), expected, strings.TrimSpace(actor), strings.TrimSpace(reason))
}
func (s *SessionProxyAdministration) Resolve(ctx context.Context, sessionID string) (*SessionProxyConfiguration, error) {
	if s == nil || s.Store == nil || s.Keys == nil || strings.TrimSpace(sessionID) == "" {
		return nil, ErrSessionProxyInvalid
	}
	raw, _, err := s.Store.LoadSessionProxy(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var envelope sessionProxyEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.SchemaVersion != 1 || strings.TrimSpace(envelope.KeyVersion) == "" {
		return nil, ErrSessionProxyInvalid
	}
	cipher, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, ErrSessionProxyInvalid
	}
	plain, err := s.Keys.Open(envelope.KeyVersion, proxyPurpose(sessionID), cipher)
	if err != nil {
		return nil, err
	}
	var value SessionProxyConfiguration
	if err := json.Unmarshal([]byte(plain), &value); err != nil {
		return nil, ErrSessionProxyInvalid
	}
	value, err = normaliseSessionProxy(value)
	return &value, err
}

func (s *SessionProxyAdministration) Status(ctx context.Context, sessionID string) (SessionProxyStatus, error) {
	raw, version, err := s.Store.LoadSessionProxy(ctx, strings.TrimSpace(sessionID))
	return SessionProxyStatus{SessionID: strings.TrimSpace(sessionID), Configured: len(raw) > 0, Version: version}, err
}
func proxyConfigurationMutable(status Status) bool {
	switch status {
	case StatusNew, StatusPairing, StatusPaused, StatusDisconnected, StatusRecovering, StatusRecoveryFail, StatusRestricted:
		return true
	default:
		return false
	}
}
