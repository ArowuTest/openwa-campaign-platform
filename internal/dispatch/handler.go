package dispatch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/jobs"
	"campaign-platform/internal/platformpolicy"
)

const JobType = "DISPATCH_CAMPAIGN_RECIPIENT"

type JobPayload struct {
	CampaignRecipientID string `json:"campaignRecipientId"`
}

type MetaRepresentation string

const (
	MetaRepresentationTemplate MetaRepresentation = "TEMPLATE"
	MetaRepresentationFreeForm MetaRepresentation = "FREE_FORM"
)

type MetaTemplateParameter struct {
	Type string
	Text string
	Link string
}

type MetaTemplateComponent struct {
	Type       string
	SubType    string
	Index      int
	Parameters []MetaTemplateParameter
}

type Material struct {
	SenderPoolID                string
	Provider                    string
	Engine                      string
	GatewayPoolID               string
	GatewayPoolVersion          int64
	GatewayAdapterVersion       string
	GatewayNodeID               string
	GatewayNodeURL              string
	GatewayNodeVersion          int64
	SessionID                   string
	SessionLeaseVersion         int64
	SessionConfigurationVersion int64
	AuthorityExpiresAt          time.Time
	RouteReference              string
	MetaSenderID                string
	MetaSenderVersion           int64
	MetaCredentialKey           string
	MetaGraphAPIVersion         string
	MetaPhoneNumberID           string
	MetaRepresentation          MetaRepresentation
	MetaFreeFormEligibleUntil   time.Time
	MetaTemplateName            string
	MetaTemplateLanguage        string
	MetaBodyParameters          []string
	MetaComponents              []MetaTemplateComponent
	RecipientE164               string
	MessageType                 string
	Body                        string
	MediaObjectURL              string
	ClientReference             string
}

type MaterialLoader interface {
	Load(context.Context, delivery.Recipient) (Material, error)
}

type PermanentMaterialError struct{ Err error }

func (e PermanentMaterialError) Error() string {
	if e.Err == nil {
		return "dispatch material is permanently invalid"
	}
	return e.Err.Error()
}
func (e PermanentMaterialError) Unwrap() error { return e.Err }

type EligibilityDecision struct {
	Eligible bool
	Reason   string
}

type FinalEligibility interface {
	Check(context.Context, delivery.Recipient, time.Time) (EligibilityDecision, error)
}

type GatewayRequest struct {
	IdempotencyKey              string
	Provider                    string
	Engine                      string
	GatewayPoolID               string
	GatewayPoolVersion          int64
	GatewayAdapterVersion       string
	GatewayNodeID               string
	GatewayNodeURL              string
	GatewayNodeVersion          int64
	SessionID                   string
	SessionLeaseVersion         int64
	SessionConfigurationVersion int64
	AuthorityExpiresAt          time.Time
	RouteReference              string
	MetaSenderID                string
	MetaSenderVersion           int64
	MetaCredentialKey           string
	MetaGraphAPIVersion         string
	MetaPhoneNumberID           string
	MetaRepresentation          MetaRepresentation
	MetaFreeFormEligibleUntil   time.Time
	MetaTemplateName            string
	MetaTemplateLanguage        string
	MetaBodyParameters          []string
	MetaComponents              []MetaTemplateComponent
	RecipientE164               string
	MessageType                 string
	Body                        string
	MediaURL                    string
	ClientReference             string
}

type GatewayResult struct {
	Accepted          bool
	ProviderMessageID string
	AcceptedAt        time.Time
	RawStatusCode     string
}

type FailureSafety string

const (
	FailureSafeToRetry    FailureSafety = "SAFE_TO_RETRY"
	FailureOutcomeUnknown FailureSafety = "OUTCOME_UNKNOWN"
	FailurePermanent      FailureSafety = "PERMANENT"
)

type GatewayError struct {
	Code       string
	Safety     FailureSafety
	RetryAfter time.Duration
	Err        error
}

func (e GatewayError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}
func (e GatewayError) Unwrap() error { return e.Err }

