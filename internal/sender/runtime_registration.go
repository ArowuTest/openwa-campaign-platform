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
	"net"
	"net/url"
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

type RuntimeResourceHealth struct {
	Scope                   string `json:"scope"`
	FilesystemPath          string `json:"filesystemPath"`
	DiskTotalBytes          *int64 `json:"diskTotalBytes,omitempty"`
	DiskFreeBytes           *int64 `json:"diskFreeBytes,omitempty"`
	DiskAvailableBytes      *int64 `json:"diskAvailableBytes,omitempty"`
	InodesTotal             *int64 `json:"inodesTotal,omitempty"`
	InodesFree              *int64 `json:"inodesFree,omitempty"`
	ProcessID               int64  `json:"processId"`
	ProcessUptimeSeconds    int64  `json:"processUptimeSeconds"`
	OpenFileDescriptorCount *int64 `json:"openFileDescriptorCount,omitempty"`
	NetworkRXBytes          *int64 `json:"networkRxBytes,omitempty"`
	NetworkTXBytes          *int64 `json:"networkTxBytes,omitempty"`
	NetworkInterfaceCount   int64  `json:"networkInterfaceCount"`
}
type RuntimeReport struct {
	NodeID               string                `json:"nodeId"`
	ExpectedNodeVersion  int64                 `json:"expectedNodeVersion"`
	GatewayPoolID        string                `json:"gatewayPoolId"`
	Provider             string                `json:"provider"`
	Engine               string                `json:"engine"`
	AdapterVersion       string                `json:"adapterVersion"`
	GatewayVersion       string                `json:"gatewayVersion"`
	WorkerVersion        string                `json:"workerVersion"`
	ConfigurationVersion string                `json:"configurationVersion"`
	BootID               string                `json:"bootId"`
	BootStartedAt        time.Time             `json:"bootStartedAt,omitempty"`
	RuntimeSequence      int64                 `json:"runtimeSequence"`
	InternalURL          string                `json:"internalUrl"`
	DeclaredInternalURL  string                `json:"-"`
	Capabilities         []Capability          `json:"capabilities"`
	RuntimeState         RuntimeState          `json:"runtimeState"`
	Capacity             int                   `json:"capacity"`
	SessionCount         int                   `json:"sessionCount"`
	QueueDepth           int64                 `json:"queueDepth"`
	CPUPercent           float64               `json:"cpuPercent"`
	MemoryBytes          int64                 `json:"memoryBytes"`
	ResourceHealth       RuntimeResourceHealth `json:"resourceHealth"`
	ObservedAt           time.Time             `json:"observedAt"`
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
	GetNode(context.Context, string) (Node, error)
	UseRuntimeNonce(context.Context, string, string, string, time.Time) error
	ApplyRuntimeReport(context.Context, string, int64, RuntimeReport, string, string, time.Time, time.Time) (Node, error)
	RecordRuntimeRejection(context.Context, string, RuntimeReport, string, string, string, time.Time, time.Time) error
	ListRuntimeEvents(context.Context, string, int) ([]RuntimeEvent, error)
}

