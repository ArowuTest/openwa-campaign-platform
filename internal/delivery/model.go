package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusAuthorised           Status = "AUTHORISED"
	StatusQueued               Status = "QUEUED"
	StatusClaimed              Status = "CLAIMED"
	StatusSubmitting           Status = "SUBMITTING"
	StatusGatewayAccepted      Status = "GATEWAY_ACCEPTED"
	StatusSent                 Status = "SENT"
	StatusDelivered            Status = "DELIVERED"
	StatusRead                 Status = "READ"
	StatusFailedRetryable      Status = "FAILED_RETRYABLE"
	StatusFailedPermanent      Status = "FAILED_PERMANENT"
	StatusUnknown              Status = "UNKNOWN"
	StatusSuppressedBeforeSend Status = "SUPPRESSED_BEFORE_SEND"
	StatusCancelled            Status = "CANCELLED"
)

type EventType string

const (
	EventQueued          EventType = "queued"
	EventClaimed         EventType = "claimed"
	EventSubmitting      EventType = "submitting"
	EventGatewayAccepted EventType = "gateway.accepted"
	EventSent            EventType = "message.sent"
	EventDelivered       EventType = "message.delivered"
	EventRead            EventType = "message.read"
	EventFailedRetryable EventType = "message.failed_retryable"
	EventFailedPermanent EventType = "message.failed_permanent"
	EventUnknown         EventType = "message.unknown"
	EventSuppressed      EventType = "message.suppressed"
	EventCancelled       EventType = "message.cancelled"
)

type Recipient struct {
	ID                      string
	CampaignID              string
	ContactID               string
	MessageVersionID        string
	IdempotencyKey          string
	Status                  Status
	HighestAcknowledgement  Status
	ProviderMessageID       string
	AttemptCount            int
	LastErrorCode           string
	LastErrorDetail         string
	SubmittedAt             *time.Time
	CompletedAt             *time.Time
	UpdatedAt               time.Time
	LastEventAt             *time.Time
	ReconciliationRequired  bool
	ContradictoryEventCount int
	AppliedEventKeys        map[string]struct{} `json:"-"`
}

type Event struct {
	DeduplicationKey  string
	Type              EventType
	ProviderEventID   string
	ProviderMessageID string
	ErrorCode         string
	ErrorDetail       string
	OccurredAt        time.Time
}

func NewIdempotencyKey(campaignID, contactID, messageVersionID string) (string, error) {
	if strings.TrimSpace(campaignID) == "" || strings.TrimSpace(contactID) == "" || strings.TrimSpace(messageVersionID) == "" {
		return "", errors.New("campaign, contact and message version are required")
	}
	sum := sha256.Sum256([]byte(campaignID + "\x1f" + contactID + "\x1f" + messageVersionID))
	return hex.EncodeToString(sum[:]), nil
}

// Fingerprint binds a deduplication key to one immutable event payload. A key
// reused with different content is a conflict, not an idempotent replay.
func Fingerprint(event Event) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		string(event.Type), event.ProviderEventID, event.ProviderMessageID, event.ErrorCode, event.ErrorDetail,
		event.OccurredAt.UTC().Format(time.RFC3339Nano),
	}, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// Apply is a deterministic delivery aggregate reducer. Durable stores must additionally
