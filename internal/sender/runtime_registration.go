package sender

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

var (
	ErrRuntimeAuthentication = errors.New("gateway runtime authentication failed")
	ErrRuntimeReplay         = errors.New("gateway runtime report replayed")
	ErrRuntimeDrift          = errors.New("gateway runtime identity differs from governed configuration")
)

const (
	RuntimeTimestampHeader = "X-Gateway-Runtime-Timestamp"
	RuntimeNonceHeader     = "X-Gateway-Runtime-Nonce"
	RuntimeSignatureHeader = "X-Gateway-Runtime-Signature"
)

type RuntimeState string

const (
	RuntimeReady       RuntimeState = "READY"
	RuntimeDegraded    RuntimeState = "DEGRADED"
	RuntimeUnavailable RuntimeState = "UNAVAILABLE"
	RuntimeDraining    RuntimeState = "DRAINING"
)

type RuntimeReport struct {
	NodeID               string       `json:"nodeId"`
	ExpectedNodeVersion  int64        `json:"expectedNodeVersion"`
	GatewayPoolID        string       `json:"gatewayPoolId"`
	Provider             string       `json:"provider"`
	Engine               string       `json:"engine"`
	AdapterVersion       string       `json:"adapterVersion"`
	GatewayVersion       string       `json:"gatewayVersion"`
	WorkerVersion        string       `json:"workerVersion"`
	ConfigurationVersion string       `json:"configurationVersion"`
	BootID               string       `json:"bootId"`
	InternalURL          string       `json:"internalUrl"`
	Capabilities         []Capability `json:"capabilities"`
	RuntimeState         RuntimeState `json:"runtimeState"`
	Capacity             int          `json:"capacity"`
	SessionCount         int          `json:"sessionCount"`
	QueueDepth           int64        `json:"queueDepth"`
	CPUPercent           float64      `json:"cpuPercent"`
	MemoryBytes          int64        `json:"memoryBytes"`
	ObservedAt           time.Time    `json:"observedAt"`
}

type RuntimeEvent struct {
	ID              string         `json:"id"`
	NodeID          string         `json:"nodeId"`
	GatewayPoolID   string         `json:"gatewayPoolId"`
	EventType       string         `json:"eventType"`
	NodeVersion     int64          `json:"nodeVersion"`
	BootID          string         `json:"bootId"`
	RuntimeIdentity map[string]any `json:"runtimeIdentity"`
	RequestHash     string         `json:"requestHash,omitempty"`
	Reason          string         `json:"reason"`
	OccurredAt      time.Time      `json:"occurredAt"`
}

type RuntimeRegistrationStore interface {
	UseRuntimeNonce(context.Context, string, string, string, time.Time) error
	ApplyRuntimeReport(context.Context, string, int64, RuntimeReport, string, time.Time) (Node, error)
	RecordRuntimeRejection(context.Context, string, RuntimeReport, string, string, time.Time) error
	ListRuntimeEvents(context.Context, string, int) ([]RuntimeEvent, error)
}

type RuntimeRegistrationService struct {
	Store           RuntimeRegistrationStore
	GatewayPools    GatewayPoolStore
	Secret          []byte
	PreviousSecrets [][]byte
	MaximumSkew     time.Duration
	Clock           func() time.Time
}

func (s *RuntimeRegistrationService) now() time.Time {
	if s != nil && s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func DecodeRuntimeReport(raw []byte) (RuntimeReport, error) {
	var report RuntimeReport
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return RuntimeReport{}, fmt.Errorf("decode gateway runtime report: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return RuntimeReport{}, errors.New("gateway runtime report contains trailing JSON")
	} else if !errors.Is(err, io.EOF) {
		return RuntimeReport{}, fmt.Errorf("decode trailing gateway runtime data: %w", err)
	}
	return report, nil
}

func runtimeMAC(secret []byte, timestamp, nonce string, raw []byte) []byte {
	digest := sha256.Sum256(raw)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(nonce))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(hex.EncodeToString(digest[:])))
	return mac.Sum(nil)
}

