package campaign

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"campaign-platform/internal/consent"
	"campaign-platform/internal/organisation"
	"campaign-platform/internal/sender"
	"campaign-platform/internal/shared/id"
)

var (
	ErrDraftNotEditable           = errors.New("campaign draft is not editable")
	ErrDraftReferencesLocked      = errors.New("campaign draft references are locked")
	ErrDraftGovernanceUnavailable = errors.New("campaign draft governance is unavailable")
	ErrDraftInvalid               = errors.New("campaign draft is invalid")
)

type DraftSaveInput struct {
	ExpectedVersion             int64
	ActorID                     string `json:"-"`
	Reason                      string
	Name                        string
	OrganisationID              string
	PurposeID                   string
	ConsentReviewID             string
	RequestedStartAt            *time.Time
	CompletionDeadlineAt        *time.Time
	Timezone                    string
	QuietHoursStart             string
	QuietHoursEnd               string
	MaximumUniqueRecipients     int64
	MaximumMessagesPerRecipient int
	Transport                   TransportSelection
}
type DraftFieldError struct{ Field string }

func (e *DraftFieldError) Error() string { return "campaign draft field is invalid: " + e.Field }
func (e *DraftFieldError) Unwrap() error { return ErrDraftInvalid }
func draftInvalid(field string) error    { return &DraftFieldError{Field: field} }

var draftUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var draftTime = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

type draftPurposeGetter interface {
	Get(context.Context, string) (consent.Purpose, error)
}
type draftReviewGetter interface {
	Get(context.Context, string) (consent.Review, error)
}
type draftPoolGetter interface {
	GetPool(context.Context, string) (sender.Pool, error)
}

func (s *Service) WithConsentPurposeReader(r draftPurposeGetter) *Service {
	s.draftPurposes = r
	return s
}
func (s *Service) WithConsentReviewReader(r draftReviewGetter) *Service { s.draftReviews = r; return s }
func (s *Service) WithSenderPoolReader(r draftPoolGetter) *Service      { s.draftPools = r; return s }

