package testmessage

import (
	"context"
	"errors"
	"strings"
	"time"

	"campaign-platform/internal/message"
	"campaign-platform/internal/shared/crypto"
	"campaign-platform/internal/shared/id"
)

const JobType = "TEST_MESSAGE_SEND"

type RecipientStatus string

const (
	RecipientDraft    RecipientStatus = "DRAFT"
	RecipientPending  RecipientStatus = "PENDING_APPROVAL"
	RecipientActive   RecipientStatus = "ACTIVE"
	RecipientRejected RecipientStatus = "REJECTED"
	RecipientRevoked  RecipientStatus = "REVOKED"
)

type SendStatus string

const (
	SendPending    SendStatus = "PENDING"
	SendProcessing SendStatus = "PROCESSING"
	SendAccepted   SendStatus = "ACCEPTED"
	SendFailed     SendStatus = "FAILED"
	SendUnknown    SendStatus = "UNKNOWN"
)

type Recipient struct {
	ID               string          `json:"id"`
	Label            string          `json:"label"`
	MSISDNEncrypted  []byte          `json:"-"`
	MSISDNLookupHash []byte          `json:"-"`
	MaskedMSISDN     string          `json:"maskedMsisdn"`
	Status           RecipientStatus `json:"status"`
	CreatedBy        string          `json:"createdBy"`
	SubmittedBy      string          `json:"submittedBy,omitempty"`
	ApprovedBy       string          `json:"approvedBy,omitempty"`
	Reason           string          `json:"reason"`
	Version          int64           `json:"version"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

type Send struct {
	ID                 string            `json:"id"`
	CampaignID         string            `json:"campaignId"`
	MessageVersionID   string            `json:"messageVersionId"`
	MessageContentHash string            `json:"messageContentHash"`
	TestRecipientID    string            `json:"testRecipientId"`
	GatewayPoolID      string            `json:"gatewayPoolId"`
	SenderPoolID       string            `json:"senderPoolId,omitempty"`
	Provider           string            `json:"provider"`
	Engine             string            `json:"engine"`
	SenderSessionID    string            `json:"senderSessionId"`
	VariableValues     map[string]string `json:"variableValues"`
	Status             SendStatus        `json:"status"`
	ProviderMessageID  string            `json:"providerMessageId,omitempty"`
	FailureCode        string            `json:"failureCode,omitempty"`
	CreatedBy          string            `json:"createdBy"`
	Reason             string            `json:"reason"`
	IdempotencyKey     string            `json:"idempotencyKey"`
	AttemptCount       int               `json:"attemptCount"`
	LeaseOwner         string            `json:"leaseOwner,omitempty"`
	LeaseVersion       int64             `json:"leaseVersion"`
	LeaseExpiresAt     *time.Time        `json:"leaseExpiresAt,omitempty"`
	CreatedAt          time.Time         `json:"createdAt"`
	UpdatedAt          time.Time         `json:"updatedAt"`
	CompletedAt        *time.Time        `json:"completedAt,omitempty"`
}

var (
	ErrInvalid            = errors.New("test-message request is invalid")
	ErrNotFound           = errors.New("test-message record not found")
	ErrConflict           = errors.New("test-message record conflict")
	ErrRecipientNotActive = errors.New("test recipient is not approved and active")
	ErrLeaseConflict      = errors.New("test-message lease conflict")
)

type Repository interface {
	CreateRecipient(context.Context, Recipient) (Recipient, error)
	GetRecipient(context.Context, string) (Recipient, error)
	ListRecipients(context.Context) ([]Recipient, error)
	CompareAndSwapRecipient(context.Context, Recipient, int64) (Recipient, error)
	CreateSend(context.Context, Send) (Send, error)
	GetSend(context.Context, string) (Send, error)
	ListSends(context.Context, string) ([]Send, error)
	ClaimSends(context.Context, string, time.Time, time.Duration, int) ([]Send, error)
	CompleteSend(context.Context, Send, SendStatus, string, string, time.Time) error
}

type RouteValidator interface {
	ValidateTestRoute(context.Context, string, string, string, string, string) error
}

type RouteValidatorFunc func(context.Context, string, string, string, string, string) error

func (f RouteValidatorFunc) ValidateTestRoute(ctx context.Context, gatewayPoolID, senderPoolID, provider, engine, sessionID string) error {
	return f(ctx, gatewayPoolID, senderPoolID, provider, engine, sessionID)
}

type Service struct {
	Repository Repository
	Protector  *crypto.MSISDNProtector
	Messages   *message.Service
	Routes     RouteValidator
	Clock      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) CreateRecipient(ctx context.Context, label, e164, actor, reason string) (Recipient, error) {
	label = strings.TrimSpace(label)
	e164 = strings.TrimSpace(e164)
	actor = strings.TrimSpace(actor)
	reason = strings.TrimSpace(reason)
	if s == nil || s.Repository == nil || s.Protector == nil || label == "" || actor == "" || len(reason) < 8 || len(e164) < 8 || e164[0] != '+' {
		return Recipient{}, ErrInvalid
	}
	cipher, err := s.Protector.Encrypt(e164)
	if err != nil {
		return Recipient{}, err
	}
	ident, err := id.New()
	if err != nil {
		return Recipient{}, err
	}
	now := s.now()
	r := Recipient{ID: ident, Label: label, MSISDNEncrypted: cipher, MSISDNLookupHash: s.Protector.LookupHMAC(e164), MaskedMSISDN: crypto.Mask(e164), Status: RecipientDraft, CreatedBy: actor, Reason: reason, Version: 1, CreatedAt: now, UpdatedAt: now}
	return s.Repository.CreateRecipient(ctx, r)
}
func (s *Service) SubmitRecipient(ctx context.Context, idv, actor, reason string, expected int64) (Recipient, error) {
	return s.changeRecipient(ctx, idv, actor, reason, expected, RecipientPending, false)
}
func (s *Service) DecideRecipient(ctx context.Context, idv, actor, reason string, expected int64, approve bool) (Recipient, error) {
	target := RecipientRejected
	if approve {
		target = RecipientActive
	}
	return s.changeRecipient(ctx, idv, actor, reason, expected, target, true)
}
func (s *Service) RevokeRecipient(ctx context.Context, idv, actor, reason string, expected int64) (Recipient, error) {
	return s.changeRecipient(ctx, idv, actor, reason, expected, RecipientRevoked, true)
}
func (s *Service) changeRecipient(ctx context.Context, idv, actor, reason string, expected int64, target RecipientStatus, checker bool) (Recipient, error) {
	r, err := s.Repository.GetRecipient(ctx, strings.TrimSpace(idv))
	if err != nil {
		return Recipient{}, err
	}
	actor = strings.TrimSpace(actor)
	reason = strings.TrimSpace(reason)
	if actor == "" || len(reason) < 8 || r.Version != expected {
		return Recipient{}, ErrConflict
	}
	if target == RecipientPending && r.Status != RecipientDraft {
		return Recipient{}, ErrInvalid
	}
	if (target == RecipientActive || target == RecipientRejected) && r.Status != RecipientPending {
		return Recipient{}, ErrInvalid
	}
	if target == RecipientRevoked && r.Status != RecipientActive {
		return Recipient{}, ErrInvalid
	}
	if checker && actor == r.CreatedBy {
		return Recipient{}, ErrInvalid
	}
	if target == RecipientPending {
		r.SubmittedBy = actor
	} else {
		r.ApprovedBy = actor
	}
	r.Status = target
	r.Reason = reason
	r.UpdatedAt = s.now()
	return s.Repository.CompareAndSwapRecipient(ctx, r, expected)
}
func (s *Service) Schedule(ctx context.Context, campaignID, messageVersionID, recipientID, gatewayPoolID, senderPoolID, provider, engine, sessionID, actor, reason, idempotency string, values map[string]string) (Send, error) {
	if s == nil || s.Repository == nil || s.Messages == nil {
		return Send{}, ErrInvalid
	}
	r, err := s.Repository.GetRecipient(ctx, recipientID)
	if err != nil {
		return Send{}, err
	}
	if r.Status != RecipientActive {
		return Send{}, ErrRecipientNotActive
	}
	v, err := s.Messages.Get(ctx, messageVersionID)
	if err != nil {
		return Send{}, err
	}
	if strings.TrimSpace(campaignID) == "" {
		campaignID = v.CampaignID
	}
	if v.CampaignID != campaignID {
		return Send{}, message.ErrCampaignMismatch
	}
	if _, err = s.Messages.Preview(ctx, messageVersionID, message.RenderInput{Values: values, Mode: message.RenderPreview}); err != nil {
		return Send{}, err
	}
	provider = strings.ToUpper(strings.TrimSpace(provider))
	engine = strings.ToUpper(strings.TrimSpace(engine))
	if strings.TrimSpace(gatewayPoolID) == "" || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(actor) == "" || provider != "OPENWA" || (engine != "WHATSAPP_WEB_JS" && engine != "BAILEYS") || len(strings.TrimSpace(reason)) < 8 || len(strings.TrimSpace(idempotency)) < 16 {
		return Send{}, ErrInvalid
	}
	if s.Routes == nil {
		return Send{}, ErrInvalid
	}
	if err := s.Routes.ValidateTestRoute(ctx, strings.TrimSpace(gatewayPoolID), strings.TrimSpace(senderPoolID), provider, engine, strings.TrimSpace(sessionID)); err != nil {
		return Send{}, err
	}
	ident, err := id.New()
	if err != nil {
		return Send{}, err
	}
	now := s.now()
	send := Send{ID: ident, CampaignID: campaignID, MessageVersionID: messageVersionID, MessageContentHash: v.ContentHash, TestRecipientID: recipientID, GatewayPoolID: gatewayPoolID, SenderPoolID: strings.TrimSpace(senderPoolID), Provider: provider, Engine: engine, SenderSessionID: sessionID, VariableValues: values, Status: SendPending, CreatedBy: actor, Reason: strings.TrimSpace(reason), IdempotencyKey: strings.TrimSpace(idempotency), LeaseVersion: 0, CreatedAt: now, UpdatedAt: now}
	return s.Repository.CreateSend(ctx, send)
}
func (s *Service) ListRecipients(ctx context.Context) ([]Recipient, error) {
	return s.Repository.ListRecipients(ctx)
}
func (s *Service) GetSend(ctx context.Context, idv string) (Send, error) {
	return s.Repository.GetSend(ctx, idv)
}
func (s *Service) ListSends(ctx context.Context, campaignID string) ([]Send, error) {
	return s.Repository.ListSends(ctx, campaignID)
}
