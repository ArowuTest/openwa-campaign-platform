package consent

import (
	"context"
	"errors"
	"strings"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/inbound"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

// DefaultGatewayServiceActorID identifies the internal gateway service in
// consent evidence. Deployments may later replace this with a provisioned
// service identity, but it must remain stable and attributable.
const DefaultGatewayServiceActorID = "00000000-0000-4000-8000-000000000002"

type OptOutPolicy struct {
	Keywords []string
}

func DefaultOptOutPolicy() OptOutPolicy {
	return OptOutPolicy{Keywords: []string{"STOP", "UNSUBSCRIBE", "CANCEL", "END", "QUIT", "OPTOUT", "OPT OUT"}}
}

type OptOutResult struct {
	Recognised  bool        `json:"recognised"`
	Replayed    bool        `json:"replayed"`
	ContactID   string      `json:"contactId,omitempty"`
	Suppression Suppression `json:"suppression,omitempty"`
}

type OptOutPolicyProvider interface {
	Active(context.Context) (OptOutPolicy, error)
}

type InboundRecipientEvidence struct {
	RecipientID string
	ContactID   string
	CampaignID  string
}

type InboundSenderResolver interface {
	ResolveInboundSender(context.Context, []byte, string) (InboundRecipientEvidence, error)
}

type InboundEventReference struct {
	ClientReference         string
	QuotedProviderMessageID string
	SenderMSISDN            string
	SessionID               string
	ProviderMessageID       string
}

type OptOutMetricRecorder interface {
	RecordOptOut(context.Context, string, string, time.Time) error
}

type OptOutProcessor struct {
	Deliveries *delivery.Service
	Ledger     *LedgerService
	Policy     OptOutPolicy
	Policies   OptOutPolicyProvider
	Metrics    OptOutMetricRecorder
	ActorID    string
	Clock      func() time.Time
	Inbox      *inbound.Service
	Protector  *sharedcrypto.MSISDNProtector
	Senders    InboundSenderResolver
}

func (p *OptOutProcessor) Process(ctx context.Context, recipientID, eventID, messageText, sourceReference string) (OptOutResult, error) {
	if p == nil || p.Deliveries == nil {
		return OptOutResult{}, errors.New("opt-out processor dependencies are required")
	}
	recipient, err := p.Deliveries.Get(ctx, strings.TrimSpace(recipientID))
	if err != nil {
		return OptOutResult{}, err
	}
	return p.processResolved(ctx, InboundRecipientEvidence{RecipientID: recipient.ID, ContactID: recipient.ContactID, CampaignID: recipient.CampaignID}, eventID, messageText, sourceReference, "")
}

func (p *OptOutProcessor) ProcessInbound(ctx context.Context, reference InboundEventReference, eventID, messageText, sourceReference string) (OptOutResult, error) {
	if p == nil || p.Deliveries == nil || p.Ledger == nil {
		return OptOutResult{}, errors.New("opt-out processor dependencies are required")
	}
	var evidence InboundRecipientEvidence
	var err error
	switch {
	case strings.TrimSpace(reference.ClientReference) != "":
		var recipient delivery.Recipient
		recipient, err = p.Deliveries.Get(ctx, strings.TrimSpace(reference.ClientReference))
		evidence = InboundRecipientEvidence{RecipientID: recipient.ID, ContactID: recipient.ContactID, CampaignID: recipient.CampaignID}
	case strings.TrimSpace(reference.QuotedProviderMessageID) != "":
		var recipient delivery.Recipient
		recipient, err = p.Deliveries.GetByProviderMessageID(ctx, strings.TrimSpace(reference.QuotedProviderMessageID))
		evidence = InboundRecipientEvidence{RecipientID: recipient.ID, ContactID: recipient.ContactID, CampaignID: recipient.CampaignID}
	case strings.TrimSpace(reference.SenderMSISDN) != "":
		if p.Protector == nil || p.Senders == nil {
			return OptOutResult{}, errors.New("sender correlation is not configured")
		}
		e164, normaliseErr := sharedcrypto.NormalizeE164(reference.SenderMSISDN)
		if normaliseErr != nil {
			return OptOutResult{}, normaliseErr
		}
		evidence, err = p.Senders.ResolveInboundSender(ctx, p.Protector.LookupHMAC(e164), strings.TrimSpace(reference.SessionID))
	default:
		return OptOutResult{}, errors.New("inbound event has no recipient correlation evidence")
	}
	if err != nil {
		return OptOutResult{}, err
	}
	return p.processResolved(ctx, evidence, eventID, messageText, sourceReference, strings.TrimSpace(reference.ProviderMessageID))
}

func (p *OptOutProcessor) processResolved(ctx context.Context, recipient InboundRecipientEvidence, eventID, messageText, sourceReference, providerMessageID string) (OptOutResult, error) {
	if p == nil || p.Ledger == nil {
		return OptOutResult{}, errors.New("opt-out ledger is required")
	}
	if strings.TrimSpace(recipient.RecipientID) == "" || strings.TrimSpace(recipient.ContactID) == "" || strings.TrimSpace(eventID) == "" {
		return OptOutResult{}, errors.New("recipient and event identifiers are required")
	}
	policy := p.Policy
	if p.Policies != nil {
		governed, policyErr := p.Policies.Active(ctx)
		if policyErr != nil {
			return OptOutResult{}, policyErr
		}
		policy = governed
	}
	if len(policy.Keywords) == 0 {
		policy = DefaultOptOutPolicy()
	}
	recognised := policy.Recognises(messageText)
	if p.Inbox != nil {
		_, _, err := p.Inbox.Record(ctx, inbound.CreateInput{EventID: eventID, RecipientID: recipient.RecipientID, ContactID: recipient.ContactID, CampaignID: recipient.CampaignID, SessionID: sourceReference, ProviderMessageID: providerMessageID, MessageText: messageText, Classification: map[bool]inbound.Classification{true: inbound.ClassificationOptOut, false: inbound.ClassificationUnreviewed}[recognised]})
		if err != nil {
			return OptOutResult{}, err
		}
	}
	if !recognised {
		return OptOutResult{Recognised: false, ContactID: recipient.ContactID}, nil
	}
	actor := strings.TrimSpace(p.ActorID)
	if actor == "" {
		actor = DefaultGatewayServiceActorID
	}
	now := time.Now().UTC()
	if p.Clock != nil {
		now = p.Clock().UTC()
	}
	suppressionInput := SuppressionInput{
		ContactID: recipient.ContactID, Scope: SuppressionGlobal, Reason: "INBOUND_STOP", EffectiveAt: &now,
		SourceReference: strings.TrimSpace(sourceReference), CreatedBy: actor, ClientRequestID: "gateway-inbound:" + strings.TrimSpace(eventID),
	}
	campaignID := strings.TrimSpace(recipient.CampaignID)
	var suppression Suppression
	var created bool
	var err error
	if campaignID != "" && p.Ledger.SupportsAtomicOptOutMetric() {
		suppression, created, err = p.Ledger.CreateSuppressionWithOptOutMetric(ctx, suppressionInput, campaignID, now)
	} else {
		suppression, created, err = p.Ledger.CreateSuppression(ctx, suppressionInput)
		if err == nil && p.Metrics != nil && campaignID != "" {
			err = p.Metrics.RecordOptOut(ctx, campaignID, suppression.ID, now)
		}
	}
	if err != nil {
		return OptOutResult{}, err
	}
	return OptOutResult{Recognised: true, Replayed: !created, ContactID: recipient.ContactID, Suppression: suppression}, nil
}

func (p OptOutPolicy) Recognises(value string) bool {
	normalised := normaliseOptOutText(value)
	if normalised == "" {
		return false
	}
	for _, keyword := range p.Keywords {
		if normaliseOptOutText(keyword) == normalised {
			return true
		}
	}
	return false
}

func normaliseOptOutText(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.Trim(value, " .,!?:;\t\r\n")
	return strings.Join(strings.Fields(value), " ")
}
