package inbound

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"campaign-platform/internal/shared/id"
)

var (
	ErrNotFound       = errors.New("inbound reply not found")
	ErrConflict       = errors.New("inbound reply version conflict")
	ErrReplayConflict = errors.New("inbound event replay conflict")
)

type Classification string

const (
	ClassificationUnreviewed Classification = "UNREVIEWED"
	ClassificationOptOut     Classification = "OPT_OUT"
	ClassificationQuestion   Classification = "QUESTION"
	ClassificationComplaint  Classification = "COMPLAINT"
	ClassificationOther      Classification = "OTHER"
)

type Reply struct {
	ID                  string         `json:"id"`
	EventID             string         `json:"eventId"`
	RecipientID         string         `json:"recipientId"`
	ContactID           string         `json:"contactId"`
	CampaignID          string         `json:"campaignId"`
	SessionID           string         `json:"sessionId"`
	ProviderMessageID   string         `json:"providerMessageId,omitempty"`
	MessageText         string         `json:"messageText,omitempty"`
	ContentRetainUntil  time.Time      `json:"contentRetainUntil"`
	ContentRedactedAt   *time.Time     `json:"contentRedactedAt,omitempty"`
	LegalHold           bool           `json:"legalHold"`
	LegalHoldReason     string         `json:"legalHoldReason,omitempty"`
	LegalHoldAppliedBy  string         `json:"legalHoldAppliedBy,omitempty"`
	LegalHoldAppliedAt  *time.Time     `json:"legalHoldAppliedAt,omitempty"`
	LegalHoldReleasedBy string         `json:"legalHoldReleasedBy,omitempty"`
	LegalHoldReleasedAt *time.Time     `json:"legalHoldReleasedAt,omitempty"`
	MessageFingerprint  string         `json:"messageFingerprint"`
	Classification      Classification `json:"classification"`
	Escalated           bool           `json:"escalated"`
	EscalationReason    string         `json:"escalationReason,omitempty"`
	OccurredAt          time.Time      `json:"occurredAt"`
	CreatedAt           time.Time      `json:"createdAt"`
	ReviewedAt          *time.Time     `json:"reviewedAt,omitempty"`
	ReviewedBy          string         `json:"reviewedBy,omitempty"`
	Version             int64          `json:"version"`
}

type CreateInput struct {
	EventID, RecipientID, ContactID, CampaignID, SessionID, ProviderMessageID, MessageText string
	Classification                                                                         Classification
	OccurredAt                                                                             time.Time
}

type Summary struct {
	Total      int64 `json:"total"`
	Unreviewed int64 `json:"unreviewed"`
	OptOut     int64 `json:"optOut"`
	Questions  int64 `json:"questions"`
	Complaints int64 `json:"complaints"`
	Other      int64 `json:"other"`
	Escalated  int64 `json:"escalated"`
}

type Repository interface {
	Create(context.Context, Reply) (Reply, bool, error)
	Get(context.Context, string) (Reply, error)
	List(context.Context, int) ([]Reply, error)
	UpdateReview(context.Context, string, int64, Classification, bool, string, string, time.Time) (Reply, error)
	Summary(context.Context, string) (Summary, error)
	RedactExpired(context.Context, time.Time) (int64, error)
	SetLegalHold(context.Context, string, int64, bool, string, string, time.Time) (Reply, error)
}

type RetentionResolver interface {
	ActiveDuration(context.Context) (time.Duration, error)
}

type ContentReencryptor interface {
	ReencryptContent(context.Context, int) (int64, error)
}

type Service struct {
	Repository        Repository
	Clock             func() time.Time
	ContentRetention  time.Duration
	RetentionPolicies RetentionResolver
	Audit             AuditSink
}

