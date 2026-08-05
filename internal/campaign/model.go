package campaign

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/shared/id"
)

type Status string

const (
	StatusDraft                   Status = "DRAFT"
	StatusConsentReviewPending    Status = "CONSENT_REVIEW_PENDING"
	StatusConsentApproved         Status = "CONSENT_APPROVED"
	StatusAudienceBuilding        Status = "AUDIENCE_BUILDING"
	StatusAudienceValidated       Status = "AUDIENCE_VALIDATED"
	StatusMessageReviewPending    Status = "MESSAGE_REVIEW_PENDING"
	StatusMessageApproved         Status = "MESSAGE_APPROVED"
	StatusCommercialApproved      Status = "COMMERCIAL_APPROVED"
	StatusFinalApprovalPending    Status = "FINAL_APPROVAL_PENDING"
	StatusScheduled               Status = "SCHEDULED"
	StatusDispatching             Status = "DISPATCHING"
	StatusPaused                  Status = "PAUSED"
	StatusCompleted               Status = "COMPLETED"
	StatusCompletedWithExceptions Status = "COMPLETED_WITH_EXCEPTIONS"
	StatusCancelled               Status = "CANCELLED"
)

type Action string

const (
	ActionSubmitConsentReview  Action = "SUBMIT_CONSENT_REVIEW"
	ActionApproveConsent       Action = "APPROVE_CONSENT"
	ActionStartAudienceBuild   Action = "START_AUDIENCE_BUILD"
	ActionValidateAudience     Action = "VALIDATE_AUDIENCE"
	ActionSubmitMessage        Action = "SUBMIT_MESSAGE"
	ActionApproveMessage       Action = "APPROVE_MESSAGE"
	ActionApproveCommercial    Action = "APPROVE_COMMERCIAL"
	ActionRequestFinalApproval Action = "REQUEST_FINAL_APPROVAL"
	ActionApproveFinal         Action = "APPROVE_FINAL"
	ActionStartDispatch        Action = "START_DISPATCH"
	ActionPause                Action = "PAUSE"
	ActionResume               Action = "RESUME"
	ActionComplete             Action = "COMPLETE"
	ActionCancel               Action = "CANCEL"
)

type Campaign struct {
	ID                          string             `json:"id"`
	OrganisationID              string             `json:"organisationId"`
	Name                        string             `json:"name"`
	PurposeID                   string             `json:"purposeId"`
	ConsentReviewID             string             `json:"consentReviewId"`
	Status                      Status             `json:"status"`
	RequestedStartAt            *time.Time         `json:"requestedStartAt,omitempty"`
	CompletionDeadlineAt        *time.Time         `json:"completionDeadlineAt,omitempty"`
	MaximumUniqueRecipients     int64              `json:"maximumUniqueRecipients"`
	MaximumMessagesPerRecipient int                `json:"maximumMessagesPerRecipient"`
	AudienceSnapshotID          string             `json:"audienceSnapshotId,omitempty"`
	AudienceSnapshotHash        string             `json:"audienceSnapshotHash,omitempty"`
	EligibleAudienceCount       int64              `json:"eligibleAudienceCount"`
	MessageVersionID            string             `json:"messageVersionId,omitempty"`
	MessageContentHash          string             `json:"messageContentHash,omitempty"`
	SenderPool                  string             `json:"senderPool,omitempty"`
	Transport                   TransportSelection `json:"transport"`
	CreatedBy                   string             `json:"createdBy"`
	FinalApprovedBy             string             `json:"finalApprovedBy,omitempty"`
	PauseReason                 string             `json:"pauseReason,omitempty"`
	CreatedAt                   time.Time          `json:"createdAt"`
	UpdatedAt                   time.Time          `json:"updatedAt"`
	Version                     int64              `json:"version"`
}

type CreateInput struct {
	OrganisationID              string             `json:"organisationId"`
	Name                        string             `json:"name"`
	PurposeID                   string             `json:"purposeId"`
	ConsentReviewID             string             `json:"consentReviewId"`
	RequestedStartAt            *time.Time         `json:"requestedStartAt"`
	CompletionDeadlineAt        *time.Time         `json:"completionDeadlineAt"`
	MaximumUniqueRecipients     int64              `json:"maximumUniqueRecipients"`
	MaximumMessagesPerRecipient int                `json:"maximumMessagesPerRecipient"`
	SenderPool                  string             `json:"senderPool"`
	Transport                   TransportSelection `json:"transport"`
	CreatedBy                   string             `json:"createdBy"`
}

