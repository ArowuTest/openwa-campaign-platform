package organisation

import (
	"errors"
	"net/mail"
	"strings"
	"time"

	"campaign-platform/internal/shared/id"
)

type Status string

const (
	StatusActive      Status = "ACTIVE"
	StatusSuspended   Status = "SUSPENDED"
	StatusUnderReview Status = "UNDER_REVIEW"
	StatusClosed      Status = "CLOSED"
)

func (s Status) Valid() bool {
	switch s {
	case StatusActive, StatusSuspended, StatusUnderReview, StatusClosed:
		return true
	}
	return false
}

type Organisation struct {
	ID                  string    `json:"id"`
	LegalName           string    `json:"legalName"`
	TradingName         string    `json:"tradingName,omitempty"`
	CountryISO2         string    `json:"countryISO2,omitempty"`
	Status              Status    `json:"status"`
	PrimaryContactName  string    `json:"primaryContactName,omitempty"`
	PrimaryContactEmail string    `json:"primaryContactEmail,omitempty"`
	InternalNotes       string    `json:"internalNotes,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
	Version             int64     `json:"version"`
}

type Event struct {
	ID             string    `json:"id"`
	OrganisationID string    `json:"organisationId"`
	Version        int64     `json:"version"`
	EventType      string    `json:"eventType"`
	ActorID        string    `json:"actorId"`
	Reason         string    `json:"reason,omitempty"`
	BeforeStatus   Status    `json:"beforeStatus,omitempty"`
	AfterStatus    Status    `json:"afterStatus,omitempty"`
	OccurredAt     time.Time `json:"occurredAt"`
}

type CreateInput struct {
	LegalName           string `json:"legalName"`
	TradingName         string `json:"tradingName"`
	CountryISO2         string `json:"countryISO2"`
	PrimaryContactName  string `json:"primaryContactName"`
	PrimaryContactEmail string `json:"primaryContactEmail"`
	InternalNotes       string `json:"internalNotes"`
}

type UpdateInput struct {
	ExpectedVersion     int64  `json:"expectedVersion"`
	LegalName           string `json:"legalName"`
	TradingName         string `json:"tradingName"`
	CountryISO2         string `json:"countryISO2"`
	PrimaryContactName  string `json:"primaryContactName"`
	PrimaryContactEmail string `json:"primaryContactEmail"`
	InternalNotes       string `json:"internalNotes"`
	ActorID             string `json:"-"`
	Reason              string `json:"reason"`
}

type StatusInput struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Status          Status `json:"status"`
	ActorID         string `json:"-"`
	Reason          string `json:"reason"`
}

func New(input CreateInput, now time.Time) (Organisation, error) {
	input = cleanCreate(input)
	if err := validateFields(input.LegalName, input.CountryISO2, input.PrimaryContactEmail); err != nil {
		return Organisation{}, err
	}
	identifier, err := id.New()
	if err != nil {
		return Organisation{}, err
	}
	return Organisation{ID: identifier, LegalName: input.LegalName, TradingName: input.TradingName, CountryISO2: input.CountryISO2,
		Status: StatusUnderReview, PrimaryContactName: input.PrimaryContactName, PrimaryContactEmail: input.PrimaryContactEmail,
		InternalNotes: input.InternalNotes, CreatedAt: now.UTC(), UpdatedAt: now.UTC(), Version: 1}, nil
}

func ApplyUpdate(current Organisation, input UpdateInput, now time.Time) (Organisation, error) {
	if current.Status == StatusClosed {
		return Organisation{}, ErrClosed
	}
	if input.ExpectedVersion <= 0 {
		return Organisation{}, errors.New("expected version is required")
	}
	cleaned := cleanCreate(CreateInput{input.LegalName, input.TradingName, input.CountryISO2, input.PrimaryContactName, input.PrimaryContactEmail, input.InternalNotes})
	if err := validateFields(cleaned.LegalName, cleaned.CountryISO2, cleaned.PrimaryContactEmail); err != nil {
		return Organisation{}, err
	}
	if strings.TrimSpace(input.ActorID) == "" {
		return Organisation{}, errors.New("actor ID is required")
	}
	current.LegalName = cleaned.LegalName
	current.TradingName = cleaned.TradingName
	current.CountryISO2 = cleaned.CountryISO2
	current.PrimaryContactName = cleaned.PrimaryContactName
	current.PrimaryContactEmail = cleaned.PrimaryContactEmail
	current.InternalNotes = cleaned.InternalNotes
	current.UpdatedAt = now.UTC()
	current.Version++
	return current, nil
}

func ApplyStatus(current Organisation, input StatusInput, now time.Time) (Organisation, error) {
	if input.ExpectedVersion <= 0 {
		return Organisation{}, errors.New("expected version is required")
	}
	if !input.Status.Valid() {
		return Organisation{}, errors.New("invalid organisation status")
	}
	if strings.TrimSpace(input.ActorID) == "" {
		return Organisation{}, errors.New("actor ID is required")
	}
	if strings.TrimSpace(input.Reason) == "" {
		return Organisation{}, errors.New("reason is required")
	}
	if current.Status == StatusClosed {
		return Organisation{}, ErrClosed
	}
	if current.Status == input.Status {
		return Organisation{}, errors.New("organisation already has requested status")
	}
	current.Status = input.Status
	current.UpdatedAt = now.UTC()
	current.Version++
	return current, nil
}

func NewEvent(orgID string, version int64, typ, actor, reason string, before, after Status, now time.Time) (Event, error) {
	eid, err := id.New()
	if err != nil {
		return Event{}, err
	}
	return Event{ID: eid, OrganisationID: orgID, Version: version, EventType: typ, ActorID: strings.TrimSpace(actor), Reason: strings.TrimSpace(reason), BeforeStatus: before, AfterStatus: after, OccurredAt: now.UTC()}, nil
}

func cleanCreate(v CreateInput) CreateInput {
	v.LegalName = strings.TrimSpace(v.LegalName)
	v.TradingName = strings.TrimSpace(v.TradingName)
	v.CountryISO2 = strings.ToUpper(strings.TrimSpace(v.CountryISO2))
	v.PrimaryContactName = strings.TrimSpace(v.PrimaryContactName)
	v.PrimaryContactEmail = strings.ToLower(strings.TrimSpace(v.PrimaryContactEmail))
	v.InternalNotes = strings.TrimSpace(v.InternalNotes)
	return v
}
func validateFields(name, country, email string) error {
	if name == "" {
		return errors.New("legal name is required")
	}
	if country != "" && len(country) != 2 {
		return errors.New("country ISO2 must contain two letters")
	}
	if email != "" {
		if _, err := mail.ParseAddress(email); err != nil {
			return errors.New("primary contact email is invalid")
		}
	}
	return nil
}
