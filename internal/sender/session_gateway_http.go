package sender

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"campaign-platform/internal/observability"
)

type HTTPSessionGateway struct {
	CommandSecret        string
	Client               *http.Client
	Clock                func() time.Time
	Nonce                func() (string, error)
	MaximumResponseBytes int64
}

func (g *HTTPSessionGateway) Create(ctx context.Context, n Node, s GovernedSession) (SessionGatewayResult, error) {
	return g.request(ctx, n, http.MethodPost, "/v1/sessions", map[string]any{"name": s.ID})
}
func (g *HTTPSessionGateway) Start(ctx context.Context, n Node, s GovernedSession) (SessionGatewayResult, error) {
	return g.request(ctx, n, http.MethodPost, "/v1/sessions/"+url.PathEscape(s.ID)+"/start", nil)
}
func (g *HTTPSessionGateway) Stop(ctx context.Context, n Node, s GovernedSession) (SessionGatewayResult, error) {
	return g.request(ctx, n, http.MethodPost, "/v1/sessions/"+url.PathEscape(s.ID)+"/stop", nil)
}
func (g *HTTPSessionGateway) Logout(ctx context.Context, n Node, s GovernedSession) (SessionGatewayResult, error) {
	return g.request(ctx, n, http.MethodPost, "/v1/sessions/"+url.PathEscape(s.ID)+"/logout", nil)
}
func (g *HTTPSessionGateway) Delete(ctx context.Context, n Node, s GovernedSession) error {
	_, err := g.request(ctx, n, http.MethodDelete, "/v1/sessions/"+url.PathEscape(s.ID), nil)
	return err
}
func (g *HTTPSessionGateway) QR(ctx context.Context, n Node, s GovernedSession) (SessionGatewayResult, error) {
	return g.request(ctx, n, http.MethodGet, "/v1/sessions/"+url.PathEscape(s.ID)+"/qr", nil)
}
func (g *HTTPSessionGateway) PairingCode(ctx context.Context, n Node, s GovernedSession, phone string) (SessionGatewayResult, error) {
	return g.request(ctx, n, http.MethodPost, "/v1/sessions/"+url.PathEscape(s.ID)+"/pairing-code", map[string]any{"phoneNumber": phone})
}
func (g *HTTPSessionGateway) Drain(ctx context.Context, n Node, s GovernedSession) error {
	_, err := g.request(ctx, n, http.MethodPost, "/v1/sessions/"+url.PathEscape(s.ID)+"/drain", nil)
	return err
}
func (g *HTTPSessionGateway) Resume(ctx context.Context, n Node, s GovernedSession) error {
	_, err := g.request(ctx, n, http.MethodPost, "/v1/sessions/"+url.PathEscape(s.ID)+"/resume", nil)
	return err
}
func (g *HTTPSessionGateway) Health(ctx context.Context, n Node, s GovernedSession) (SessionGatewayResult, error) {
	return g.request(ctx, n, http.MethodGet, "/v1/sessions/"+url.PathEscape(s.ID)+"/health", nil)
}

func (g *HTTPSessionGateway) request(ctx context.Context, node Node, method, path string, payload any) (SessionGatewayResult, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(node.InternalURL), "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return SessionGatewayResult{}, errors.New("valid governed gateway node URL is required")
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	body := []byte{}
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return SessionGatewayResult{}, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, base.String(), bytes.NewReader(body))
	if err != nil {
		return SessionGatewayResult{}, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Gateway-Target-Node-ID", strings.TrimSpace(node.ID))
	req.Header.Set("X-Gateway-Target-Node-Version", strconv.FormatInt(node.Version, 10))
	req.Header.Set("X-Gateway-Target-Pool-ID", strings.TrimSpace(node.GatewayPoolID))
	req.Header.Set("X-Gateway-Target-Provider", strings.ToUpper(strings.TrimSpace(node.Provider)))
	req.Header.Set("X-Gateway-Target-Engine", strings.ToUpper(strings.TrimSpace(node.Engine)))
	req.Header.Set("X-Gateway-Target-Adapter-Version", strings.TrimSpace(node.AdapterVersion))
	if err := g.sign(req, body); err != nil {
		return SessionGatewayResult{}, err
	}
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	observability.InjectTrace(req)
	resp, err := client.Do(req)
	if err != nil {
		return SessionGatewayResult{}, fmt.Errorf("invoke gateway session operation: %w", err)
	}
	defer resp.Body.Close()
	limit := g.MaximumResponseBytes
	if limit <= 0 || limit > 1<<20 {
		limit = 1 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return SessionGatewayResult{}, err
	}
	if int64(len(raw)) > limit {
		return SessionGatewayResult{}, errors.New("gateway session response too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SessionGatewayResult{}, fmt.Errorf("gateway session operation returned HTTP %d: %s", resp.StatusCode, redactSessionGatewayError(raw))
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return SessionGatewayResult{SessionID: ""}, nil
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return SessionGatewayResult{}, fmt.Errorf("decode gateway session response: %w", err)
	}
	id := stringValue(generic, "sessionId")
	if id == "" {
		id = stringValue(generic, "id")
	}
	status := stringValue(generic, "status")
	return SessionGatewayResult{SessionID: id, Status: status, Payload: generic}, nil
}
func (g *HTTPSessionGateway) sign(req *http.Request, body []byte) error {
	secret := strings.TrimSpace(g.CommandSecret)
	if len([]byte(secret)) < 32 {
		return errors.New("gateway command secret must contain at least 32 bytes")
	}
	now := time.Now().UTC()
	if g.Clock != nil {
		now = g.Clock().UTC()
	}
	nonce, err := sessionNonce()
	if g.Nonce != nil {
		nonce, err = g.Nonce()
	}
	if err != nil || nonce == "" {
		return errors.New("create gateway command nonce")
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	digest := sha256.Sum256(body)
	canonical := strings.Join([]string{strings.ToUpper(req.Method), req.URL.RequestURI(), timestamp, nonce, hex.EncodeToString(digest[:])}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	req.Header.Set("X-Gateway-Timestamp", timestamp)
	req.Header.Set("X-Gateway-Nonce", nonce)
	req.Header.Set("X-Gateway-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	return nil
}
func sessionNonce() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func stringValue(v map[string]any, key string) string {
	x, ok := v[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(x))
}
func redactSessionGatewayError(raw []byte) string {
	v := strings.TrimSpace(string(raw))
	if len(v) > 300 {
		v = v[:300]
	}
	if v == "" {
		return "gateway rejected operation"
	}
	return v
}