type RuntimeRegistrationService struct {
	Store                      RuntimeRegistrationStore
	GatewayPools               GatewayPoolStore
	Secret                     []byte
	PreviousSecrets            [][]byte
	AllowedInternalHosts       []string
	ForbiddenInternalHosts     []string
	RequireCanonicalRuntimeURL bool
	MaximumSkew                time.Duration
	Clock                      func() time.Time
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

func validateRuntimeResourceHealth(health *RuntimeResourceHealth) error {
	health.Scope = strings.ToUpper(strings.TrimSpace(health.Scope))
	health.FilesystemPath = strings.TrimSpace(health.FilesystemPath)
	if health.Scope != "CONTAINER" || health.FilesystemPath == "" || len(health.FilesystemPath) > 512 ||
		!strings.HasPrefix(health.FilesystemPath, "/") || health.ProcessID <= 0 ||
		health.ProcessUptimeSeconds < 0 || health.NetworkInterfaceCount < 1 {
		return ErrRuntimeDrift
	}
	values := []*int64{
		health.DiskTotalBytes, health.DiskFreeBytes, health.DiskAvailableBytes,
		health.InodesTotal, health.InodesFree, health.OpenFileDescriptorCount,
		health.NetworkRXBytes, health.NetworkTXBytes,
	}
	for _, value := range values {
		if value == nil || *value < 0 {
			return ErrRuntimeDrift
		}
	}
	if *health.DiskTotalBytes <= 0 || *health.DiskFreeBytes > *health.DiskTotalBytes ||
		*health.DiskAvailableBytes > *health.DiskTotalBytes || *health.InodesTotal <= 0 ||
		*health.InodesFree > *health.InodesTotal {
		return ErrRuntimeDrift
	}
	return nil
}
func runtimeBootStartedAt(report RuntimeReport) time.Time {
	if !report.BootStartedAt.IsZero() {
		return report.BootStartedAt.UTC()
	}
	if report.ObservedAt.IsZero() || report.ResourceHealth.ProcessUptimeSeconds < 0 {
		return time.Time{}
	}
	return report.ObservedAt.UTC().Add(-time.Duration(report.ResourceHealth.ProcessUptimeSeconds) * time.Second)
}

func validateRuntimeReport(report *RuntimeReport, pathNodeID string, pool GatewayPool, now time.Time, allowedInternalHosts, forbiddenInternalHosts []string) error {
	return validateRuntimeReportForMode(report, pathNodeID, pool, now, allowedInternalHosts, forbiddenInternalHosts, false)
}

func validateRuntimeReportForMode(report *RuntimeReport, pathNodeID string, pool GatewayPool, now time.Time, allowedInternalHosts, forbiddenInternalHosts []string, allowDevelopmentHTTP bool) error {
	report.NodeID = strings.TrimSpace(report.NodeID)
	report.GatewayPoolID = strings.TrimSpace(report.GatewayPoolID)
	report.Provider = strings.ToUpper(strings.TrimSpace(report.Provider))
	report.Engine = strings.ToUpper(strings.TrimSpace(report.Engine))
	report.AdapterVersion = strings.TrimSpace(report.AdapterVersion)
	report.GatewayVersion = strings.TrimSpace(report.GatewayVersion)
	report.WorkerVersion = strings.TrimSpace(report.WorkerVersion)
	report.ConfigurationVersion = strings.TrimSpace(report.ConfigurationVersion)
	report.BootID = strings.TrimSpace(report.BootID)
	canonicalInternalURL, validInternalURL := canonicalRuntimeInternalURLForMode(report.InternalURL, allowedInternalHosts, forbiddenInternalHosts, allowDevelopmentHTTP)
	report.InternalURL = canonicalInternalURL
	if report.NodeID == "" || report.NodeID != strings.TrimSpace(pathNodeID) || report.ExpectedNodeVersion <= 0 || report.GatewayPoolID == "" || report.BootID == "" || report.RuntimeSequence <= 0 || report.GatewayVersion == "" || report.WorkerVersion == "" || report.ConfigurationVersion == "" || report.InternalURL == "" {
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
	if !validInternalURL {
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
	if err := validateRuntimeResourceHealth(&report.ResourceHealth); err != nil {
		return err
	}
	if report.ObservedAt.IsZero() {
		report.ObservedAt = now
	} else {
		report.ObservedAt = report.ObservedAt.UTC()
		if report.ObservedAt.After(now.Add(5*time.Minute)) || report.ObservedAt.Before(now.Add(-15*time.Minute)) {
			return ErrRuntimeDrift
		}
	}
	derivedBootStartedAt := report.ObservedAt.Add(-time.Duration(report.ResourceHealth.ProcessUptimeSeconds) * time.Second)
	if report.BootStartedAt.IsZero() {
		report.BootStartedAt = derivedBootStartedAt
	} else {
		report.BootStartedAt = report.BootStartedAt.UTC()
		delta := report.BootStartedAt.Sub(derivedBootStartedAt)
		if delta < 0 {
			delta = -delta
		}
		if report.BootStartedAt.After(report.ObservedAt) || delta > 5*time.Second {
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
	report.DeclaredInternalURL = strings.TrimSpace(report.InternalURL)
	report.NodeID = strings.TrimSpace(report.NodeID)
	if report.NodeID == "" || report.NodeID != strings.TrimSpace(pathNodeID) {
		return Node{}, ErrRuntimeDrift
	}
	governedNode, err := s.Store.GetNode(ctx, report.NodeID)
	if err != nil {
		return Node{}, err
	}
	canonicalInternalURL, validInternalURL := canonicalRuntimeInternalURLForMode(report.InternalURL, s.AllowedInternalHosts, s.ForbiddenInternalHosts, !s.RequireCanonicalRuntimeURL)
	if validInternalURL {
		report.InternalURL = canonicalInternalURL
	} else {
		report.InternalURL = ""
	}
	pool, err := s.GatewayPools.GetGatewayPool(ctx, strings.TrimSpace(report.GatewayPoolID))
	if err != nil {
		if !errors.Is(err, ErrSenderNotFound) {
			return Node{}, err
		}
		recordErr := s.Store.RecordRuntimeRejection(ctx, strings.TrimSpace(pathNodeID), report, nonce, requestHash, "gateway pool unavailable", now.Add(maximumSkew), now)
		if recordErr != nil {
			return Node{}, errors.Join(err, fmt.Errorf("record gateway runtime rejection: %w", recordErr))
		}
		return Node{}, err
	}
	if err = validateRuntimeReportForMode(&report, pathNodeID, pool, now, s.AllowedInternalHosts, s.ForbiddenInternalHosts, !s.RequireCanonicalRuntimeURL); err != nil {
		recordErr := s.Store.RecordRuntimeRejection(ctx, strings.TrimSpace(pathNodeID), report, nonce, requestHash, err.Error(), now.Add(maximumSkew), now)
		if recordErr != nil {
			return Node{}, errors.Join(err, fmt.Errorf("record gateway runtime rejection: %w", recordErr))
		}
		return Node{}, err
	}
	if !governedRuntimeAuthorityMatches(governedNode, report) {
		err = ErrRuntimeDrift
		recordErr := s.Store.RecordRuntimeRejection(ctx, strings.TrimSpace(pathNodeID), report, nonce, requestHash, err.Error(), now.Add(maximumSkew), now)
		if recordErr != nil {
			return Node{}, errors.Join(err, fmt.Errorf("record gateway runtime rejection: %w", recordErr))
		}
		return Node{}, err
	}
	return s.Store.ApplyRuntimeReport(ctx, report.NodeID, report.ExpectedNodeVersion, report, nonce, requestHash, now.Add(maximumSkew), now)
}

func (s *RuntimeRegistrationService) Events(ctx context.Context, nodeID string, limit int) ([]RuntimeEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	return s.Store.ListRuntimeEvents(ctx, strings.TrimSpace(nodeID), limit)
}

func normalizeRuntimeHostname(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.Trim(strings.TrimSpace(value), "[]")), ".")
}

func validRuntimeInternalURL(raw string, allowedInternalHosts, forbiddenInternalHosts []string) bool {
	_, valid := canonicalRuntimeInternalURL(raw, allowedInternalHosts, forbiddenInternalHosts)
	return valid
}

func canonicalRuntimeInternalURL(raw string, allowedInternalHosts, forbiddenInternalHosts []string) (string, bool) {
	return canonicalRuntimeInternalURLForMode(raw, allowedInternalHosts, forbiddenInternalHosts, false)
}

func canonicalRuntimeInternalURLForMode(raw string, allowedInternalHosts, forbiddenInternalHosts []string, allowDevelopmentHTTP bool) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil {
		return "", false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if parsed.Host == "" || (scheme != "https" && !(allowDevelopmentHTTP && scheme == "http")) || parsed.User != nil || parsed.Opaque != "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", false
	}
	hostname := normalizeRuntimeHostname(parsed.Hostname())
	if hostname == "" || strings.Contains(hostname, "%") {
		return "", false
	}
	if hostname == "control-api" || hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") {
		return "", false
	}
	if !allowDevelopmentHTTP && (hostname == "openwa-gateway" || hostname == "host.docker.internal" || strings.HasSuffix(hostname, ".docker.internal")) {
		return "", false
	}
	for _, candidate := range forbiddenInternalHosts {
		if forbidden := normalizeRuntimeHostname(candidate); forbidden != "" && forbidden == hostname {
			return "", false
		}
	}
	if allowedInternalHosts != nil {
		approved := false
		for _, candidate := range allowedInternalHosts {
			if normalizeRuntimeHostname(candidate) == hostname {
				approved = true
				break
			}
		}
		if !approved {
			return "", false
		}
	}
	ip := net.ParseIP(hostname)
	if isRuntimeIPLikeHostname(hostname) {
		// Deployed runtime authority is DNS-only. Development HTTP may still use
		// canonical private literals for local container/network plumbing, but
		// legacy numeric IPv4 spellings are never accepted as DNS names.
		if ip == nil || !allowDevelopmentHTTP || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return "", false
		}
		if v4 := ip.To4(); v4 != nil {
			if !(v4[0] == 10 || (v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31) || (v4[0] == 192 && v4[1] == 168)) {
				return "", false
			}
		} else if len(ip) != net.IPv6len || (ip[0]&0xfe) != 0xfc {
			return "", false
		}
	}
	authority := hostname
	if strings.Contains(hostname, ":") {
		authority = "[" + hostname + "]"
	}
	port := parsed.Port()
	if strings.HasSuffix(parsed.Host, ":") {
		return "", false
	}
	if port != "" {
		portNumber, portErr := strconv.Atoi(port)
		if portErr != nil || portNumber < 1 || portNumber > 65535 {
			return "", false
		}
		defaultPort := 443
		if scheme == "http" {
			defaultPort = 80
		}
		if portNumber != defaultPort {
			authority = net.JoinHostPort(hostname, strconv.Itoa(portNumber))
		}
	}
	return (&url.URL{Scheme: scheme, Host: authority}).String(), true
}

