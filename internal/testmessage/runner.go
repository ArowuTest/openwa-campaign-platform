package testmessage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/dispatch"
	"campaign-platform/internal/message"
	"campaign-platform/internal/shared/crypto"
)

type MediaResolver interface {
	Resolve(context.Context, string, time.Duration) (string, error)
}

type Processor struct {
	Repository Repository
	Protector  *crypto.MSISDNProtector
	Messages   *message.Service
	Gateway    dispatch.Gateway
	Routes     RouteValidator
	Pacing     dispatch.PacingController
	Media      MediaResolver
	Clock      func() time.Time
}

func (p *Processor) Process(ctx context.Context, send Send) error {
	if p == nil || p.Repository == nil || p.Protector == nil || p.Messages == nil || p.Gateway == nil || p.Routes == nil {
		return errors.New("test-message processor dependencies are required")
	}
	now := time.Now().UTC()
	if p.Clock != nil {
		now = p.Clock().UTC()
	}
	recipient, err := p.Repository.GetRecipient(ctx, send.TestRecipientID)
	if err != nil {
		return p.finish(ctx, send, SendFailed, "", "TEST_RECIPIENT_NOT_FOUND", now)
	}
	if recipient.Status != RecipientActive {
		return p.finish(ctx, send, SendFailed, "", "TEST_RECIPIENT_NOT_ACTIVE", now)
	}
	e164, err := p.Protector.Decrypt(recipient.MSISDNEncrypted)
	if err != nil {
		return p.finish(ctx, send, SendFailed, "", "TEST_RECIPIENT_DECRYPT_FAILED", now)
	}
	version, err := p.Messages.Get(ctx, send.MessageVersionID)
	if err != nil {
		return p.finish(ctx, send, SendFailed, "", "MESSAGE_VERSION_NOT_FOUND", now)
	}
	if version.ContentHash != send.MessageContentHash || version.CampaignID != send.CampaignID {
		return p.finish(ctx, send, SendFailed, "", "MESSAGE_VERSION_CHANGED", now)
	}
	requiredCapability, err := testMessageCapability(version.Type)
	if err != nil {
		return p.finish(ctx, send, SendFailed, "", "MESSAGE_TYPE_UNSUPPORTED", now)
	}
	routeEvidence, err := p.Routes.ValidateTestRoute(ctx, RouteRequirements{
		CampaignID: send.CampaignID, GatewayPoolID: send.GatewayPoolID, SenderPoolID: send.SenderPoolID,
		Provider: send.Provider, Engine: send.Engine, SessionID: send.SenderSessionID,
		RequiredCapabilities: []string{requiredCapability}, At: now,
	})
	if err != nil {
		return p.finish(ctx, send, SendFailed, "", "TEST_ROUTE_NO_LONGER_VALID", now)
	}
	if routeEvidence.GatewayPoolVersion != send.GatewayPoolVersion ||
		routeEvidence.AdapterVersion != send.ProviderAdapterVersion ||
		routeEvidence.ProviderDefinitionID != send.ProviderDefinitionID ||
		routeEvidence.ProviderDefinitionVersion != send.ProviderDefinitionVersion {
		return p.finish(ctx, send, SendFailed, "", "TEST_ROUTE_EVIDENCE_CHANGED", now)
	}
	rendered, err := message.Render(version, message.RenderInput{Values: send.VariableValues, Mode: message.RenderDispatch})
	if err != nil {
		return p.finish(ctx, send, SendFailed, "", "MESSAGE_RENDER_FAILED", now)
	}
	material := dispatch.Material{
		SenderPoolID: send.SenderPoolID, Provider: send.Provider, Engine: send.Engine,
		GatewayPoolID: send.GatewayPoolID, GatewayPoolVersion: routeEvidence.GatewayPoolVersion,
		GatewayAdapterVersion: routeEvidence.AdapterVersion, GatewayNodeID: routeEvidence.GatewayNodeID,
		GatewayNodeVersion: routeEvidence.GatewayNodeVersion, SessionID: send.SenderSessionID,
		SessionLeaseVersion: routeEvidence.SessionLeaseVersion, SessionConfigurationVersion: routeEvidence.SessionConfigurationVersion,
		AuthorityExpiresAt: routeEvidence.AuthorityExpiresAt, RouteReference: send.RouteReference,
		RecipientE164: e164, MessageType: strings.ToLower(string(version.Type)), Body: rendered.Body,
		ClientReference: "test-send:" + send.ID,
	}
	if version.Type == message.TypeImageCaption {
		material.MessageType = "image"
	}
	if version.Type == message.TypeVideo {
		material.MessageType = "video"
	}
	if version.Type == message.TypeDocument {
		material.MessageType = "document"
	}
	if version.Type == message.TypeText {
		material.MessageType = "text"
	}
	if version.Media != nil {
		if p.Media == nil {
			return p.finish(ctx, send, SendFailed, "", "MEDIA_RESOLVER_UNAVAILABLE", now)
		}
		material.MediaObjectURL, err = p.Media.Resolve(ctx, version.Media.ObjectKey, 15*time.Minute)
		if err != nil {
			return p.finish(ctx, send, SendFailed, "", "MEDIA_URL_FAILED", now)
		}
	}
	if p.Pacing != nil {
		dr := delivery.Recipient{ID: send.ID, CampaignID: send.CampaignID, IdempotencyKey: "test:" + send.IdempotencyKey}
		if err = p.Pacing.Wait(ctx, dr, material, now); err != nil {
			return p.finish(ctx, send, SendPending, "", "PACING_WAIT_FAILED", now)
		}
	}
	result, err := p.Gateway.Send(ctx, dispatch.GatewayRequest{
		IdempotencyKey: "test:" + send.IdempotencyKey, Provider: send.Provider, Engine: send.Engine,
		GatewayPoolID: send.GatewayPoolID, GatewayPoolVersion: routeEvidence.GatewayPoolVersion,
		GatewayAdapterVersion: routeEvidence.AdapterVersion, GatewayNodeID: routeEvidence.GatewayNodeID,
		GatewayNodeVersion: routeEvidence.GatewayNodeVersion, SessionID: send.SenderSessionID,
		SessionLeaseVersion: routeEvidence.SessionLeaseVersion, SessionConfigurationVersion: routeEvidence.SessionConfigurationVersion,
		AuthorityExpiresAt: routeEvidence.AuthorityExpiresAt, RouteReference: send.RouteReference,
		RecipientE164: e164, MessageType: material.MessageType, Body: material.Body,
		MediaURL: material.MediaObjectURL, ClientReference: material.ClientReference,
	})
	if err != nil {
		var ge dispatch.GatewayError
		if !errors.As(err, &ge) {
			ge = dispatch.GatewayError{Code: "TEST_GATEWAY_UNKNOWN", Safety: dispatch.FailureOutcomeUnknown}
		}
		switch ge.Safety {
		case dispatch.FailureSafeToRetry:
			return p.finish(ctx, send, SendPending, "", nonEmpty(ge.Code, "TEST_GATEWAY_RETRYABLE"), now)
		case dispatch.FailurePermanent:
			return p.finish(ctx, send, SendFailed, "", nonEmpty(ge.Code, "TEST_GATEWAY_PERMANENT"), now)
		default:
			return p.finish(ctx, send, SendUnknown, "", nonEmpty(ge.Code, "TEST_GATEWAY_UNKNOWN"), now)
		}
	}
	if !result.Accepted || strings.TrimSpace(result.ProviderMessageID) == "" {
		return p.finish(ctx, send, SendUnknown, "", "TEST_GATEWAY_INVALID_ACCEPTANCE", now)
	}
	return p.finish(ctx, send, SendAccepted, result.ProviderMessageID, "", now)
}
func (p *Processor) finish(ctx context.Context, s Send, status SendStatus, providerID, code string, now time.Time) error {
	return p.Repository.CompleteSend(ctx, s, status, providerID, code, now)
}
func nonEmpty(v, f string) string {
	if strings.TrimSpace(v) == "" {
		return f
	}
	return strings.TrimSpace(v)
}