func (s *Service) Record(ctx context.Context, input CreateInput) (Reply, bool, error) {
	if s == nil || s.Repository == nil {
		return Reply{}, false, errors.New("inbound repository is required")
	}
	input.EventID = strings.TrimSpace(input.EventID)
	input.RecipientID = strings.TrimSpace(input.RecipientID)
	input.ContactID = strings.TrimSpace(input.ContactID)
	input.CampaignID = strings.TrimSpace(input.CampaignID)
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.MessageText = strings.TrimSpace(input.MessageText)
	if input.EventID == "" || input.RecipientID == "" || input.ContactID == "" || input.CampaignID == "" || input.SessionID == "" || input.MessageText == "" {
		return Reply{}, false, errors.New("complete inbound reply evidence is required")
	}
	if len(input.MessageText) > 4096 {
		return Reply{}, false, errors.New("inbound reply exceeds maximum length")
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	occurred := input.OccurredAt.UTC()
	if occurred.IsZero() {
		occurred = now
	}
	classification := input.Classification
	if classification == "" {
		classification = ClassificationUnreviewed
	}
	sum := sha256.Sum256([]byte(input.MessageText))
	identifier, err := id.New()
	if err != nil {
		return Reply{}, false, err
	}
	retention := s.ContentRetention
	if s.RetentionPolicies != nil {
		resolved, resolveErr := s.RetentionPolicies.ActiveDuration(ctx)
		if resolveErr != nil {
			return Reply{}, false, resolveErr
		}
		retention = resolved
	}
	if retention <= 0 {
		retention = 90 * 24 * time.Hour
	}
	reply := Reply{ID: identifier, EventID: input.EventID, RecipientID: input.RecipientID, ContactID: input.ContactID, CampaignID: input.CampaignID, SessionID: input.SessionID, ProviderMessageID: strings.TrimSpace(input.ProviderMessageID), MessageText: input.MessageText, MessageFingerprint: hex.EncodeToString(sum[:]), Classification: classification, OccurredAt: occurred, CreatedAt: now, ContentRetainUntil: now.Add(retention), Version: 1}
	return s.Repository.Create(ctx, reply)
}

func (s *Service) Summary(ctx context.Context, campaignID string) (Summary, error) {
	if s == nil || s.Repository == nil {
		return Summary{}, errors.New("inbound repository is required")
	}
	if strings.TrimSpace(campaignID) == "" {
		return Summary{}, errors.New("campaign id is required")
	}
	return s.Repository.Summary(ctx, strings.TrimSpace(campaignID))
}

func (s *Service) Get(ctx context.Context, id string) (Reply, error) {
	if s == nil || s.Repository == nil {
		return Reply{}, errors.New("inbound repository is required")
	}
	return s.Repository.Get(ctx, strings.TrimSpace(id))
}

type AuditRecord struct {
	ActorID, Action, ObjectType, ObjectID, ReasonCode, CorrelationID string
	After                                                            map[string]any
}
type AuditSink interface {
	RecordInboundAudit(context.Context, AuditRecord) error
}

func (s *Service) Reveal(ctx context.Context, id, actorID, correlationID string) (Reply, error) {
	item, err := s.Get(ctx, id)
	if err != nil {
		return Reply{}, err
	}
	if s.Audit == nil {
		return Reply{}, errors.New("audit recorder is required for content reveal")
	}
	err = s.Audit.RecordInboundAudit(ctx, AuditRecord{ActorID: strings.TrimSpace(actorID), Action: "INBOUND_CONTENT_REVEALED", ObjectType: "INBOUND_REPLY", ObjectID: item.ID, After: map[string]any{"fingerprint": item.MessageFingerprint, "retainedUntil": item.ContentRetainUntil, "legalHold": item.LegalHold}, ReasonCode: "AUTHORISED_OPERATIONAL_REVIEW", CorrelationID: strings.TrimSpace(correlationID)})
	if err != nil {
		return Reply{}, err
	}
	return item, nil
}

func (s *Service) RedactExpiredAudited(ctx context.Context, actorID, correlationID string) (int64, error) {
	count, err := s.RedactExpired(ctx)
	if err != nil {
		return 0, err
	}
	if s.Audit == nil {
		return 0, errors.New("audit recorder is required for retention sweep")
	}
	err = s.Audit.RecordInboundAudit(ctx, AuditRecord{ActorID: strings.TrimSpace(actorID), Action: "INBOUND_RETENTION_SWEEP", ObjectType: "INBOUND_REPLY_RETENTION", ObjectID: "expired-content", After: map[string]any{"redactedCount": count}, ReasonCode: "RETENTION_POLICY", CorrelationID: strings.TrimSpace(correlationID)})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Service) RedactExpired(ctx context.Context) (int64, error) {
	if s == nil || s.Repository == nil {
		return 0, errors.New("inbound repository is required")
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	return s.Repository.RedactExpired(ctx, now)
}

func (s *Service) List(ctx context.Context, limit int) ([]Reply, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.Repository.List(ctx, limit)
}
func (s *Service) Review(ctx context.Context, id string, expected int64, classification Classification, escalated bool, reason, actor string) (Reply, error) {
	actor = strings.TrimSpace(actor)
	reason = strings.TrimSpace(reason)
	if actor == "" || expected <= 0 {
		return Reply{}, errors.New("actor and expected version are required")
	}
	switch classification {
	case ClassificationOptOut, ClassificationQuestion, ClassificationComplaint, ClassificationOther:
	default:
		return Reply{}, errors.New("a reviewed classification is required")
	}
	if escalated && reason == "" {
		return Reply{}, errors.New("escalation reason is required")
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	return s.Repository.UpdateReview(ctx, strings.TrimSpace(id), expected, classification, escalated, reason, actor, now)
}

func (s *Service) SetLegalHold(ctx context.Context, id string, expected int64, hold bool, reason, actorID, correlationID string) (Reply, error) {
	if s == nil || s.Repository == nil || s.Audit == nil {
		return Reply{}, errors.New("repository and audit recorder are required")
	}
	reason = strings.TrimSpace(reason)
	actorID = strings.TrimSpace(actorID)
	if expected <= 0 || actorID == "" || reason == "" {
		return Reply{}, errors.New("expected version, actor and reason are required")
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	item, err := s.Repository.SetLegalHold(ctx, strings.TrimSpace(id), expected, hold, reason, actorID, now)
	if err != nil {
		return Reply{}, err
	}
	action := "INBOUND_LEGAL_HOLD_APPLIED"
	code := "LEGAL_HOLD"
	if !hold {
		action = "INBOUND_LEGAL_HOLD_RELEASED"
		code = "LEGAL_HOLD_RELEASE"
	}
	err = s.Audit.RecordInboundAudit(ctx, AuditRecord{ActorID: actorID, Action: action, ObjectType: "INBOUND_REPLY", ObjectID: item.ID, After: map[string]any{"legalHold": item.LegalHold, "reason": reason, "version": item.Version}, ReasonCode: code, CorrelationID: strings.TrimSpace(correlationID)})
	if err != nil {
		return Reply{}, err
	}
	return item, nil
}

func (s *Service) ReencryptContentAudited(ctx context.Context, limit int, actorID, correlationID string) (int64, error) {
	if s == nil || s.Repository == nil || s.Audit == nil {
		return 0, errors.New("repository and audit recorder are required")
	}
	reencryptor, ok := s.Repository.(ContentReencryptor)
	if !ok {
		return 0, errors.New("content re-encryption is not supported")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return 0, errors.New("actor is required")
	}
	count, err := reencryptor.ReencryptContent(ctx, limit)
	if err != nil {
		return 0, err
	}
	err = s.Audit.RecordInboundAudit(ctx, AuditRecord{ActorID: actorID, Action: "INBOUND_CONTENT_REENCRYPTED", ObjectType: "INBOUND_REPLY_CONTENT", ObjectID: "rotation-batch", After: map[string]any{"reencryptedCount": count}, ReasonCode: "KEY_ROTATION", CorrelationID: strings.TrimSpace(correlationID)})
	if err != nil {
		return 0, err
	}
	return count, nil
}