type TransitionInput struct {
	Action                Action `json:"action"`
	ActorID               string `json:"-"`
	Reason                string `json:"reason"`
	AudienceSnapshotID    string `json:"audienceSnapshotId"`
	AudienceSnapshotHash  string `json:"-"`
	EligibleAudienceCount int64  `json:"-"`
	MessageVersionID      string `json:"messageVersionId"`
	MessageContentHash    string `json:"-"`
	FailedCount           int64  `json:"failedCount"`
	UnknownCount          int64  `json:"unknownCount"`
	ExpectedVersion       int64  `json:"expectedVersion"`
}

func New(input CreateInput, now time.Time) (Campaign, error) {
	if strings.TrimSpace(input.OrganisationID) == "" || strings.TrimSpace(input.Name) == "" ||
		strings.TrimSpace(input.PurposeID) == "" || strings.TrimSpace(input.ConsentReviewID) == "" {
		return Campaign{}, errors.New("organisation, name, purpose and consent review are required")
	}
	if strings.TrimSpace(input.CreatedBy) == "" {
		return Campaign{}, errors.New("creator identity is required")
	}
	if input.MaximumUniqueRecipients <= 0 {
		return Campaign{}, errors.New("maximum unique recipients must be greater than zero")
	}
	if input.MaximumMessagesPerRecipient <= 0 {
		input.MaximumMessagesPerRecipient = 1
	}
	if input.MaximumMessagesPerRecipient != 1 {
		return Campaign{}, errors.New("initial release permits exactly one approved message per campaign recipient")
	}
	if err := input.Transport.Validate(); err != nil {
		return Campaign{}, err
	}
	if input.RequestedStartAt != nil && input.CompletionDeadlineAt != nil && !input.CompletionDeadlineAt.After(*input.RequestedStartAt) {
		return Campaign{}, errors.New("completion deadline must be after the requested start time")
	}
	identifier, err := id.New()
	if err != nil {
		return Campaign{}, err
	}
	return Campaign{
		ID:                          identifier,
		OrganisationID:              strings.TrimSpace(input.OrganisationID),
		Name:                        strings.TrimSpace(input.Name),
		PurposeID:                   strings.TrimSpace(input.PurposeID),
		ConsentReviewID:             strings.TrimSpace(input.ConsentReviewID),
		Status:                      StatusDraft,
		RequestedStartAt:            input.RequestedStartAt,
		CompletionDeadlineAt:        input.CompletionDeadlineAt,
		MaximumUniqueRecipients:     input.MaximumUniqueRecipients,
		MaximumMessagesPerRecipient: input.MaximumMessagesPerRecipient,
		SenderPool:                  strings.TrimSpace(input.SenderPool),
		Transport:                   input.Transport,
		CreatedBy:                   strings.TrimSpace(input.CreatedBy),
		CreatedAt:                   now.UTC(),
		UpdatedAt:                   now.UTC(),
		Version:                     1,
	}, nil
}