func SignRuntimeReport(secret []byte, timestamp time.Time, nonce string, raw []byte) (string, string, string, error) {
	if len(secret) < 32 || len(strings.TrimSpace(nonce)) < 16 {
		return "", "", "", ErrRuntimeAuthentication
	}
	ts := strconv.FormatInt(timestamp.UTC().Unix(), 10)
	signature := "sha256=" + hex.EncodeToString(runtimeMAC(secret, ts, nonce, raw))
	return ts, nonce, signature, nil
}

func VerifyRuntimeReport(secret []byte, timestamp, nonce, signature string, raw []byte, now time.Time, maximumSkew time.Duration) (string, time.Time, error) {
	if len(secret) < 32 {
		return "", time.Time{}, ErrRuntimeAuthentication
	}
	timestamp, nonce, signature = strings.TrimSpace(timestamp), strings.TrimSpace(nonce), strings.TrimSpace(signature)
	if len(nonce) < 16 || len(nonce) > 200 || timestamp == "" || !strings.HasPrefix(signature, "sha256=") {
		return "", time.Time{}, ErrRuntimeAuthentication
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || seconds <= 0 {
		return "", time.Time{}, ErrRuntimeAuthentication
	}
	observed := time.Unix(seconds, 0).UTC()
	if maximumSkew <= 0 {
		maximumSkew = 5 * time.Minute
	}
	delta := now.UTC().Sub(observed)
	if delta < 0 {
		delta = -delta
	}
	if delta > maximumSkew {
		return "", time.Time{}, ErrRuntimeAuthentication
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil || len(provided) != sha256.Size || !hmac.Equal(provided, runtimeMAC(secret, timestamp, nonce, raw)) {
		return "", time.Time{}, ErrRuntimeAuthentication
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), observed, nil
}

func verifyRuntimeReportAny(secrets [][]byte, timestamp, nonce, signature string, raw []byte, now time.Time, maximumSkew time.Duration) (string, time.Time, error) {
	var configured bool
	for _, secret := range secrets {
		if len(secret) < 32 {
			continue
		}
		configured = true
		requestHash, observed, err := VerifyRuntimeReport(secret, timestamp, nonce, signature, raw, now, maximumSkew)
		if err == nil {
			return requestHash, observed, nil
		}
	}
	if !configured {
		return "", time.Time{}, ErrRuntimeAuthentication
	}
	return "", time.Time{}, ErrRuntimeAuthentication
}

func normalizeRuntimeCapabilities(values []Capability) ([]Capability, error) {
	allowed := map[Capability]bool{
		CapabilitySendText: true, CapabilitySendImage: true, CapabilitySendVideo: true,
		CapabilitySendDocument: true, CapabilityDeliveryEvents: true,
		CapabilityReadEvents: true, CapabilityInboundMessages: true,
	}
	seen := map[Capability]bool{}
	out := make([]Capability, 0, len(values))
	for _, value := range values {
		value = Capability(strings.ToUpper(strings.TrimSpace(string(value))))
		if !allowed[value] {
			return nil, fmt.Errorf("unsupported runtime capability %q", value)
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("runtime capability declaration is required")
	}
	return out, nil
}

func validateRuntimeReport(report *RuntimeReport, pathNodeID string, pool GatewayPool, now time.Time) error {
	report.NodeID = strings.TrimSpace(report.NodeID)
	report.GatewayPoolID = strings.TrimSpace(report.GatewayPoolID)
	report.Provider = strings.ToUpper(strings.TrimSpace(report.Provider))
	report.Engine = strings.ToUpper(strings.TrimSpace(report.Engine))
	report.AdapterVersion = strings.TrimSpace(report.AdapterVersion)
	report.GatewayVersion = strings.TrimSpace(report.GatewayVersion)
	report.WorkerVersion = strings.TrimSpace(report.WorkerVersion)
	report.ConfigurationVersion = strings.TrimSpace(report.ConfigurationVersion)
	report.BootID = strings.TrimSpace(report.BootID)
	report.InternalURL = strings.TrimRight(strings.TrimSpace(report.InternalURL), "/")
	if report.NodeID == "" || report.NodeID != strings.TrimSpace(pathNodeID) || report.ExpectedNodeVersion <= 0 || report.GatewayPoolID == "" || report.BootID == "" || report.GatewayVersion == "" || report.WorkerVersion == "" || report.ConfigurationVersion == "" || report.InternalURL == "" {
		return ErrRuntimeDrift
	}
	if pool.ID != report.GatewayPoolID || pool.Status != GatewayPoolActive || pool.Provider != GatewayProvider(report.Provider) || pool.Engine != GatewayEngine(report.Engine) || pool.AdapterVersion != report.AdapterVersion {
		return ErrRuntimeDrift
	}
	if pool.EffectiveFrom != nil && pool.EffectiveFrom.After(now) {
		return ErrRuntimeDrift
	}
	if pool.EffectiveTo != nil && !pool.EffectiveTo.After(now) {
		return ErrRuntimeDrift
	}
	if !strings.HasPrefix(report.InternalURL, "https://") && !strings.HasPrefix(report.InternalURL, "http://") {
		return ErrRuntimeDrift
	}
	switch report.RuntimeState {
	case RuntimeReady, RuntimeDegraded, RuntimeUnavailable, RuntimeDraining:
	default:
		return ErrRuntimeDrift
	}
	if report.Capacity < 0 || report.Capacity > 1000 || report.SessionCount < 0 || report.SessionCount > 10000 || report.QueueDepth < 0 || report.CPUPercent < 0 || report.CPUPercent > 100 || report.MemoryBytes < 0 {
		return ErrRuntimeDrift
	}
	if report.ObservedAt.IsZero() {
		report.ObservedAt = now
	} else {
		report.ObservedAt = report.ObservedAt.UTC()
		if report.ObservedAt.After(now.Add(5*time.Minute)) || report.ObservedAt.Before(now.Add(-15*time.Minute)) {
			return ErrRuntimeDrift
		}
	}
	capabilities, err := normalizeRuntimeCapabilities(report.Capabilities)
	if err != nil {
		return err
	}
	report.Capabilities = capabilities
	available := map[Capability]bool{}
	for _, capability := range report.Capabilities {
		available[capability] = true
	}
	for _, required := range pool.Capabilities {
		if !available[required] {
			return fmt.Errorf("%w: runtime lacks governed capability %s", ErrRuntimeDrift, required)
		}
	}
	return nil
}

func (s *RuntimeRegistrationService) Register(ctx context.Context, pathNodeID, timestamp, nonce, signature string, raw []byte) (Node, error) {
	if s == nil || s.Store == nil || s.GatewayPools == nil {
		return Node{}, errors.New("gateway runtime registration dependencies are required")
	}
	now := s.now()
	maximumSkew := s.MaximumSkew
	if maximumSkew <= 0 {
		maximumSkew = 5 * time.Minute
	}
	requestHash, _, err := verifyRuntimeReportAny(append([][]byte{s.Secret}, s.PreviousSecrets...), timestamp, nonce, signature, raw, now, maximumSkew)
	if err != nil {
		return Node{}, err
	}
	report, err := DecodeRuntimeReport(raw)
	if err != nil {
		return Node{}, err
	}
	pool, err := s.GatewayPools.GetGatewayPool(ctx, strings.TrimSpace(report.GatewayPoolID))
	if err != nil {
		recordErr := s.Store.RecordRuntimeRejection(ctx, strings.TrimSpace(pathNodeID), report, requestHash, "gateway pool unavailable", now)
		if recordErr != nil {
			return Node{}, errors.Join(err, fmt.Errorf("record gateway runtime rejection: %w", recordErr))
		}
		return Node{}, err
	}
	if err = validateRuntimeReport(&report, pathNodeID, pool, now); err != nil {
		recordErr := s.Store.RecordRuntimeRejection(ctx, strings.TrimSpace(pathNodeID), report, requestHash, err.Error(), now)
		if recordErr != nil {
			return Node{}, errors.Join(err, fmt.Errorf("record gateway runtime rejection: %w", recordErr))
		}
		return Node{}, err
	}
	if err = s.Store.UseRuntimeNonce(ctx, report.NodeID, nonce, requestHash, now.Add(maximumSkew)); err != nil {
		return Node{}, err
	}
	return s.Store.ApplyRuntimeReport(ctx, report.NodeID, report.ExpectedNodeVersion, report, requestHash, now)
}

func (s *RuntimeRegistrationService) Events(ctx context.Context, nodeID string, limit int) ([]RuntimeEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	return s.Store.ListRuntimeEvents(ctx, strings.TrimSpace(nodeID), limit)
}
