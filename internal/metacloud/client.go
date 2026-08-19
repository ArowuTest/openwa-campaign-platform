package metacloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type FailureSafety string

const (
	SafetySafeToRetry    FailureSafety = "SAFE_TO_RETRY"
	SafetyOutcomeUnknown FailureSafety = "OUTCOME_UNKNOWN"
	SafetyPermanent      FailureSafety = "PERMANENT"
)

type APIError struct {
	Code       string
	Safety     FailureSafety
	RetryAfter time.Duration
	Detail     string
	Err        error
}

func (e APIError) Error() string {
	if e.Detail != "" {
		return e.Code + ": " + e.Detail
	}
	if e.Err != nil {
		return e.Code + ": " + safeClientError(e.Err)
	}
	return e.Code
}
func (e APIError) Unwrap() error { return e.Err }

type MessageRequest struct {
	CredentialKey   string
	GraphAPIVersion string
	PhoneNumberID   string
	Payload         any
}

type MessageResult struct{ MessageID string }

type Client struct {
	Credentials          CredentialResolver
	HTTPClient           *http.Client
	BaseURL              string
	MaximumResponseBytes int64
}

func (c *Client) SendMessage(ctx context.Context, request MessageRequest) (MessageResult, error) {
	if c == nil || c.Credentials == nil {
		return MessageResult{}, APIError{Code: "META_CONFIGURATION_INVALID", Safety: SafetyPermanent, Err: errors.New("credential resolver is required")}
	}
	credential, err := c.Credentials.Resolve(request.CredentialKey)
	if err != nil {
		return MessageResult{}, APIError{Code: "META_CREDENTIAL_UNAVAILABLE", Safety: SafetyPermanent, Err: err}
	}
	version := strings.TrimSpace(request.GraphAPIVersion)
	phoneID := strings.TrimSpace(request.PhoneNumberID)
	if !graphVersionPattern.MatchString(version) || phoneID == "" || request.Payload == nil || strings.ContainsAny(phoneID, "/?#\\") {
		return MessageResult{}, APIError{Code: "META_REQUEST_INVALID", Safety: SafetyPermanent, Err: errors.New("graph version, phone number ID and payload are required")}
	}
	base, err := c.messageBaseURL()
	if err != nil {
		return MessageResult{}, APIError{Code: "META_CONFIGURATION_INVALID", Safety: SafetyPermanent, Err: err}
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + url.PathEscape(version) + "/" + url.PathEscape(phoneID) + "/messages"
	body, err := json.Marshal(request.Payload)
	if err != nil {
		return MessageResult{}, APIError{Code: "META_REQUEST_INVALID", Safety: SafetyPermanent, Err: err}
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(body))
	if err != nil {
		return MessageResult{}, APIError{Code: "META_REQUEST_INVALID", Safety: SafetyPermanent, Err: err}
	}
	httpRequest.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	httpRequest.Header.Set("Content-Type", "application/json")

	client := c.hardenedHTTPClient()
	response, err := client.Do(httpRequest)
	if err != nil {
		return MessageResult{}, APIError{Code: "META_TRANSPORT_UNKNOWN", Safety: SafetyOutcomeUnknown, Err: err}
	}
	defer response.Body.Close()
	return c.decodeMessageResponse(response)
}

func (c *Client) messageBaseURL() (*url.URL, error) {
	raw := strings.TrimSpace(c.BaseURL)
	if raw == "" {
		raw = "https://graph.facebook.com"
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("valid Meta Graph base URL is required")
	}
	host := strings.ToLower(parsed.Hostname())
	if raw != "https://graph.facebook.com" && host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return nil, errors.New("custom Meta Graph base URL must be loopback-only")
	}
	if host == "graph.facebook.com" && parsed.Scheme != "https" {
		return nil, errors.New("Meta Graph base URL must use HTTPS")
	}
	return parsed, nil
}

func (c *Client) hardenedHTTPClient() *http.Client {
	if c.HTTPClient == nil {
		return &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	clone := *c.HTTPClient
	if clone.Timeout <= 0 {
		clone.Timeout = 30 * time.Second
	}
	clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &clone
}

func (c *Client) decodeMessageResponse(response *http.Response) (MessageResult, error) {
	limit := c.MaximumResponseBytes
	if limit <= 0 || limit > 1<<20 {
		limit = 1 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return MessageResult{}, APIError{Code: "META_RESPONSE_READ_FAILED", Safety: SafetyOutcomeUnknown, Err: err}
	}
	if int64(len(raw)) > limit {
		return MessageResult{}, APIError{Code: "META_RESPONSE_TOO_LARGE", Safety: SafetyOutcomeUnknown, Err: errors.New("provider response exceeds limit")}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		safety := SafetyPermanent
		if response.StatusCode == http.StatusTooManyRequests {
			safety = SafetySafeToRetry
		}
		if response.StatusCode == http.StatusRequestTimeout {
			safety = SafetyOutcomeUnknown
		}
		if response.StatusCode >= 500 {
			safety = SafetyOutcomeUnknown
		}
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			safety = SafetyOutcomeUnknown
		}
		return MessageResult{}, APIError{Code: fmt.Sprintf("META_HTTP_%d", response.StatusCode), Safety: safety, RetryAfter: parseMetaRetryAfter(response.Header.Get("Retry-After"), time.Now().UTC()), Detail: redactedMetaDetail(raw)}
	}
	var decoded struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return MessageResult{}, APIError{Code: "META_RESPONSE_INVALID", Safety: SafetyOutcomeUnknown, Err: err}
	}
	if len(decoded.Messages) == 0 || strings.TrimSpace(decoded.Messages[0].ID) == "" {
		return MessageResult{}, APIError{Code: "META_ACCEPTANCE_UNKNOWN", Safety: SafetyOutcomeUnknown, Err: errors.New("successful Meta response omitted message ID")}
	}
	return MessageResult{MessageID: strings.TrimSpace(decoded.Messages[0].ID)}, nil
}

func parseMetaRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil && when.After(now) {
		return when.Sub(now)
	}
	return 0
}

func redactedMetaDetail(_ []byte) string {
	return "Meta rejected request"
}

func safeClientError(err error) string {
	if err == nil {
		return ""
	}
	value := strings.TrimSpace(err.Error())
	if len(value) > 300 {
		value = value[:300]
	}
	return value
}