// enforce a unique constraint on Event.DeduplicationKey in the same transaction as the
// recipient update. The in-memory key set exists to make the reducer itself idempotent.
func Apply(recipient Recipient, event Event) (Recipient, bool, error) {
	if strings.TrimSpace(event.DeduplicationKey) == "" {
		return Recipient{}, false, errors.New("delivery event deduplication key is required")
	}
	if event.OccurredAt.IsZero() {
		return Recipient{}, false, errors.New("delivery event occurrence time is required")
	}
	next, err := statusForEvent(event.Type)
	if err != nil {
		return Recipient{}, false, err
	}
	if recipient.AppliedEventKeys == nil {
		recipient.AppliedEventKeys = make(map[string]struct{})
	}
	if _, exists := recipient.AppliedEventKeys[event.DeduplicationKey]; exists {
		return recipient, false, nil
	}
	recipient.AppliedEventKeys[event.DeduplicationKey] = struct{}{}
	occurred := event.OccurredAt.UTC()
	recipient.LastEventAt = &occurred

	if event.ProviderMessageID != "" {
		if recipient.ProviderMessageID != "" && recipient.ProviderMessageID != event.ProviderMessageID {
			recipient.ReconciliationRequired = true
			recipient.ContradictoryEventCount++
			recipient.LastErrorCode = "PROVIDER_MESSAGE_ID_MISMATCH"
			recipient.LastErrorDetail = "event referenced a different provider message identifier"
			recipient.UpdatedAt = occurred
			return recipient, true, nil
		}
		recipient.ProviderMessageID = event.ProviderMessageID
	}

	currentAck := acknowledgementRank(recipient.HighestAcknowledgement)
	if currentAck < 0 {
		currentAck = acknowledgementRank(recipient.Status)
	}
	nextAck := acknowledgementRank(next)

	// Provider acknowledgements may resolve an earlier terminal/unknown outcome, but
	// that contradiction remains visible for reconciliation.
	if nextAck >= 0 && terminal(recipient.Status) && currentAck < 0 {
		recipient.ReconciliationRequired = true
		recipient.ContradictoryEventCount++
	}

	// Queue/claim/submission callbacks are progress evidence only. A late lower-stage
	// callback must never downgrade an acknowledgement, an unknown outcome or a
	// terminal decision. FAILED_RETRYABLE is the sole state from which a new queued
	// attempt may legitimately restart the pre-send progression.
	if nextProgress := progressRank(next); nextProgress >= 0 {
		currentProgress := progressRank(recipient.Status)
		if currentAck >= 0 || recipient.Status == StatusUnknown || terminal(recipient.Status) {
			if recipient.Status != StatusFailedRetryable {
				recipient.UpdatedAt = maxTime(recipient.UpdatedAt, occurred)
				return recipient, true, nil
			}
		}
		if currentProgress >= nextProgress && recipient.Status != StatusFailedRetryable {
			recipient.UpdatedAt = maxTime(recipient.UpdatedAt, occurred)
			return recipient, true, nil
		}
	}

	// Acknowledgements may arrive out of order, but only the strongest evidence is retained.
	if nextAck >= 0 {
		if nextAck <= currentAck {
			recipient.UpdatedAt = maxTime(recipient.UpdatedAt, occurred)
			return recipient, true, nil
		}
		recipient.Status = next
		recipient.HighestAcknowledgement = next
		recipient.LastErrorCode = ""
		recipient.LastErrorDetail = ""
		recipient.UpdatedAt = maxTime(recipient.UpdatedAt, occurred)
		if next == StatusRead {
			completed := occurred
			recipient.CompletedAt = &completed
		}
		return recipient, true, nil
	}

	// A late failure or uncertainty cannot erase evidence that the provider reported sent,
	// delivered or read. It creates a reconciliation exception instead.
	if (failureStatus(next) || next == StatusUnknown) && currentAck >= acknowledgementRank(StatusSent) {
		recipient.ReconciliationRequired = true
		recipient.ContradictoryEventCount++
		recipient.LastErrorCode = nonEmpty(event.ErrorCode, "CONTRADICTORY_LATE_EVENT")
		recipient.LastErrorDetail = event.ErrorDetail
		recipient.UpdatedAt = maxTime(recipient.UpdatedAt, occurred)
		return recipient, true, nil
	}

	// Suppression and cancellation are valid only before provider acceptance. Once a send
	// has been accepted they are recorded as contradictions rather than rewriting history.
	if (next == StatusSuppressedBeforeSend || next == StatusCancelled) && currentAck >= acknowledgementRank(StatusGatewayAccepted) {
		recipient.ReconciliationRequired = true
		recipient.ContradictoryEventCount++
		recipient.LastErrorCode = "LATE_PRE_SEND_TERMINAL_EVENT"
		recipient.LastErrorDetail = fmt.Sprintf("%s arrived after provider acceptance", next)
		recipient.UpdatedAt = maxTime(recipient.UpdatedAt, occurred)
		return recipient, true, nil
	}

	if event.Type == EventSubmitting {
		recipient.AttemptCount++
		submitted := occurred
		recipient.SubmittedAt = &submitted
	}
	if failureStatus(next) || next == StatusUnknown {
		recipient.LastErrorCode = event.ErrorCode
		recipient.LastErrorDetail = event.ErrorDetail
	}
	recipient.Status = next
	recipient.UpdatedAt = maxTime(recipient.UpdatedAt, occurred)
	if terminal(next) {
		completed := occurred
		recipient.CompletedAt = &completed
	}
	return recipient, true, nil
}

func statusForEvent(event EventType) (Status, error) {
	switch event {
	case EventQueued:
		return StatusQueued, nil
	case EventClaimed:
		return StatusClaimed, nil
	case EventSubmitting:
		return StatusSubmitting, nil
	case EventGatewayAccepted:
		return StatusGatewayAccepted, nil
	case EventSent:
		return StatusSent, nil
	case EventDelivered:
		return StatusDelivered, nil
	case EventRead:
		return StatusRead, nil
	case EventFailedRetryable:
		return StatusFailedRetryable, nil
	case EventFailedPermanent:
		return StatusFailedPermanent, nil
	case EventUnknown:
		return StatusUnknown, nil
	case EventSuppressed:
		return StatusSuppressedBeforeSend, nil
	case EventCancelled:
		return StatusCancelled, nil
	default:
		return "", errors.New("unsupported delivery event")
	}
}

func acknowledgementRank(status Status) int {
	switch status {
	case StatusGatewayAccepted:
		return 1
	case StatusSent:
		return 2
	case StatusDelivered:
		return 3
	case StatusRead:
		return 4
	default:
		return -1
	}
}

func progressRank(status Status) int {
	switch status {
	case StatusAuthorised:
		return 0
	case StatusQueued:
		return 1
	case StatusClaimed:
		return 2
	case StatusSubmitting:
		return 3
	default:
		return -1
	}
}

func terminal(status Status) bool {
	switch status {
	case StatusRead, StatusFailedPermanent, StatusSuppressedBeforeSend, StatusCancelled:
		return true
	default:
		return false
	}
}

func failureStatus(status Status) bool {
	return status == StatusFailedRetryable || status == StatusFailedPermanent
}

func maxTime(current, candidate time.Time) time.Time {
	if current.After(candidate) {
		return current
	}
	return candidate
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
