package httpserver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"campaign-platform/internal/consent"
	"campaign-platform/internal/delivery"
	"campaign-platform/internal/metacloud"
	"campaign-platform/internal/shared/httpx"
)

type MetaWebhookSenderResolver interface {
	ResolveMetaWebhookSender(context.Context, string, string, string) (metacloud.Sender, error)
}

type MetaWebhookDeliveryResolver interface {
	ResolveMetaWebhookRecipient(context.Context, metacloud.Sender, string) (delivery.Recipient, error)
	ResolveMetaWebhookInboundRecipient(context.Context, metacloud.Sender, string) (delivery.Recipient, error)
}

type MetaConversationWindowStore interface {
	ObserveInbound(context.Context, metacloud.ConversationWindowObservation, time.Duration) (metacloud.ConversationWindowEvidence, bool, error)
}

func (s *Server) verifyMetaWebhook(w http.ResponseWriter, r *http.Request) {
	if s.deps.MetaCredentials == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "META_WEBHOOK_UNAVAILABLE", "Meta webhook verification is not configured.", nil)
		return
	}
	credential, err := s.deps.MetaCredentials.Resolve(strings.TrimSpace(r.PathValue("credentialKey")))
	if err != nil {
		httpx.WriteError(w, r, http.StatusForbidden, "META_WEBHOOK_VERIFICATION_FAILED", "The Meta webhook verification request was rejected.", nil)
		return
	}
	challenge, err := metacloud.VerifyWebhookChallenge(credential, r.URL.Query().Get("hub.mode"), r.URL.Query().Get("hub.verify_token"), r.URL.Query().Get("hub.challenge"))
	if err != nil {
		httpx.WriteError(w, r, http.StatusForbidden, "META_WEBHOOK_VERIFICATION_FAILED", "The Meta webhook verification request was rejected.", nil)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, challenge)
}

func (s *Server) ingestMetaWebhook(w http.ResponseWriter, r *http.Request) {
	if s.deps.MetaCredentials == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "META_WEBHOOK_UNAVAILABLE", "Meta webhook processing is not configured.", nil)
		return
	}
	credentialKey := strings.TrimSpace(r.PathValue("credentialKey"))
	credential, err := s.deps.MetaCredentials.Resolve(credentialKey)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnauthorized, "META_WEBHOOK_SIGNATURE_INVALID", "The Meta webhook could not be authenticated.", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.deps.MetaWebhookMaxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpx.WriteError(w, r, http.StatusRequestEntityTooLarge, "META_WEBHOOK_TOO_LARGE", "The Meta webhook exceeded the permitted size.", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusBadRequest, "META_WEBHOOK_UNREADABLE", "The Meta webhook body could not be read.", nil)
		return
	}
	if s.deps.MetaWebhookSenders == nil || s.deps.MetaWebhookDeliveries == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "META_WEBHOOK_UNAVAILABLE", "Meta webhook processing is not configured.", nil)
		return
	}
	if err := metacloud.VerifyWebhookSignature(credential.AppSecret, r.Header.Get(metacloud.WebhookSignatureHeader), body); err != nil {
		httpx.WriteError(w, r, http.StatusUnauthorized, "META_WEBHOOK_SIGNATURE_INVALID", "The Meta webhook could not be authenticated.", nil)
		return
	}
	notification, err := metacloud.DecodeWebhook(body)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "META_WEBHOOK_INVALID", "The Meta webhook payload is invalid.", nil)
		return
	}
	resolvedSenders := make([]metacloud.Sender, len(notification.Changes))
	boundChanges := make([]bool, len(notification.Changes))
	boundCount := 0
	for i, change := range notification.Changes {
		sender, resolveErr := s.resolveMetaWebhookSender(r.Context(), credentialKey, change.WABAID, change.PhoneNumberID)
		if errors.Is(resolveErr, metacloud.ErrNotFound) {
			continue
		}
		if resolveErr != nil {
			s.internalError(w, r, resolveErr)
			return
		}
		resolvedSenders[i] = sender
		boundChanges[i] = true
		boundCount++
	}
	if len(notification.Changes) > 0 && boundCount == 0 {
		httpx.WriteError(w, r, http.StatusForbidden, "META_WEBHOOK_SENDER_UNBOUND", "The Meta webhook sender is not bound to this endpoint.", nil)
		return
	}
	result := metaWebhookResult{}
	pendingStatus := false
	timestampSkew := false
	var processingErr error
	for i, change := range notification.Changes {
		if !boundChanges[i] {
			continue
		}
		sender := resolvedSenders[i]
		if err := s.applyMetaStatuses(r.Context(), sender, change.Statuses, &result); err != nil {
			if errors.Is(err, errMetaWebhookRecipientPending) {
				pendingStatus = true
			} else if errors.Is(err, errMetaWebhookTimestampSkew) {
				timestampSkew = true
			} else if processingErr == nil {
				processingErr = err
			}
		}
		if err := s.applyMetaInbound(r.Context(), sender, change.Messages, &result); err != nil {
			if errors.Is(err, errMetaWebhookTimestampSkew) {
				timestampSkew = true
			} else if processingErr == nil {
				processingErr = err
			}
		}
	}
	if pendingStatus {
		s.writeMetaWebhookProcessingError(w, r, errMetaWebhookRecipientPending)
		return
	}
	if timestampSkew {
		s.writeMetaWebhookProcessingError(w, r, errMetaWebhookTimestampSkew)
		return
	}
	if processingErr != nil {
		s.writeMetaWebhookProcessingError(w, r, processingErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"accepted": true, "statusEvents": result.StatusEvents, "replayedStatuses": result.ReplayedStatuses,
		"ignoredStatuses": result.IgnoredStatuses, "inboundMessages": result.InboundMessages,
		"replayedInbound": result.ReplayedInbound, "recognisedOptOuts": result.RecognisedOptOuts, "ignoredInbound": result.IgnoredInbound,
	})
}