func draftEditable(c Campaign) bool {
	return c.Status == StatusDraft && c.AudienceSnapshotID == "" && c.AudienceSnapshotHash == "" && c.EligibleAudienceCount == 0 &&
		c.MessageVersionID == "" && c.MessageContentHash == "" && c.FinalApprovedBy == "" && c.CommercialApprovalID == ""
}
func utcDraftTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	v := value.UTC()
	return &v
}
func sameDraftTime(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}
func draftEditableTransport(t TransportSelection) TransportSelection {
	t.ProviderDefinitionID = ""
	t.ProviderDefinitionVersion = 0
	t.GatewayPoolVersion = 0
	t.RequiredCapabilities = append([]string{}, t.RequiredCapabilities...)
	sort.Strings(t.RequiredCapabilities)
	return t
}
func sameDraftTransport(a, b TransportSelection) bool {
	return reflect.DeepEqual(draftEditableTransport(a), draftEditableTransport(b))
}
func draftChangedFields(a, b Campaign) []string {
	fields := []string{}
	if a.Name != b.Name {
		fields = append(fields, "NAME")
	}
	if !sameDraftTime(a.RequestedStartAt, b.RequestedStartAt) || !sameDraftTime(a.CompletionDeadlineAt, b.CompletionDeadlineAt) || a.Timezone != b.Timezone || a.QuietHoursStart != b.QuietHoursStart || a.QuietHoursEnd != b.QuietHoursEnd {
		fields = append(fields, "SCHEDULE")
	}
	if !sameDraftTransport(a.Transport, b.Transport) {
		fields = append(fields, "TRANSPORT")
	}
	if a.MaximumUniqueRecipients != b.MaximumUniqueRecipients || a.MaximumMessagesPerRecipient != b.MaximumMessagesPerRecipient {
		fields = append(fields, "ENTITLEMENT")
	}
	return fields
}
func validateDraftInput(input DraftSaveInput) error {
	if input.ExpectedVersion < 1 {
		return draftInvalid("EXPECTED_VERSION")
	}
	if strings.TrimSpace(input.ActorID) == "" {
		return draftInvalid("ACTOR")
	}
	reason := utf8.RuneCountInString(strings.TrimSpace(input.Reason))
	if reason < 8 || reason > 1000 {
		return draftInvalid("REASON")
	}
	name := utf8.RuneCountInString(strings.TrimSpace(input.Name))
	if name < 1 || name > 250 {
		return draftInvalid("NAME")
	}
	for _, value := range []string{input.OrganisationID, input.PurposeID, input.ConsentReviewID} {
		if !draftUUID.MatchString(value) {
			return draftInvalid("REFERENCES")
		}
	}
	if input.MaximumUniqueRecipients < 1 || input.MaximumUniqueRecipients > 9007199254740991 {
		return draftInvalid("MAXIMUM_UNIQUE_RECIPIENTS")
	}
	if input.MaximumMessagesPerRecipient != 1 {
		return draftInvalid("MAXIMUM_MESSAGES_PER_RECIPIENT")
	}
	if strings.TrimSpace(input.Timezone) == "" || strings.TrimSpace(input.Timezone) == "Local" {
		return draftInvalid("TIMEZONE")
	}
	if _, _, _, err := validateDispatchWindow(input.Timezone, input.QuietHoursStart, input.QuietHoursEnd); err != nil {
		return draftInvalid("DISPATCH_WINDOW")
	}
	for _, q := range []string{strings.TrimSpace(input.QuietHoursStart), strings.TrimSpace(input.QuietHoursEnd)} {
		if q != "" && !draftTime.MatchString(q) {
			return draftInvalid("QUIET_HOURS")
		}
	}
	if input.RequestedStartAt != nil && input.CompletionDeadlineAt != nil && !input.CompletionDeadlineAt.After(*input.RequestedStartAt) {
		return draftInvalid("SCHEDULE")
	}
	t := input.Transport
	if t.Channel != "WHATSAPP" || t.Provider != ProviderOpenWA || t.RoutingMode != RoutingSenderPool || t.FallbackMode != FallbackNone || t.SessionID != "" || t.MetaSenderID != "" {
		return draftInvalid("TRANSPORT")
	}
	if !draftUUID.MatchString(t.GatewayPoolID) || !draftUUID.MatchString(t.SenderPoolID) {
		return draftInvalid("TRANSPORT_IDS")
	}
	for _, v := range []string{t.AdapterVersion, t.RoutingPolicyVersion, t.CapacityEvidenceVersion} {
		if strings.TrimSpace(v) == "" || utf8.RuneCountInString(v) > 100 {
			return draftInvalid("TRANSPORT_VERSIONS")
		}
	}
	if err := t.Validate(); err != nil {
		return draftInvalid("TRANSPORT")
	}
	allowed := map[string]bool{"SEND_TEXT": true, "SEND_TEMPLATE": true, "SEND_IMAGE": true, "SEND_VIDEO": true, "SEND_DOCUMENT": true, "DELIVERY_EVENTS": true, "READ_EVENTS": true, "INBOUND_MESSAGES": true, "PAIRING_QR": true, "PAIRING_CODE": true}
	seen := map[string]bool{}
	for _, v := range t.RequiredCapabilities {
		if !allowed[v] || seen[v] {
			return draftInvalid("REQUIRED_CAPABILITIES")
		}
		seen[v] = true
	}
	return nil
}
func (c Campaign) SaveDraft(input DraftSaveInput, now time.Time) (Campaign, MaterialChangeEvent, error) {
	if input.ExpectedVersion != c.Version {
		return Campaign{}, MaterialChangeEvent{}, ErrConflict
	}
	if !draftEditable(c) {
		return Campaign{}, MaterialChangeEvent{}, ErrDraftNotEditable
	}
	if input.OrganisationID != c.OrganisationID || input.PurposeID != c.PurposeID || input.ConsentReviewID != c.ConsentReviewID {
		return Campaign{}, MaterialChangeEvent{}, ErrDraftReferencesLocked
	}
	if err := validateDraftInput(input); err != nil {
		return Campaign{}, MaterialChangeEvent{}, err
	}
	original := c
	c.Name = strings.TrimSpace(input.Name)
	c.MaximumUniqueRecipients = input.MaximumUniqueRecipients
	c.MaximumMessagesPerRecipient = input.MaximumMessagesPerRecipient
	c.RequestedStartAt = utcDraftTime(input.RequestedStartAt)
	c.CompletionDeadlineAt = utcDraftTime(input.CompletionDeadlineAt)
	c.Timezone, c.QuietHoursStart, c.QuietHoursEnd, _ = validateDispatchWindow(input.Timezone, input.QuietHoursStart, input.QuietHoursEnd)
	if !sameDraftTransport(original.Transport, input.Transport) {
		c.Transport = input.Transport
		c.Transport.RequiredCapabilities = append([]string{}, input.Transport.RequiredCapabilities...)
		sort.Strings(c.Transport.RequiredCapabilities)
		c.SenderPool = input.Transport.SenderPoolID
	}
	fields := draftChangedFields(original, c)
	if len(fields) == 0 {
		return original, MaterialChangeEvent{}, nil
	}
	c.Version++
	c.UpdatedAt = now.UTC()
	eventID, err := id.New()
	if err != nil {
		return Campaign{}, MaterialChangeEvent{}, err
	}
	return c, MaterialChangeEvent{ID: eventID, CampaignID: c.ID, ActorID: strings.TrimSpace(input.ActorID), Reason: strings.TrimSpace(input.Reason), ChangedFields: fields, PreviousStatus: StatusDraft, NewStatus: StatusDraft, PreviousVersion: original.Version, NewVersion: c.Version, CreatedAt: now.UTC()}, nil
}