type Gateway interface {
	Send(context.Context, GatewayRequest) (GatewayResult, error)
}

// GatewayPreflight performs only local, pre-submission readiness checks. It
// must not make a provider submission or any call whose acceptance can be ambiguous.
type GatewayPreflight interface {
	Preflight(context.Context, GatewayRequest) error
}

// GatewayPreparedSender submits a request that already passed pre-submission readiness checks.
// Implementations must not repeat retryable transport/window readiness checks after SUBMITTING.
type GatewayPreparedSender interface {
	SendPrepared(context.Context, GatewayRequest) (GatewayResult, error)
}

type Handler struct {
	Ledger      *delivery.Service
	Materials   MaterialLoader
	Eligibility FinalEligibility
	Gateway     Gateway
	Throttle    Throttle
	Pacing      PacingController
	Maintenance *platformpolicy.MaintenanceAdministration
	Clock       func() time.Time
}

func (h *Handler) Handle(ctx context.Context, job jobs.Job) error {
	if h == nil || h.Ledger == nil || h.Materials == nil || h.Eligibility == nil || h.Gateway == nil {
		return errors.New("dispatch handler dependencies are required")
	}
	preflight, preflightOK := h.Gateway.(GatewayPreflight)
	prepared, preparedOK := h.Gateway.(GatewayPreparedSender)
	if !preflightOK || !preparedOK {
		return jobs.RetryableError{Code: "GATEWAY_PREPARED_CONTRACT_REQUIRED", RetryAfter: time.Minute, Err: errors.New("dispatch gateway must implement preflight and prepared send")}
	}
	var payload JobPayload
	if err := decodePayload(job.Payload, &payload); err != nil {
		return err
	}
	recipient, err := h.Ledger.Get(ctx, payload.CampaignRecipientID)
	if err != nil {
		return err
	}
	if sendAlreadyAccepted(recipient.Status) || terminalWithoutSend(recipient.Status) {
		return nil
	}
	now := time.Now().UTC()
	if h.Clock != nil {
		now = h.Clock().UTC()
	}
	if recipient.Status == delivery.StatusSubmitting {
		_, _, err = h.Ledger.ApplyEvent(ctx, recipient.ID, delivery.Event{
			DeduplicationKey: "recovery-unknown:" + recipient.IdempotencyKey,
			Type:             delivery.EventUnknown,
			ErrorCode:        "SUBMISSION_OUTCOME_UNRESOLVED",
			ErrorDetail:      "dispatch resumed with unresolved prior submission",
			OccurredAt:       now,
		})
		return err
	}
	decision, err := h.Eligibility.Check(ctx, recipient, now)
	if err != nil {
		return jobs.RetryableError{Code: "ELIGIBILITY_CHECK_FAILED", Err: err}
	}
	if !decision.Eligible {
		return h.handleIneligible(ctx, recipient, decision, now)
	}
	material, err := h.Materials.Load(ctx, recipient)
	if err != nil {
		var permanent PermanentMaterialError
		if errors.As(err, &permanent) {
			_, _, applyErr := h.Ledger.ApplyEvent(ctx, recipient.ID, delivery.Event{
				DeduplicationKey: fmt.Sprintf("material-permanent:%s:%d", job.ID, job.AttemptCount),
				Type:             delivery.EventFailedPermanent,
				ErrorCode:        "MATERIAL_INVALID",
				ErrorDetail:      safeError(permanent),
				OccurredAt:       now,
			})
			return applyErr
		}
		return jobs.RetryableError{Code: "MATERIAL_LOAD_FAILED", Err: err}
	}
	if err := validateMaterial(material); err != nil {
		_, _, applyErr := h.Ledger.ApplyEvent(ctx, recipient.ID, delivery.Event{
			DeduplicationKey: fmt.Sprintf("material-shape-permanent:%s:%d", job.ID, job.AttemptCount),
			Type:             delivery.EventFailedPermanent,
			ErrorCode:        "MATERIAL_INVALID",
			ErrorDetail:      safeError(err),
			OccurredAt:       now,
		})
		return applyErr
	}
	if h.Maintenance != nil {
		err := h.Maintenance.Check(ctx, platformpolicy.OperationDispatchSubmit, platformpolicy.OperationalScope{Provider: material.Provider, GatewayPoolID: material.GatewayPoolID, SenderPoolID: material.SenderPoolID}, now)
		if err != nil {
			return jobs.RetryableError{Code: "MAINTENANCE_MODE_ACTIVE", RetryAfter: time.Minute, Err: err}
		}
	}
	attemptKey := fmt.Sprintf("%s:%d", job.ID, job.AttemptCount)
	if h.Pacing != nil {
		if err := h.Pacing.Wait(ctx, recipient, material, now); err != nil {
			code := "PACING_WAIT_INTERRUPTED"
			if errors.Is(err, ErrHourlyAllowanceExhausted) {
				code = "SENDER_HOURLY_ALLOWANCE_EXHAUSTED"
			}
			if errors.Is(err, ErrDailyAllowanceExhausted) {
				code = "SENDER_DAILY_ALLOWANCE_EXHAUSTED"
			}
			if errors.Is(err, ErrActiveCampaignLimit) {
				code = "SENDER_ACTIVE_CAMPAIGN_LIMIT"
			}
			return jobs.RetryableError{Code: code, RetryAfter: time.Minute, Err: err}
		}
	} else if h.Throttle != nil {
		if err := h.Throttle.Wait(ctx, material.SessionID); err != nil {
			return jobs.RetryableError{Code: "THROTTLE_WAIT_INTERRUPTED", Err: err}
		}
	}
	request := gatewayRequestFromMaterial(recipient, material)
	if preflightErr := preflight.Preflight(ctx, request); preflightErr != nil {
		var gatewayErr GatewayError
		if errors.As(preflightErr, &gatewayErr) && gatewayErr.Safety == FailurePermanent {
			_, _, applyErr := h.Ledger.ApplyEvent(ctx, recipient.ID, delivery.Event{DeduplicationKey: "preflight-permanent:" + attemptKey, Type: delivery.EventFailedPermanent, ErrorCode: nonEmpty(gatewayErr.Code, "GATEWAY_PREFLIGHT_PERMANENT"), ErrorDetail: safeError(preflightErr), OccurredAt: now})
			if applyErr != nil {
				return applyErr
			}
			return nil
		}
		code, retryAfter := "GATEWAY_PREFLIGHT_FAILED", time.Duration(0)
		if errors.As(preflightErr, &gatewayErr) {
			code, retryAfter = nonEmpty(gatewayErr.Code, code), gatewayErr.RetryAfter
		}
		return jobs.RetryableError{Code: code, RetryAfter: retryAfter, Err: preflightErr}
	}
	finalNow := time.Now().UTC()
	if h.Clock != nil {
		finalNow = h.Clock().UTC()
	}
	decision, err = h.Eligibility.Check(ctx, recipient, finalNow)
	if err != nil {
		return jobs.RetryableError{Code: "ELIGIBILITY_RECHECK_FAILED", Err: err}
	}
	if !decision.Eligible {
		return h.handleIneligible(ctx, recipient, decision, finalNow)
	}
	now = finalNow
	if _, _, err = h.Ledger.ApplyEvent(ctx, recipient.ID, delivery.Event{DeduplicationKey: "submitting:" + attemptKey, Type: delivery.EventSubmitting, OccurredAt: now}); err != nil {
		if !errors.Is(err, delivery.ErrCampaignNotDispatchable) {
			return err
		}
		decision, checkErr := h.Eligibility.Check(ctx, recipient, now)
		if checkErr != nil {
			return jobs.RetryableError{Code: "ELIGIBILITY_RECHECK_FAILED", Err: checkErr}
		}
		if !decision.Eligible {
			return h.handleIneligible(ctx, recipient, decision, now)
		}
		return jobs.RetryableError{Code: "CAMPAIGN_STATE_CHANGED_BEFORE_SUBMIT", Err: err}
	}
	result, sendErr := prepared.SendPrepared(ctx, request)
	if sendErr != nil {
		var gatewayErr GatewayError
		if !errors.As(sendErr, &gatewayErr) {
			gatewayErr = GatewayError{Code: "GATEWAY_OUTCOME_UNKNOWN", Safety: FailureOutcomeUnknown, Err: sendErr}
		}
		switch gatewayErr.Safety {
		case FailureSafeToRetry:
			_, _, applyErr := h.Ledger.ApplyEvent(ctx, recipient.ID, delivery.Event{DeduplicationKey: "retryable:" + attemptKey, Type: delivery.EventFailedRetryable, ErrorCode: nonEmpty(gatewayErr.Code, "GATEWAY_RETRYABLE"), ErrorDetail: safeError(sendErr), OccurredAt: now})
			if applyErr != nil {
				return applyErr
			}
			return jobs.RetryableError{Code: nonEmpty(gatewayErr.Code, "GATEWAY_RETRYABLE"), RetryAfter: gatewayErr.RetryAfter, Err: sendErr}
		case FailurePermanent:
			_, _, applyErr := h.Ledger.ApplyEvent(ctx, recipient.ID, delivery.Event{DeduplicationKey: "permanent:" + attemptKey, Type: delivery.EventFailedPermanent, ErrorCode: nonEmpty(gatewayErr.Code, "GATEWAY_PERMANENT"), ErrorDetail: safeError(sendErr), OccurredAt: now})
			return applyErr
		default:
			_, _, applyErr := h.Ledger.ApplyEvent(ctx, recipient.ID, delivery.Event{DeduplicationKey: "unknown:" + attemptKey, Type: delivery.EventUnknown, ErrorCode: nonEmpty(gatewayErr.Code, "GATEWAY_OUTCOME_UNKNOWN"), ErrorDetail: safeError(sendErr), OccurredAt: now})
			// Unknown outcomes are deliberately not retried automatically because a duplicate
			// recipient message may already have been accepted by the provider.
			return applyErr
		}
	}
	if !result.Accepted || strings.TrimSpace(result.ProviderMessageID) == "" {
		_, _, applyErr := h.Ledger.ApplyEvent(ctx, recipient.ID, delivery.Event{DeduplicationKey: "invalid-result:" + attemptKey, Type: delivery.EventUnknown, ErrorCode: "GATEWAY_INVALID_ACCEPTANCE", OccurredAt: now})
		return applyErr
	}
	acceptedAt := result.AcceptedAt.UTC()
	if acceptedAt.IsZero() {
		acceptedAt = now
	}
	_, _, err = h.Ledger.ApplyEvent(ctx, recipient.ID, delivery.Event{DeduplicationKey: "gateway-accepted:" + recipient.IdempotencyKey, Type: delivery.EventGatewayAccepted, ProviderMessageID: result.ProviderMessageID, OccurredAt: acceptedAt})
	return err
}