const metaWebhookMaxFutureSkew = 5 * time.Minute

var (
	errMetaWebhookRecipientPending = errors.New("Meta webhook recipient is not yet visible")
	errMetaWebhookTimestampSkew    = errors.New("Meta webhook event timestamp exceeds receipt skew")
)

type metaWebhookResult struct {
	StatusEvents, ReplayedStatuses, IgnoredStatuses int
	InboundMessages, ReplayedInbound                int
	RecognisedOptOuts, IgnoredInbound               int
}

func (s *Server) resolveMetaWebhookSender(ctx context.Context, credentialKey, wabaID, phoneNumberID string) (metacloud.Sender, error) {
	if s.deps.MetaWebhookSenders == nil {
		return metacloud.Sender{}, errors.New("Meta webhook sender resolver is not configured")
	}
	sender, err := s.deps.MetaWebhookSenders.ResolveMetaWebhookSender(ctx, strings.TrimSpace(credentialKey), strings.TrimSpace(wabaID), strings.TrimSpace(phoneNumberID))
	if err != nil {
		return metacloud.Sender{}, err
	}
	if strings.TrimSpace(sender.OrganisationID) == "" {
		return metacloud.Sender{}, errors.New("Meta webhook sender is missing organisation authority")
	}
	statusOK := sender.Status == metacloud.StatusActive || sender.Status == metacloud.StatusRetired
	if !statusOK || strings.TrimSpace(sender.ID) == "" || sender.CredentialKey != strings.TrimSpace(credentialKey) || sender.WABAID != strings.TrimSpace(wabaID) || sender.PhoneNumberID != strings.TrimSpace(phoneNumberID) {
		return metacloud.Sender{}, metacloud.ErrNotFound
	}
	return sender, nil
}

func (s *Server) applyMetaStatuses(ctx context.Context, sender metacloud.Sender, statuses []metacloud.WebhookStatus, result *metaWebhookResult) error {
	if len(statuses) == 0 {
		return nil
	}
	if s.deps.DeliveryEvents == nil || s.deps.MetaWebhookDeliveries == nil {
		return errors.New("sender-bound delivery event service is not configured for Meta webhooks")
	}
	pending := false
	timestampSkew := false
	var replayConflict error
	for _, status := range statuses {
		if status.OccurredAt.After(time.Now().UTC().Add(metaWebhookMaxFutureSkew)) {
			timestampSkew = true
			continue
		}
		recipient, err := s.deps.MetaWebhookDeliveries.ResolveMetaWebhookRecipient(ctx, sender, status.ProviderMessageID)
		if errors.Is(err, metacloud.ErrWebhookSenderMismatch) {
			result.IgnoredStatuses++
			continue
		}
		if errors.Is(err, delivery.ErrRecipientNotFound) {
			pending = true
			continue
		}
		if err != nil {
			return err
		}
		event, ok := metaDeliveryEvent(status)
		if !ok {
			result.IgnoredStatuses++
			continue
		}
		_, changed, err := s.deps.DeliveryEvents.ApplyEvent(ctx, recipient.ID, event)
		if errors.Is(err, delivery.ErrEventDedupMismatch) {
			if replayConflict == nil {
				replayConflict = err
			}
			continue
		}
		if err != nil {
			return err
		}
		result.StatusEvents++
		if !changed {
			result.ReplayedStatuses++
		}
	}
	if pending {
		return errMetaWebhookRecipientPending
	}
	if timestampSkew {
		return errMetaWebhookTimestampSkew
	}
	if replayConflict != nil {
		return replayConflict
	}
	return nil
}

func metaDeliveryEvent(status metacloud.WebhookStatus) (delivery.Event, bool) {
	var eventType delivery.EventType
	switch status.Status {
	case "sent":
		eventType = delivery.EventSent
	case "delivered":
		eventType = delivery.EventDelivered
	case "read":
		eventType = delivery.EventRead
	case "failed":
		eventType = delivery.EventFailedPermanent
	default:
		return delivery.Event{}, false
	}
	key := "meta-status:" + status.ProviderMessageID + ":" + status.Status + ":" + strconv.FormatInt(status.OccurredAt.Unix(), 10)
	return delivery.Event{
		DeduplicationKey: key, Type: eventType, ProviderEventID: key, ProviderMessageID: status.ProviderMessageID,
		ErrorCode: status.ErrorCode, ErrorDetail: status.ErrorDetail, OccurredAt: status.OccurredAt,
	}, true
}

