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
	CommercialApprovalID        string             `json:"commercialApprovalId,omitempty"`
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
	CommercialApprovalID  string `json:"-"`
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
	if input.Action == ActionApproveCommercial && strings.TrimSpace(input.CommercialApprovalID) != "" {
		c.CommercialApprovalID = strings.TrimSpace(input.CommercialApprovalID)
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

type MaterialAmendmentInput struct {
	ActorID                 string             `json:"-"`
	Reason                  string             `json:"reason"`
	ExpectedVersion         int64              `json:"expectedVersion"`
	ChangeSchedule          bool               `json:"changeSchedule"`
	RequestedStartAt        *time.Time         `json:"requestedStartAt"`
	CompletionDeadlineAt    *time.Time         `json:"completionDeadlineAt"`
	ChangeAudience          bool               `json:"changeAudience"`
	AudienceSnapshotID      string             `json:"audienceSnapshotId"`
	AudienceSnapshotHash    string             `json:"-"`
	EligibleAudienceCount   int64              `json:"-"`
	ChangeMessage           bool               `json:"changeMessage"`
	MessageVersionID        string             `json:"messageVersionId"`
	MessageContentHash      string             `json:"-"`
	ChangeTransport         bool               `json:"changeTransport"`
	Transport               TransportSelection `json:"transport"`
	ChangeEntitlement       bool               `json:"changeEntitlement"`
	MaximumUniqueRecipients int64              `json:"maximumUniqueRecipients"`
}

type MaterialChangeEvent struct {
	ID              string    `json:"id"`
	CampaignID      string    `json:"campaignId"`
	Sequence        int64     `json:"sequence"`
	ActorID         string    `json:"actorId"`
	Reason          string    `json:"reason"`
	ChangedFields   []string  `json:"changedFields"`
	PreviousStatus  Status    `json:"previousStatus"`
	NewStatus       Status    `json:"newStatus"`
	PreviousVersion int64     `json:"previousVersion"`
	NewVersion      int64     `json:"newVersion"`
	CreatedAt       time.Time `json:"createdAt"`
}

func (c Campaign) AmendMaterial(input MaterialAmendmentInput, now time.Time) (Campaign, MaterialChangeEvent, error) {
	if strings.TrimSpace(input.ActorID) == "" || strings.TrimSpace(input.Reason) == "" {
		return Campaign{}, MaterialChangeEvent{}, errors.New("actor identity and amendment reason are required")
	}
	if input.ExpectedVersion <= 0 || input.ExpectedVersion != c.Version {
		return Campaign{}, MaterialChangeEvent{}, fmt.Errorf("version conflict: expected %d, current %d", input.ExpectedVersion, c.Version)
	}
	allowed := map[Status]bool{
		StatusAudienceValidated: true, StatusMessageReviewPending: true, StatusMessageApproved: true,
		StatusCommercialApproved: true, StatusFinalApprovalPending: true, StatusScheduled: true,
	}
	if !allowed[c.Status] {
		return Campaign{}, MaterialChangeEvent{}, fmt.Errorf("material amendment is unavailable from status %s", c.Status)
	}
	changed := make([]string, 0, 8)
	previousStatus, previousVersion := c.Status, c.Version
	regress := StatusCommercialApproved
	if input.ChangeSchedule {
		if input.RequestedStartAt == nil || input.CompletionDeadlineAt == nil || !input.CompletionDeadlineAt.After(*input.RequestedStartAt) {
			return Campaign{}, MaterialChangeEvent{}, errors.New("schedule amendment requires a start time and later completion deadline")
		}
		c.RequestedStartAt, c.CompletionDeadlineAt = input.RequestedStartAt, input.CompletionDeadlineAt
		changed = append(changed, "SCHEDULE")
	}
	if input.ChangeTransport {
		if err := input.Transport.Validate(); err != nil {
			return Campaign{}, MaterialChangeEvent{}, err
		}
		c.Transport = input.Transport
		c.SenderPool = input.Transport.SenderPoolID
		changed = append(changed, "TRANSPORT")
	}
	if input.ChangeAudience {
		if strings.TrimSpace(input.AudienceSnapshotID) == "" || strings.TrimSpace(input.AudienceSnapshotHash) == "" || input.EligibleAudienceCount <= 0 {
			return Campaign{}, MaterialChangeEvent{}, errors.New("audience amendment requires an immutable snapshot, hash and positive eligible count")
		}
		if input.EligibleAudienceCount > c.MaximumUniqueRecipients {
			return Campaign{}, MaterialChangeEvent{}, errors.New("eligible audience exceeds authorised maximum")
		}
		c.AudienceSnapshotID, c.AudienceSnapshotHash, c.EligibleAudienceCount = strings.TrimSpace(input.AudienceSnapshotID), strings.TrimSpace(input.AudienceSnapshotHash), input.EligibleAudienceCount
		changed = append(changed, "AUDIENCE")
		regress = StatusMessageApproved
		c.CommercialApprovalID = ""
	}
	if input.ChangeMessage {
		if strings.TrimSpace(input.MessageVersionID) == "" || strings.TrimSpace(input.MessageContentHash) == "" {
			return Campaign{}, MaterialChangeEvent{}, errors.New("message amendment requires an immutable version and content hash")
		}
		c.MessageVersionID, c.MessageContentHash = strings.TrimSpace(input.MessageVersionID), strings.TrimSpace(input.MessageContentHash)
		changed = append(changed, "MESSAGE")
		regress = StatusMessageReviewPending
		c.CommercialApprovalID = ""
	}
	if input.ChangeEntitlement {
		if input.MaximumUniqueRecipients <= 0 || input.MaximumUniqueRecipients < c.EligibleAudienceCount {
			return Campaign{}, MaterialChangeEvent{}, errors.New("maximum recipients must cover the frozen eligible audience")
		}
		c.MaximumUniqueRecipients = input.MaximumUniqueRecipients
		changed = append(changed, "ENTITLEMENT")
		if regress != StatusMessageReviewPending {
			regress = StatusMessageApproved
		}
		c.CommercialApprovalID = ""
	}
	if len(changed) == 0 {
		return Campaign{}, MaterialChangeEvent{}, errors.New("at least one material change is required")
	}
	if regress == StatusCommercialApproved && c.CommercialApprovalID == "" {
		regress = StatusMessageApproved
	}
	c.Status = regress
	c.FinalApprovedBy = ""
	c.PauseReason = ""
	c.Version++
	c.UpdatedAt = now.UTC()
	eventID, err := id.New()
	if err != nil {
		return Campaign{}, MaterialChangeEvent{}, err
	}
	return c, MaterialChangeEvent{ID: eventID, CampaignID: c.ID, ActorID: strings.TrimSpace(input.ActorID), Reason: strings.TrimSpace(input.Reason), ChangedFields: changed, PreviousStatus: previousStatus, NewStatus: c.Status, PreviousVersion: previousVersion, NewVersion: c.Version, CreatedAt: now.UTC()}, nil
}

func HashMessage(body string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(body)))
	return hex.EncodeToString(sum[:])
}
