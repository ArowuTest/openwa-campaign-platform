package dispatch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/jobs"
)

const JobType = "DISPATCH_CAMPAIGN_RECIPIENT"

type JobPayload struct {
	CampaignRecipientID string `json:"campaignRecipientId"`
}

type Material struct {
	SenderPoolID                string
	Provider                    string
	Engine                      string
	GatewayPoolID               string
	GatewayPoolVersion          int64
	GatewayAdapterVersion       string
	GatewayNodeID               string
	GatewayNodeVersion          int64
	SessionID                   string
	SessionLeaseVersion         int64
	SessionConfigurationVersion int64
	AuthorityExpiresAt          time.Time
	RouteReference              string
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
	GatewayNodeVersion          int64
	SessionID                   string
	SessionLeaseVersion         int64
	SessionConfigurationVersion int64
	AuthorityExpiresAt          time.Time
	RouteReference              string
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

type Handler struct {
	Ledger      *delivery.Service
	Materials   MaterialLoader
	Eligibility FinalEligibility
	Gateway     Gateway
	Throttle    Throttle
	Pacing      PacingController
	Clock       func() time.Time
}

func (h *Handler) Handle(ctx context.Context, job jobs.Job) error {
	if h == nil || h.Ledger == nil || h.Materials == nil || h.Eligibility == nil || h.Gateway == nil {
		return errors.New("dispatch handler dependencies are required")
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
	decision, err := h.Eligibility.Check(ctx, recipient, now)
	if err != nil {
		return jobs.RetryableError{Code: "ELIGIBILITY_CHECK_FAILED", Err: err}
	}
	if !decision.Eligible {
		_, _, err = h.Ledger.ApplyEvent(ctx, recipient.ID, delivery.Event{DeduplicationKey: "final-exclusion:" + recipient.IdempotencyKey, Type: delivery.EventSuppressed, ErrorCode: nonEmpty(decision.Reason, "INELIGIBLE_FINAL_CHECK"), OccurredAt: now})
		return err
	}
	material, err := h.Materials.Load(ctx, recipient)
	if err != nil {
		var permanent PermanentMaterialError
		if errors.As(err, &permanent) {
			return permanent
		}
		return jobs.RetryableError{Code: "MATERIAL_LOAD_FAILED", Err: err}
	}
	if err := validateMaterial(material); err != nil {
		return err
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
	if _, _, err = h.Ledger.ApplyEvent(ctx, recipient.ID, delivery.Event{DeduplicationKey: "submitting:" + attemptKey, Type: delivery.EventSubmitting, OccurredAt: now}); err != nil {
		return err
	}
	result, sendErr := h.Gateway.Send(ctx, GatewayRequest{
		IdempotencyKey: recipient.IdempotencyKey, Provider: material.Provider, Engine: material.Engine,
		GatewayPoolID: material.GatewayPoolID, GatewayPoolVersion: material.GatewayPoolVersion,
		GatewayAdapterVersion: material.GatewayAdapterVersion, GatewayNodeID: material.GatewayNodeID,
		GatewayNodeVersion: material.GatewayNodeVersion, SessionID: material.SessionID,
		SessionLeaseVersion: material.SessionLeaseVersion, SessionConfigurationVersion: material.SessionConfigurationVersion,
		AuthorityExpiresAt: material.AuthorityExpiresAt, RouteReference: material.RouteReference,
		RecipientE164: material.RecipientE164, MessageType: material.MessageType, Body: material.Body,
		MediaURL: material.MediaObjectURL, ClientReference: material.ClientReference,
	})
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
	case delivery.StatusFailedPermanent, delivery.StatusSuppressedBeforeSend, delivery.StatusCancelled:
		return true
	default:
		return false
	}
}
func validateMaterial(v Material) error {
	if strings.TrimSpace(v.Provider) == "" || strings.TrimSpace(v.Engine) == "" || strings.TrimSpace(v.GatewayPoolID) == "" ||
		v.GatewayPoolVersion <= 0 || strings.TrimSpace(v.GatewayAdapterVersion) == "" || strings.TrimSpace(v.GatewayNodeID) == "" ||
		v.GatewayNodeVersion <= 0 || strings.TrimSpace(v.SessionID) == "" || v.SessionLeaseVersion <= 0 ||
		v.SessionConfigurationVersion <= 0 || v.AuthorityExpiresAt.IsZero() || strings.TrimSpace(v.RouteReference) == "" ||
		!strings.HasPrefix(v.RecipientE164, "+") {
		return errors.New("dispatch material requires session and E.164 recipient")
	}
	if v.MessageType == "text" && strings.TrimSpace(v.Body) == "" {
		return errors.New("text dispatch requires body")
	}
	if v.MessageType != "text" && strings.TrimSpace(v.MediaObjectURL) == "" {
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