func (s *Server) applyMetaInbound(ctx context.Context, sender metacloud.Sender, messages []metacloud.WebhookMessage, result *metaWebhookResult) error {
	if len(messages) == 0 {
		return nil
	}
	if s.deps.OptOutProcessor == nil || s.deps.MetaWebhookDeliveries == nil {
		return errors.New("sender-bound opt-out processing is not configured for Meta webhooks")
	}
	receiptCutoff := time.Now().UTC().Add(metaWebhookMaxFutureSkew)
	timestampSkew := false
	var replayConflict error
	for _, message := range messages {
		if message.OccurredAt.IsZero() || message.OccurredAt.After(receiptCutoff) {
			timestampSkew = true
			continue
		}
		var recipient delivery.Recipient
		var err error
		if strings.TrimSpace(message.QuotedProviderMessageID) != "" {
			recipient, err = s.deps.MetaWebhookDeliveries.ResolveMetaWebhookRecipient(ctx, sender, message.QuotedProviderMessageID)
			if errors.Is(err, metacloud.ErrWebhookSenderMismatch) || errors.Is(err, delivery.ErrRecipientNotFound) {
				recipient, err = s.deps.MetaWebhookDeliveries.ResolveMetaWebhookInboundRecipient(ctx, sender, message.From)
			} else if err == nil {
				fromRecipient, fromErr := s.deps.MetaWebhookDeliveries.ResolveMetaWebhookInboundRecipient(ctx, sender, message.From)
				switch {
				case errors.Is(fromErr, delivery.ErrRecipientNotFound):
					result.IgnoredInbound++
					continue
				case fromErr != nil:
					return fromErr
				case strings.TrimSpace(fromRecipient.ContactID) == "", strings.TrimSpace(recipient.ContactID) == "", fromRecipient.ContactID != recipient.ContactID:
					result.IgnoredInbound++
					continue
				}
			}
		} else {
			recipient, err = s.deps.MetaWebhookDeliveries.ResolveMetaWebhookInboundRecipient(ctx, sender, message.From)
		}
		if errors.Is(err, delivery.ErrRecipientNotFound) {
			result.IgnoredInbound++
			continue
		}
		if err != nil {
			return err
		}
		contactID := strings.TrimSpace(recipient.ContactID)
		if contactID == "" || s.deps.MetaConversationWindows == nil || s.deps.MetaConversationWindow <= 0 {
			return errors.New("Meta conversation-window evidence is not configured for resolved inbound contact")
		}
		_, _, err = s.deps.MetaConversationWindows.ObserveInbound(ctx, metacloud.ConversationWindowObservation{
			MetaSenderID: sender.ID, ContactID: contactID,
			ProviderMessageID: message.MessageID, OccurredAt: message.OccurredAt,
		}, s.deps.MetaConversationWindow)
		if errors.Is(err, metacloud.ErrConversationWindowConflict) {
			if replayConflict == nil {
				replayConflict = err
			}
		} else if err != nil {
			return err
		}
		// Suppression is independently authoritative. Even when window evidence
		// conflicts with prior evidence, an authenticated STOP must still reach
		// the consent ledger before the batch-level replay conflict is returned.
		processed, err := s.deps.OptOutProcessor.ProcessInbound(ctx, consent.InboundEventReference{
			ClientReference:   recipient.ID,
			ProviderMessageID: message.MessageID,
		}, message.MessageID, message.Text, "meta:"+sender.ID+":"+message.MessageID)
		if errors.Is(err, consent.ErrLedgerReplayConflict) {
			if replayConflict == nil {
				replayConflict = err
			}
			continue
		}
		if errors.Is(err, delivery.ErrRecipientNotFound) {
			result.IgnoredInbound++
			continue
		}
		if err != nil {
			return err
		}
		result.InboundMessages++
		if processed.Replayed {
			result.ReplayedInbound++
		}
		if processed.Recognised {
			result.RecognisedOptOuts++
		}
	}
	if timestampSkew {
		return errMetaWebhookTimestampSkew
	}
	if replayConflict != nil {
		return replayConflict
	}
	return nil
}

func (s *Server) writeMetaWebhookProcessingError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, delivery.ErrEventDedupMismatch), errors.Is(err, consent.ErrLedgerReplayConflict), errors.Is(err, metacloud.ErrConversationWindowConflict):
		httpx.WriteError(w, r, http.StatusConflict, "META_WEBHOOK_REPLAY_CONFLICT", "The Meta webhook event identifier conflicted with prior evidence.", nil)
	case errors.Is(err, errMetaWebhookRecipientPending):
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "META_WEBHOOK_RECIPIENT_PENDING", "The Meta delivery recipient is not yet available for correlation; retry the webhook.", nil)
	case errors.Is(err, errMetaWebhookTimestampSkew):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "META_WEBHOOK_TIMESTAMP_INVALID", "The Meta webhook event timestamp exceeded the permitted receipt skew.", nil)
	default:
		s.internalError(w, r, err)
	}
}
