package dispatch

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

type HTTPGateway struct {
	BaseURL       string
	CommandSecret string
	// InternalKey is retained only as a temporary compatibility alias. New
	// deployments must configure CommandSecret and use signed commands.
	InternalKey          string
	Client               *http.Client
	MaximumResponseBytes int64
	Clock                func() time.Time
	Nonce                func() (string, error)
}

func (g *HTTPGateway) Send(ctx context.Context, request GatewayRequest) (GatewayResult, error) {
	now := time.Now().UTC()
	if g.Clock != nil {
		now = g.Clock().UTC()
	}
	if err := validateGatewayRequest(request, now); err != nil {
		return GatewayResult{}, GatewayError{Code: "GATEWAY_REQUEST_INVALID", Safety: FailurePermanent, Err: err}
	}
	base, err := url.Parse(strings.TrimSpace(g.BaseURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return GatewayResult{}, GatewayError{Code: "GATEWAY_CONFIGURATION_INVALID", Safety: FailurePermanent, Err: errors.New("valid gateway base URL is required")}
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/v1/messages"
	payload := map[string]any{
		"idempotencyKey": request.IdempotencyKey, "provider": request.Provider, "engine": request.Engine,
		"gatewayPoolId": request.GatewayPoolID, "gatewayPoolVersion": request.GatewayPoolVersion,
		"gatewayAdapterVersion": request.GatewayAdapterVersion, "gatewayNodeId": request.GatewayNodeID,
		"gatewayNodeVersion": request.GatewayNodeVersion, "sessionId": request.SessionID,
		"sessionLeaseVersion": request.SessionLeaseVersion, "sessionConfigurationVersion": request.SessionConfigurationVersion,
		"authorityExpiresAt": request.AuthorityExpiresAt.UTC().Format(time.RFC3339Nano), "routeReference": request.RouteReference,
		"recipientMsisdn": request.RecipientE164, "messageType": request.MessageType, "body": emptyAsNil(request.Body),
		"mediaUrl": emptyAsNil(request.MediaURL), "clientReference": emptyAsNil(request.ClientReference),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return GatewayResult{}, GatewayError{Code: "GATEWAY_REQUEST_INVALID", Safety: FailurePermanent, Err: err}
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(encoded))
	if err != nil {
		return GatewayResult{}, GatewayError{Code: "GATEWAY_REQUEST_INVALID", Safety: FailurePermanent, Err: err}
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	secret := strings.TrimSpace(g.CommandSecret)
	if secret == "" {
		secret = strings.TrimSpace(g.InternalKey)
	}
	if len([]byte(secret)) < 32 {
		return GatewayResult{}, GatewayError{Code: "GATEWAY_COMMAND_SIGNING_INVALID", Safety: FailurePermanent, Err: errors.New("gateway command secret must contain at least 32 bytes")}
	}
	nonce, err := randomNonce()
	if g.Nonce != nil {
		nonce, err = g.Nonce()
	}
	if err != nil || strings.TrimSpace(nonce) == "" {
		return GatewayResult{}, GatewayError{Code: "GATEWAY_NONCE_FAILED", Safety: FailurePermanent, Err: err}
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	canonical := signedCommandCanonical(http.MethodPost, httpRequest.URL.RequestURI(), timestamp, nonce, encoded)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	httpRequest.Header.Set("X-Gateway-Timestamp", timestamp)
	httpRequest.Header.Set("X-Gateway-Nonce", nonce)
	httpRequest.Header.Set("X-Gateway-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))

	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	observability.InjectTrace(httpRequest)
	response, err := client.Do(httpRequest)
	if err != nil {
		return GatewayResult{}, GatewayError{Code: "GATEWAY_TRANSPORT_UNKNOWN", Safety: FailureOutcomeUnknown, Err: err}
	}
	defer response.Body.Close()
	limit := g.MaximumResponseBytes
	if limit <= 0 || limit > 1<<20 {
		limit = 1 << 20
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if readErr != nil {
		return GatewayResult{}, GatewayError{Code: "GATEWAY_RESPONSE_READ_FAILED", Safety: FailureOutcomeUnknown, Err: readErr}
	}
	if int64(len(body)) > limit {
		return GatewayResult{}, GatewayError{Code: "GATEWAY_RESPONSE_TOO_LARGE", Safety: FailureOutcomeUnknown, Err: errors.New("gateway response exceeds limit")}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		safety := FailurePermanent
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable || response.StatusCode == http.StatusConflict {
			safety = FailureSafeToRetry
		}
		if response.StatusCode >= 500 && response.StatusCode != http.StatusServiceUnavailable {
			safety = FailureOutcomeUnknown
		}
		return GatewayResult{}, GatewayError{Code: fmt.Sprintf("GATEWAY_HTTP_%d", response.StatusCode), Safety: safety, RetryAfter: parseRetryAfter(response.Header.Get("Retry-After"), now), Err: errors.New(redactedGatewayError(body))}
	}
	var decoded struct {
		Accepted          bool      `json:"accepted"`
		ProviderMessageID string    `json:"providerMessageId"`
		AcceptedAt        time.Time `json:"acceptedAt"`
		ErrorCode         string    `json:"errorCode"`
		ErrorDetail       string    `json:"errorDetail"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return GatewayResult{}, GatewayError{Code: "GATEWAY_RESPONSE_INVALID", Safety: FailureOutcomeUnknown, Err: err}
	}
	if !decoded.Accepted {
		return GatewayResult{}, GatewayError{Code: nonEmpty(decoded.ErrorCode, "GATEWAY_REJECTED"), Safety: FailurePermanent, Err: errors.New(redactedGatewayError([]byte(decoded.ErrorDetail)))}
	}
	return GatewayResult{Accepted: true, ProviderMessageID: decoded.ProviderMessageID, AcceptedAt: decoded.AcceptedAt, RawStatusCode: fmt.Sprintf("HTTP_%d", response.StatusCode)}, nil
}

func validateGatewayRequest(request GatewayRequest, now time.Time) error {
	if strings.ToUpper(strings.TrimSpace(request.Provider)) != "OPENWA" {
		return errors.New("gateway request provider must be OPENWA")
	}
	engine := strings.ToUpper(strings.TrimSpace(request.Engine))
	if engine != "WHATSAPP_WEB_JS" && engine != "BAILEYS" {
		return errors.New("gateway request engine is invalid")
	}
	if strings.TrimSpace(request.GatewayPoolID) == "" || request.GatewayPoolVersion <= 0 || strings.TrimSpace(request.GatewayAdapterVersion) == "" ||
		strings.TrimSpace(request.GatewayNodeID) == "" || request.GatewayNodeVersion <= 0 || strings.TrimSpace(request.SessionID) == "" ||
		request.SessionLeaseVersion <= 0 || request.SessionConfigurationVersion <= 0 || strings.TrimSpace(request.RouteReference) == "" {
		return errors.New("gateway request authority evidence is incomplete")
	}
	if !request.AuthorityExpiresAt.After(now) {
		return errors.New("gateway request authority has expired")
	}
	return nil
}

func signedCommandCanonical(method, requestURI, timestamp, nonce string, body []byte) string {
	digest := sha256.Sum256(body)
	return strings.Join([]string{strings.ToUpper(method), requestURI, timestamp, nonce, hex.EncodeToString(digest[:])}, "\n")
}

func randomNonce() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil && when.After(now) {
		return when.Sub(now)
	}
	return 0
}

func emptyAsNil(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return strings.TrimSpace(v)
}
func redactedGatewayError(body []byte) string {
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "gateway rejected request"
	}
	if len(value) > 300 {
		value = value[:300]
	}
	return value
}
