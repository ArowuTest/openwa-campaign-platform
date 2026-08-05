package consent

import (
	"context"
	"errors"
	"strings"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/inbound"
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

type OptOutProcessor struct {
	Deliveries *delivery.Service
	Ledger     *LedgerService
	Policy     OptOutPolicy
	Policies   OptOutPolicyProvider
	ActorID    string
	Clock      func() time.Time
	Inbox      *inbound.Service
}

func (p *OptOutProcessor) Process(ctx context.Context, recipientID, eventID, messageText, sourceReference string) (OptOutResult, error) {
	if p == nil || p.Deliveries == nil || p.Ledger == nil {
		return OptOutResult{}, errors.New("opt-out processor dependencies are required")
	}
	if strings.TrimSpace(recipientID) == "" || strings.TrimSpace(eventID) == "" {
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
	recipient, err := p.Deliveries.Get(ctx, strings.TrimSpace(recipientID))
	if err != nil {
		return OptOutResult{}, err
	}
	if p.Inbox != nil {
		_, _, err = p.Inbox.Record(ctx, inbound.CreateInput{EventID: eventID, RecipientID: recipient.ID, ContactID: recipient.ContactID, CampaignID: recipient.CampaignID, SessionID: sourceReference, MessageText: messageText, Classification: map[bool]inbound.Classification{true: inbound.ClassificationOptOut, false: inbound.ClassificationUnreviewed}[recognised]})
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
	suppression, created, err := p.Ledger.CreateSuppression(ctx, SuppressionInput{
		ContactID:       recipient.ContactID,
		Scope:           SuppressionGlobal,
		Reason:          "INBOUND_STOP",
		EffectiveAt:     &now,
		SourceReference: strings.TrimSpace(sourceReference),
		CreatedBy:       actor,
		ClientRequestID: "gateway-inbound:" + strings.TrimSpace(eventID),
	})
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