func isRuntimeIPLikeHostname(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.Trim(value, "[]")))
	if normalized == "" || strings.Contains(normalized, "%") {
		return normalized != ""
	}
	if net.ParseIP(normalized) != nil {
		return true
	}
	parts := strings.Split(normalized, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		base := 10
		digits := part
		if strings.HasPrefix(part, "0x") {
			base, digits = 16, part[2:]
		} else if len(part) > 1 && part[0] == '0' {
			base, digits = 8, part[1:]
		}
		if digits == "" {
			return false
		}
		if _, err := strconv.ParseUint(digits, base, 32); err != nil {
			return false
		}
	}
	return true
}

func governedRuntimeAuthorityMatches(current Node, report RuntimeReport) bool {
	if strings.TrimSpace(current.GatewayPoolID) == "" || strings.TrimSpace(current.GatewayPoolID) != report.GatewayPoolID ||
		(strings.TrimSpace(current.Provider) != "" && strings.ToUpper(strings.TrimSpace(current.Provider)) != report.Provider) ||
		(strings.TrimSpace(current.Engine) != "" && strings.ToUpper(strings.TrimSpace(current.Engine)) != report.Engine) ||
		(strings.TrimSpace(current.AdapterVersion) != "" && strings.TrimSpace(current.AdapterVersion) != report.AdapterVersion) {
		return false
	}
	if strings.TrimSpace(current.InternalURL) == "" {
		return true
	}
	parsed, err := url.Parse(report.InternalURL)
	allowDevelopmentHTTP := err == nil && strings.EqualFold(parsed.Scheme, "http")
	canonical, valid := canonicalRuntimeInternalURLForMode(current.InternalURL, nil, nil, allowDevelopmentHTTP)
	return valid && canonical == report.InternalURL
}