func (c Campaign) Transition(input TransitionInput, now time.Time) (Campaign, error) {
	if strings.TrimSpace(input.ActorID) == "" {
		return Campaign{}, errors.New("actor identity is required")
	}
	if input.ExpectedVersion <= 0 {
		return Campaign{}, errors.New("expected version is required")
	}
	if input.ExpectedVersion != c.Version {
		return Campaign{}, fmt.Errorf("version conflict: expected %d, current %d", input.ExpectedVersion, c.Version)
	}
	next, err := c.nextStatus(input)
	if err != nil {
		return Campaign{}, err
	}
	if input.Action == ActionApproveFinal && input.ActorID == c.CreatedBy {
		return Campaign{}, errors.New("the campaign creator cannot provide final approval")
	}
	if input.Action == ActionValidateAudience {
		if strings.TrimSpace(input.AudienceSnapshotID) == "" || strings.TrimSpace(input.AudienceSnapshotHash) == "" {
			return Campaign{}, errors.New("audience validation requires a frozen snapshot and hash")
		}
		if input.EligibleAudienceCount <= 0 || input.EligibleAudienceCount > c.MaximumUniqueRecipients {
			return Campaign{}, errors.New("eligible audience must be positive and within the authorised maximum")
		}
		c.AudienceSnapshotID = input.AudienceSnapshotID
		c.AudienceSnapshotHash = input.AudienceSnapshotHash
		c.EligibleAudienceCount = input.EligibleAudienceCount
	}
	if input.Action == ActionSubmitMessage {
		if strings.TrimSpace(input.MessageVersionID) == "" || strings.TrimSpace(input.MessageContentHash) == "" {
			return Campaign{}, errors.New("message submission requires an approved version and content hash")
		}
		c.MessageVersionID = input.MessageVersionID
		c.MessageContentHash = input.MessageContentHash
	}
	if input.Action == ActionApproveFinal {
		if c.RequestedStartAt == nil || c.CompletionDeadlineAt == nil {
			return Campaign{}, errors.New("final approval requires a start time and completion deadline")
		}
		if c.AudienceSnapshotID == "" || c.MessageVersionID == "" {
			return Campaign{}, errors.New("final approval requires audience and message")
		}
		if err := c.Transport.Validate(); err != nil {
			return Campaign{}, fmt.Errorf("final approval requires valid immutable transport selection: %w", err)
		}
		c.FinalApprovedBy = input.ActorID
	}
	if input.Action == ActionPause {
		if strings.TrimSpace(input.Reason) == "" {
			return Campaign{}, errors.New("pause requires an operational reason")
		}
		c.PauseReason = strings.TrimSpace(input.Reason)
	}
	if input.Action == ActionResume {
		c.PauseReason = ""
	}
	c.Status = next
	c.Version++
	c.UpdatedAt = now.UTC()
	return c, nil
}

func (c Campaign) nextStatus(input TransitionInput) (Status, error) {
	expected := map[Action]struct {
		from []Status
		to   Status
	}{
		ActionSubmitConsentReview:  {[]Status{StatusDraft}, StatusConsentReviewPending},
		ActionApproveConsent:       {[]Status{StatusConsentReviewPending}, StatusConsentApproved},
		ActionStartAudienceBuild:   {[]Status{StatusConsentApproved}, StatusAudienceBuilding},
		ActionValidateAudience:     {[]Status{StatusAudienceBuilding}, StatusAudienceValidated},
		ActionSubmitMessage:        {[]Status{StatusAudienceValidated}, StatusMessageReviewPending},
		ActionApproveMessage:       {[]Status{StatusMessageReviewPending}, StatusMessageApproved},
		ActionApproveCommercial:    {[]Status{StatusMessageApproved}, StatusCommercialApproved},
		ActionRequestFinalApproval: {[]Status{StatusCommercialApproved}, StatusFinalApprovalPending},
		ActionApproveFinal:         {[]Status{StatusFinalApprovalPending}, StatusScheduled},
		ActionStartDispatch:        {[]Status{StatusScheduled}, StatusDispatching},
		ActionPause:                {[]Status{StatusScheduled, StatusDispatching}, StatusPaused},
		ActionResume:               {[]Status{StatusPaused}, StatusDispatching},
		ActionCancel: {[]Status{
			StatusDraft, StatusConsentReviewPending, StatusConsentApproved, StatusAudienceBuilding,
			StatusAudienceValidated, StatusMessageReviewPending, StatusMessageApproved,
			StatusCommercialApproved, StatusFinalApprovalPending, StatusScheduled, StatusPaused,
		}, StatusCancelled},
	}
	if input.Action == ActionComplete {
		if c.Status != StatusDispatching && c.Status != StatusPaused {
			return "", fmt.Errorf("action %s is unavailable from status %s", input.Action, c.Status)
		}
		if input.FailedCount > 0 || input.UnknownCount > 0 {
			return StatusCompletedWithExceptions, nil
		}
		return StatusCompleted, nil
	}
	rule, ok := expected[input.Action]
	if !ok {
		return "", fmt.Errorf("unknown campaign action %s", input.Action)
	}
	for _, status := range rule.from {
		if c.Status == status {
			return rule.to, nil
		}
	}
	return "", fmt.Errorf("action %s is unavailable from status %s", input.Action, c.Status)
}

func HashMessage(body string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(body)))
	return hex.EncodeToString(sum[:])
}