type Runner struct {
	Repository   Repository
	Processor    *Processor
	Owner        string
	Lease        time.Duration
	PollInterval time.Duration
	Batch        int
	active       atomic.Int64
}

func (r *Runner) Active() int64 {
	if r == nil {
		return 0
	}
	return r.active.Load()
}
func (r *Runner) Run(ctx context.Context) error {
	if r == nil || r.Repository == nil || r.Processor == nil || strings.TrimSpace(r.Owner) == "" {
		return errors.New("test-message runner dependencies are required")
	}
	if r.Lease <= 0 {
		r.Lease = 2 * time.Minute
	}
	if r.PollInterval <= 0 {
		r.PollInterval = time.Second
	}
	if r.Batch <= 0 || r.Batch > 100 {
		r.Batch = 20
	}
	ticker := time.NewTicker(r.PollInterval)
	defer ticker.Stop()
	for {
		now := time.Now().UTC()
		items, err := r.Repository.ClaimSends(ctx, r.Owner, now, r.Lease, r.Batch)
		if err != nil {
			return fmt.Errorf("claim test messages: %w", err)
		}
		for _, item := range items {
			r.active.Add(1)
			err = r.Processor.Process(ctx, item)
			r.active.Add(-1)
			if err != nil && !errors.Is(err, context.Canceled) {
				return fmt.Errorf("process test message %s: %w", item.ID, err)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