// GuardDraftSave must run under the authoritative repository row lock, including
// for a no-op. It protects frozen references and identity independently of service validation.
func GuardDraftSave(current, next Campaign, event MaterialChangeEvent, expected int64) error {
	if current.Version != expected {
		return ErrConflict
	}
	if !draftEditable(current) || !draftEditable(next) {
		return ErrDraftNotEditable
	}
	if current.OrganisationID != next.OrganisationID || current.PurposeID != next.PurposeID || current.ConsentReviewID != next.ConsentReviewID {
		return ErrDraftReferencesLocked
	}
	if current.ID != next.ID || current.CreatedBy != next.CreatedBy || !current.CreatedAt.Equal(next.CreatedAt) || current.PauseReason != next.PauseReason {
		return draftInvalid("IDENTITY")
	}
	if sameDraftTransport(current.Transport, next.Transport) && (!reflect.DeepEqual(current.Transport, next.Transport) || current.SenderPool != next.SenderPool) {
		return draftInvalid("TRANSPORT_BINDING")
	}
	fields := draftChangedFields(current, next)
	if len(fields) == 0 {
		if next.Version != expected {
			return ErrConflict
		}
		if event.ID != "" || !current.UpdatedAt.Equal(next.UpdatedAt) || !reflect.DeepEqual(current.Transport, next.Transport) || current.SenderPool != next.SenderPool {
			return draftInvalid("NO_OP")
		}
		return nil
	}
	if next.Version != expected+1 {
		return ErrConflict
	}
	if err := validateDraftInput(DraftSaveInput{ExpectedVersion: expected, ActorID: event.ActorID, Reason: event.Reason, Name: next.Name, OrganisationID: next.OrganisationID, PurposeID: next.PurposeID, ConsentReviewID: next.ConsentReviewID, RequestedStartAt: next.RequestedStartAt, CompletionDeadlineAt: next.CompletionDeadlineAt, Timezone: next.Timezone, QuietHoursStart: next.QuietHoursStart, QuietHoursEnd: next.QuietHoursEnd, MaximumUniqueRecipients: next.MaximumUniqueRecipients, MaximumMessagesPerRecipient: next.MaximumMessagesPerRecipient, Transport: next.Transport}); err != nil {
		return err
	}
	if event.ID == "" || event.CampaignID != current.ID || strings.TrimSpace(event.ActorID) == "" || utf8.RuneCountInString(strings.TrimSpace(event.Reason)) < 8 || event.PreviousStatus != StatusDraft || event.NewStatus != StatusDraft || event.PreviousVersion != expected || event.NewVersion != next.Version || !reflect.DeepEqual(fields, event.ChangedFields) || !event.CreatedAt.Equal(next.UpdatedAt) {
		return draftInvalid("EVENT")
	}
	if !sameDraftTransport(current.Transport, next.Transport) && next.SenderPool != next.Transport.SenderPoolID {
		return draftInvalid("TRANSPORT")
	}
	return nil
}
func (s *Service) SaveDraft(ctx context.Context, identifier string, input DraftSaveInput) (Campaign, error) {
	current, err := s.repository.Get(ctx, identifier)
	if err != nil {
		return Campaign{}, err
	}
	// Validate the complete request before touching current governance sources.
	if _, _, err = current.SaveDraft(input, s.clock()); err != nil {
		return Campaign{}, err
	}
	if s.organisations == nil || s.policies == nil || s.draftPurposes == nil || s.draftReviews == nil || s.draftPools == nil || s.providerCapabilities == nil || s.gatewayPools == nil {
		return Campaign{}, ErrDraftGovernanceUnavailable
	}
	org, err := s.organisations.Get(ctx, current.OrganisationID)
	if err != nil {
		return Campaign{}, draftInvalid("ORGANISATION")
	}
	if org.ID != current.OrganisationID || org.Status != organisation.StatusActive {
		return Campaign{}, draftInvalid("ORGANISATION")
	}
	purpose, err := s.draftPurposes.Get(ctx, current.PurposeID)
	if err != nil {
		return Campaign{}, draftInvalid("PURPOSE")
	}
	if purpose.ID != current.PurposeID || !purpose.Active || (purpose.OrganisationID != "" && purpose.OrganisationID != current.OrganisationID) || purpose.Channel != "WHATSAPP" || purpose.ConsentReviewID != current.ConsentReviewID {
		return Campaign{}, draftInvalid("PURPOSE")
	}
	if err := s.policies.ValidatePurpose(ctx, current.OrganisationID, current.PurposeID); err != nil {
		return Campaign{}, draftInvalid("ORGANISATION_POLICY")
	}
	review, err := s.draftReviews.Get(ctx, current.ConsentReviewID)
	if err != nil {
		return Campaign{}, draftInvalid("CONSENT_REVIEW")
	}
	now := s.clock().UTC()
	if review.ID != current.ConsentReviewID || review.OrganisationID != current.OrganisationID || review.Channel != "WHATSAPP" || review.WordingVersion == "" || purpose.WordingVersion != review.WordingVersion ||
		(review.Scope == consent.ReviewScopeCampaign && review.CampaignID != current.ID) || (review.CampaignID != "" && review.CampaignID != current.ID) || review.RevokedAt != nil || review.SupersededByID != "" || (review.ExpiresAt != nil && !review.ExpiresAt.After(now)) {
		return Campaign{}, draftInvalid("CONSENT_REVIEW")
	}
	switch review.Status {
	case consent.StatusDraft, consent.StatusPending, consent.StatusApproved:
	default:
		return Campaign{}, draftInvalid("CONSENT_REVIEW")
	}
	for _, poolID := range []string{current.Transport.SenderPoolID, input.Transport.SenderPoolID} {
		pool, err := s.draftPools.GetPool(ctx, poolID)
		if err != nil || pool.ID != poolID || pool.Status != "ACTIVE" {
			return Campaign{}, draftInvalid("SENDER_POOL")
		}
	}
	transport := input.Transport
	if sameDraftTransport(current.Transport, transport) {
		transport = current.Transport
	} else {
		transport.ProviderDefinitionID = ""
		transport.ProviderDefinitionVersion = 0
		transport.GatewayPoolVersion = 0
	}
	transport, err = s.resolveProviderCapabilities(ctx, transport, s.providerEffectiveAt(input.RequestedStartAt))
	if err != nil {
		return Campaign{}, draftInvalid("TRANSPORT_BINDING")
	}
	// Every successful save validates the exact saved authority. No hidden rebinding
	// of a name/schedule-only save is permitted.
	if sameDraftTransport(current.Transport, input.Transport) && (transport.ProviderDefinitionID != current.Transport.ProviderDefinitionID || transport.ProviderDefinitionVersion != current.Transport.ProviderDefinitionVersion || transport.GatewayPoolVersion != current.Transport.GatewayPoolVersion) {
		return Campaign{}, draftInvalid("TRANSPORT_BINDING")
	}
	input.Transport = transport
	next, event, err := current.SaveDraft(input, now)
	if err != nil {
		return Campaign{}, err
	}
	return s.repository.SaveDraft(ctx, next, event, input.ExpectedVersion)
}

// cloneDraftCampaign keeps the returned atomic snapshot independent of the store.
func cloneDraftCampaign(c Campaign) Campaign {
	if c.RequestedStartAt != nil {
		v := *c.RequestedStartAt
		c.RequestedStartAt = &v
	}
	if c.CompletionDeadlineAt != nil {
		v := *c.CompletionDeadlineAt
		c.CompletionDeadlineAt = &v
	}
	c.Transport.RequiredCapabilities = append([]string{}, c.Transport.RequiredCapabilities...)
	return c
}
func (r *MemoryRepository) SaveDraft(_ context.Context, next Campaign, event MaterialChangeEvent, expected int64) (Campaign, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.items[next.ID]
	if !ok {
		return Campaign{}, ErrNotFound
	}
	if err := GuardDraftSave(current, next, event, expected); err != nil {
		return Campaign{}, err
	}
	if next.Version == expected {
		return cloneDraftCampaign(current), nil
	}
	next = cloneDraftCampaign(next)
	event.Sequence = int64(len(r.events[next.ID]) + 1)
	event.ChangedFields = append([]string{}, event.ChangedFields...)
	r.items[next.ID] = next
	r.events[next.ID] = append(r.events[next.ID], event)
	return cloneDraftCampaign(next), nil
}