func (h *Handler) handleIneligible(ctx context.Context, recipient delivery.Recipient, decision EligibilityDecision, now time.Time) error {
	if transientEligibilityReason(decision.Reason) {
		reason := nonEmpty(strings.TrimSpace(decision.Reason), "ELIGIBILITY_TEMPORARILY_BLOCKED")
		return jobs.RetryableError{Code: reason, RetryAfter: time.Minute, Err: fmt.Errorf("dispatch eligibility temporarily blocked: %s", reason)}
	}
	return h.applyFinalExclusion(ctx, recipient, decision, now)
}

func transientEligibilityReason(reason string) bool {
	switch strings.ToUpper(strings.TrimSpace(reason)) {
	case "CAMPAIGN_PAUSED", "CAMPAIGN_NOT_STARTED", "CAMPAIGN_QUIET_HOURS":
		return true
	default:
		return false
	}
}

func (h *Handler) applyFinalExclusion(ctx context.Context, recipient delivery.Recipient, decision EligibilityDecision, now time.Time) error {
	eventType := delivery.EventSuppressed
	keyPrefix := "final-exclusion:"
	if strings.EqualFold(strings.TrimSpace(decision.Reason), "CAMPAIGN_CANCELLED") {
		eventType = delivery.EventCancelled
		keyPrefix = "final-cancellation:"
	}
	_, _, err := h.Ledger.ApplyEvent(ctx, recipient.ID, delivery.Event{
		DeduplicationKey: keyPrefix + recipient.IdempotencyKey,
		Type:             eventType,
		ErrorCode:        nonEmpty(decision.Reason, "INELIGIBLE_FINAL_CHECK"),
		OccurredAt:       now,
	})
	return err
}

func sendAlreadyAccepted(status delivery.Status) bool {
	switch status {
	case delivery.StatusGatewayAccepted, delivery.StatusSent, delivery.StatusDelivered, delivery.StatusRead:
		return true
	default:
		return false
	}
}
func terminalWithoutSend(status delivery.Status) bool {
	switch status {
	case delivery.StatusFailedPermanent, delivery.StatusUnknown, delivery.StatusSuppressedBeforeSend, delivery.StatusCancelled:
		return true
	default:
		return false
	}
}
func validateMaterial(v Material) error {
	provider := strings.ToUpper(strings.TrimSpace(v.Provider))
	engine := strings.ToUpper(strings.TrimSpace(v.Engine))
	if strings.TrimSpace(v.RouteReference) == "" || !strings.HasPrefix(strings.TrimSpace(v.RecipientE164), "+") {
		return errors.New("dispatch material requires route reference and E.164 recipient")
	}
	switch provider {
	case "OPENWA":
		if (engine != "WHATSAPP_WEB_JS" && engine != "BAILEYS") || strings.TrimSpace(v.GatewayPoolID) == "" ||
			v.GatewayPoolVersion <= 0 || strings.TrimSpace(v.GatewayAdapterVersion) == "" || strings.TrimSpace(v.GatewayNodeID) == "" ||
			v.GatewayNodeVersion <= 0 || strings.TrimSpace(v.SessionID) == "" || v.SessionLeaseVersion <= 0 ||
			v.SessionConfigurationVersion <= 0 || v.AuthorityExpiresAt.IsZero() {
			return errors.New("OpenWA dispatch material requires fenced gateway session authority")
		}
		if strings.TrimSpace(v.MetaSenderID) != "" || v.MetaSenderVersion != 0 || strings.TrimSpace(v.MetaCredentialKey) != "" ||
			strings.TrimSpace(v.MetaGraphAPIVersion) != "" || strings.TrimSpace(v.MetaPhoneNumberID) != "" || v.MetaRepresentation != "" ||
			!v.MetaFreeFormEligibleUntil.IsZero() || strings.TrimSpace(v.MetaTemplateName) != "" || strings.TrimSpace(v.MetaTemplateLanguage) != "" ||
			len(v.MetaBodyParameters) != 0 || len(v.MetaComponents) != 0 {
			return errors.New("OpenWA dispatch material cannot carry Meta sender or representation authority")
		}
	case "META":
		if engine != "CLOUD_API" || strings.TrimSpace(v.MetaSenderID) == "" || v.MetaSenderVersion <= 0 ||
			strings.TrimSpace(v.MetaCredentialKey) == "" || strings.TrimSpace(v.MetaGraphAPIVersion) == "" || strings.TrimSpace(v.MetaPhoneNumberID) == "" {
			return errors.New("Meta dispatch material requires governed sender evidence")
		}
		if strings.TrimSpace(v.GatewayPoolID) != "" || v.GatewayPoolVersion != 0 || strings.TrimSpace(v.GatewayAdapterVersion) != "" ||
			strings.TrimSpace(v.GatewayNodeID) != "" || v.GatewayNodeVersion != 0 || strings.TrimSpace(v.SessionID) != "" ||
			v.SessionLeaseVersion != 0 || v.SessionConfigurationVersion != 0 || !v.AuthorityExpiresAt.IsZero() {
			return errors.New("Meta dispatch material cannot carry OpenWA gateway authority")
		}
		switch v.MetaRepresentation {
		case MetaRepresentationTemplate:
			if !v.MetaFreeFormEligibleUntil.IsZero() {
				return errors.New("Meta template material cannot carry free-form eligibility")
			}
			if strings.TrimSpace(v.MetaTemplateName) == "" || strings.TrimSpace(v.MetaTemplateLanguage) == "" {
				return errors.New("Meta template material requires template evidence")
			}
		case MetaRepresentationFreeForm:
			if v.MetaFreeFormEligibleUntil.IsZero() {
				return errors.New("Meta free-form material requires explicit conversation-window deadline")
			}
			if strings.TrimSpace(v.MetaTemplateName) != "" || strings.TrimSpace(v.MetaTemplateLanguage) != "" || len(v.MetaBodyParameters) != 0 || len(v.MetaComponents) != 0 {
				return errors.New("Meta free-form material cannot carry template evidence")
			}
		default:
			return errors.New("Meta dispatch material requires explicit representation")
		}
	default:
		return errors.New("unsupported dispatch provider")
	}
	if v.MessageType == "text" && strings.TrimSpace(v.Body) == "" && !(provider == "META" && v.MetaRepresentation == MetaRepresentationTemplate) {
		return errors.New("text dispatch requires body")
	}
	if v.MessageType != "text" && strings.TrimSpace(v.MediaObjectURL) == "" && !(provider == "META" && v.MetaRepresentation == MetaRepresentationTemplate && len(v.MetaComponents) > 0) {
		return errors.New("media dispatch requires object URL")
	}
	return nil
}
func safeError(err error) string {
	if err == nil {
		return ""
	}
	value := strings.TrimSpace(err.Error())
	if len(value) > 500 {
		value = value[:500]
	}
	return value
}
func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func gatewayRequestFromMaterial(recipient delivery.Recipient, material Material) GatewayRequest {
	return GatewayRequest{
		IdempotencyKey: recipient.IdempotencyKey, Provider: material.Provider, Engine: material.Engine,
		GatewayPoolID: material.GatewayPoolID, GatewayPoolVersion: material.GatewayPoolVersion,
		GatewayAdapterVersion: material.GatewayAdapterVersion, GatewayNodeID: material.GatewayNodeID, GatewayNodeURL: material.GatewayNodeURL,
		GatewayNodeVersion: material.GatewayNodeVersion, SessionID: material.SessionID,
		SessionLeaseVersion: material.SessionLeaseVersion, SessionConfigurationVersion: material.SessionConfigurationVersion,
		AuthorityExpiresAt: material.AuthorityExpiresAt, RouteReference: material.RouteReference,
		MetaSenderID: material.MetaSenderID, MetaSenderVersion: material.MetaSenderVersion, MetaCredentialKey: material.MetaCredentialKey,
		MetaGraphAPIVersion: material.MetaGraphAPIVersion, MetaPhoneNumberID: material.MetaPhoneNumberID,
		MetaRepresentation: material.MetaRepresentation, MetaFreeFormEligibleUntil: material.MetaFreeFormEligibleUntil,
		MetaTemplateName: material.MetaTemplateName, MetaTemplateLanguage: material.MetaTemplateLanguage,
		MetaBodyParameters: append([]string(nil), material.MetaBodyParameters...), MetaComponents: cloneMetaTemplateComponents(material.MetaComponents),
		RecipientE164: material.RecipientE164, MessageType: material.MessageType, Body: material.Body,
		MediaURL: material.MediaObjectURL, ClientReference: material.ClientReference,
	}
}

func cloneMetaTemplateComponents(input []MetaTemplateComponent) []MetaTemplateComponent {
	if len(input) == 0 {
		return nil
	}
	out := make([]MetaTemplateComponent, len(input))
	for i, component := range input {
		out[i] = component
		out[i].Parameters = append([]MetaTemplateParameter(nil), component.Parameters...)
	}
	return out
}
