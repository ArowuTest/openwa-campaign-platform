package privacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/audit"
	sharedcrypto "campaign-platform/internal/shared/crypto"
	"campaign-platform/internal/shared/id"
)

type Repository interface {
	Create(context.Context, Case, Event) (Case, error)
	Get(context.Context, string) (Case, error)
	List(context.Context, Query) (Page, error)
	ListEvents(context.Context, string, int) ([]Event, error)
	AppendEvent(context.Context, Event) error
	Assign(context.Context, string, string, string, int64, time.Time, Event) (Case, error)
	Submit(context.Context, string, string, int64, time.Time, Event) (Case, error)
	Decide(context.Context, string, bool, string, string, int64, time.Time, Event) (Case, error)
	LoadSubjectPackage(context.Context, []byte, time.Time) (SubjectPackage, error)
	Execute(context.Context, Case, string, string, []byte, string, string, time.Time, Event) (Case, error)
	CreateLegalHold(context.Context, LegalHold, LegalHoldEvent) (LegalHold, error)
	SubmitLegalHold(context.Context, string, string, int64, time.Time, LegalHoldEvent) (LegalHold, error)
	DecideLegalHold(context.Context, string, bool, string, string, int64, time.Time, LegalHoldEvent) (LegalHold, error)
	ListLegalHoldEvents(context.Context, string, int) ([]LegalHoldEvent, error)
	ListLegalHolds(context.Context, []byte, bool, int) ([]LegalHold, error)
	ReleaseLegalHold(context.Context, string, string, string, int64, time.Time, LegalHoldEvent) (LegalHold, error)
	HasActiveLegalHold(context.Context, []byte, string, time.Time) (bool, error)
}

type Service struct {
	Repository Repository
	Protector  *sharedcrypto.MSISDNProtector
	Evidence   *sharedcrypto.SecretKeyring
	Audit      *audit.Recorder
	Clock      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) Create(ctx context.Context, input CreateInput, correlation string) (Case, error) {
	if s == nil || s.Repository == nil || s.Protector == nil {
		return Case{}, errors.New("privacy service is not configured")
	}
	if !validType(input.Type) || len(strings.TrimSpace(input.Reason)) < 8 || strings.TrimSpace(input.CreatedBy) == "" {
		return Case{}, ErrInvalid
	}
	normalized, err := sharedcrypto.NormalizeE164(input.MSISDN)
	if err != nil {
		return Case{}, err
	}
	changes, err := json.Marshal(input.RequestedChanges)
	if err != nil || len(changes) > 64<<10 {
		return Case{}, ErrInvalid
	}
	if string(changes) == "null" {
		changes = []byte(`{}`)
	}
	identifier, err := id.New()
	if err != nil {
		return Case{}, err
	}
	now := s.now()
	due := now.Add(30 * 24 * time.Hour)
	if input.DueAt != nil {
		due = input.DueAt.UTC()
	}
	if !due.After(now) || due.After(now.Add(366*24*time.Hour)) {
		return Case{}, ErrInvalid
	}
	item := Case{ID: identifier, Type: input.Type, Status: StatusOpen, SubjectLookupHMAC: s.Protector.LookupHMAC(normalized), SubjectMasked: sharedcrypto.Mask(normalized), OrganisationID: strings.TrimSpace(input.OrganisationID), RequestedAt: now, DueAt: due, CreatedBy: input.CreatedBy, RequestReason: strings.TrimSpace(input.Reason), RequestedChanges: changes, Version: 1, CreatedAt: now, UpdatedAt: now}
	event, err := newEvent(item.ID, "CREATED", input.CreatedBy, input.Reason, item.Version, map[string]any{"type": item.Type, "dueAt": item.DueAt}, now)
	if err != nil {
		return Case{}, err
	}
	out, err := s.Repository.Create(ctx, item, event)
	if err == nil {
		err = s.record(ctx, input.CreatedBy, "PRIVACY_CASE_CREATED", out.ID, map[string]any{"type": out.Type, "subject": out.SubjectMasked}, input.Reason, correlation, now)
	}
	return out, err
}

func (s *Service) Get(ctx context.Context, identifier string) (Case, error) {
	return s.Repository.Get(ctx, strings.TrimSpace(identifier))
}

func (s *Service) List(ctx context.Context, query Query) (Page, error) {
	return s.Repository.List(ctx, query)
}

func (s *Service) Events(ctx context.Context, identifier string, limit int) ([]Event, error) {
	return s.Repository.ListEvents(ctx, identifier, limit)
}

func (s *Service) RecordAccess(ctx context.Context, actor, action, objectID, correlation string, details any) error {
	action = strings.ToUpper(strings.TrimSpace(action))
	actor = strings.TrimSpace(actor)
	if actor == "" || action == "" {
		return ErrInvalid
	}
	return s.record(ctx, actor, action, strings.TrimSpace(objectID), details, "sensitive privacy data accessed", correlation, s.now())
}

func (s *Service) Assign(ctx context.Context, identifier, assignee, reason, actor, correlation string, expected int64) (Case, error) {
	if strings.TrimSpace(assignee) == "" || len(strings.TrimSpace(reason)) < 8 {
		return Case{}, ErrInvalid
	}
	now := s.now()
	event, err := newEvent(identifier, "ASSIGNED", actor, reason, expected+1, map[string]any{"assignedTo": assignee}, now)
	if err != nil {
		return Case{}, err
	}
	out, err := s.Repository.Assign(ctx, identifier, assignee, reason, expected, now, event)
	if err == nil {
		err = s.record(ctx, actor, "PRIVACY_CASE_ASSIGNED", out.ID, map[string]any{"assignedTo": out.AssignedTo}, reason, correlation, now)
	}
	return out, err
}

func (s *Service) Submit(ctx context.Context, identifier, reason, actor, correlation string, expected int64) (Case, error) {
	if len(strings.TrimSpace(reason)) < 8 {
		return Case{}, ErrInvalid
	}
	now := s.now()
	event, err := newEvent(identifier, "SUBMITTED", actor, reason, expected+1, nil, now)
	if err != nil {
		return Case{}, err
	}
	out, err := s.Repository.Submit(ctx, identifier, actor, expected, now, event)
	if err == nil {
		err = s.record(ctx, actor, "PRIVACY_CASE_SUBMITTED", out.ID, map[string]any{"status": out.Status}, reason, correlation, now)
	}
	return out, err
}

func (s *Service) Decide(ctx context.Context, identifier string, approve bool, reason, actor, correlation string, expected int64) (Case, error) {
	current, err := s.Repository.Get(ctx, identifier)
	if err != nil {
		return Case{}, err
	}
	if current.CreatedBy == actor || current.SubmittedBy == actor || len(strings.TrimSpace(reason)) < 8 {
		return Case{}, ErrInvalid
	}
	now := s.now()
	typeName := "REJECTED"
	if approve {
		typeName = "APPROVED"
	}
	event, err := newEvent(identifier, typeName, actor, reason, expected+1, nil, now)
	if err != nil {
		return Case{}, err
	}
	out, err := s.Repository.Decide(ctx, identifier, approve, reason, actor, expected, now, event)
	if err == nil {
		err = s.record(ctx, actor, "PRIVACY_CASE_"+typeName, out.ID, map[string]any{"status": out.Status}, reason, correlation, now)
	}
	return out, err
}

func (s *Service) Execute(ctx context.Context, identifier, reason, actor, correlation string, expected int64) (Case, error) {
	if s.Evidence == nil {
		return Case{}, errors.New("privacy evidence keyring is not configured")
	}
	current, err := s.Repository.Get(ctx, identifier)
	if err != nil {
		return Case{}, err
	}
	if current.Status != StatusApproved || current.DecidedBy == actor || current.CreatedBy == actor || len(strings.TrimSpace(reason)) < 8 {
		return Case{}, ErrConflict
	}
	now := s.now()
	if current.Type == CaseErasure {
		blocked, err := s.Repository.HasActiveLegalHold(ctx, current.SubjectLookupHMAC, "CONTACT", now)
		if err != nil {
			return Case{}, err
		}
		if blocked {
			return Case{}, ErrLegalHold
		}
	}
	var ciphertext []byte
	var keyVersion, checksum string
	if current.Type == CaseAccess || current.Type == CasePortability {
		pkg, err := s.Repository.LoadSubjectPackage(ctx, current.SubjectLookupHMAC, now)
		if err != nil {
			return Case{}, err
		}
		msisdn, err := s.Protector.Decrypt(pkg.EncryptedMSISDN)
		if err != nil {
			return Case{}, err
		}
		payload := map[string]any{"caseId": current.ID, "caseType": current.Type, "msisdn": msisdn, "subject": pkg}
		plain, err := json.Marshal(payload)
		if err != nil {
			return Case{}, err
		}
		sum := sha256.Sum256(plain)
		checksum = hex.EncodeToString(sum[:])
		ciphertext, keyVersion, err = s.Evidence.Seal("privacy-case:"+current.ID, string(plain))
		for i := range plain {
			plain[i] = 0
		}
		if err != nil {
			return Case{}, err
		}
	}
	event, err := newEvent(identifier, "COMPLETED", actor, reason, expected+1, map[string]any{"resultSha256": checksum}, now)
	if err != nil {
		return Case{}, err
	}
	out, err := s.Repository.Execute(ctx, current, actor, reason, ciphertext, keyVersion, checksum, now, event)
	if err == nil {
		err = s.record(ctx, actor, "PRIVACY_CASE_EXECUTED", out.ID, map[string]any{"type": out.Type, "resultSha256": out.ResultSHA256}, reason, correlation, now)
	}
	return out, err
}

func (s *Service) ExportEnvelope(ctx context.Context, identifier, actor, correlation string) (json.RawMessage, error) {
	current, err := s.Repository.Get(ctx, identifier)
	if err != nil {
		return nil, err
	}
	if current.Status != StatusCompleted || len(current.ResultCiphertext) == 0 || current.ResultKeyVersion == "" || current.ResultSHA256 == "" {
		return nil, ErrConflict
	}
	event, err := newEvent(current.ID, "EXPORT_REQUESTED", actor, "controlled privacy export requested", current.Version, map[string]any{"resultSha256": current.ResultSHA256}, s.now())
	if err != nil {
		return nil, err
	}
	if err = s.Repository.AppendEvent(ctx, event); err != nil {
		return nil, err
	}
	envelope, err := json.Marshal(EncryptedPackageEnvelope{CaseID: current.ID, Ciphertext: current.ResultCiphertext, KeyVersion: current.ResultKeyVersion, PlaintextSHA256: current.ResultSHA256})
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, actor, "PRIVACY_EXPORT_REQUESTED", current.ID, map[string]any{"resultSha256": current.ResultSHA256}, "controlled privacy export requested", correlation, s.now()); err != nil {
		return nil, err
	}
	return envelope, nil
}

func (s *Service) ResolvePrivacyPackage(raw json.RawMessage) (json.RawMessage, error) {
	return s.ResolveExportEnvelope(raw)
}

func (s *Service) ResolveExportEnvelope(raw json.RawMessage) (json.RawMessage, error) {
	if s == nil || s.Evidence == nil {
		return nil, errors.New("privacy evidence keyring is not configured")
	}
	var envelope EncryptedPackageEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode privacy package envelope: %w", err)
	}
	if strings.TrimSpace(envelope.CaseID) == "" || strings.TrimSpace(envelope.KeyVersion) == "" || len(envelope.Ciphertext) == 0 || len(envelope.PlaintextSHA256) != 64 {
		return nil, ErrInvalid
	}
	plain, err := s.Evidence.Open(envelope.KeyVersion, "privacy-case:"+envelope.CaseID, envelope.Ciphertext)
	if err != nil {
		return nil, err
	}
	plainBytes := []byte(plain)
	sum := sha256.Sum256(plainBytes)
	if hex.EncodeToString(sum[:]) != envelope.PlaintextSHA256 {
		for i := range plainBytes {
			plainBytes[i] = 0
		}
		return nil, errors.New("privacy package integrity verification failed")
	}
	payload := json.RawMessage(append([]byte(nil), plainBytes...))
	for i := range plainBytes {
		plainBytes[i] = 0
	}
	return payload, nil
}

func (s *Service) CreateLegalHold(ctx context.Context, msisdn, organisationID, scope, reason, actor, correlation string, expiresAt *time.Time) (LegalHold, error) {
	if !validHoldScope(scope) || len(strings.TrimSpace(reason)) < 8 {
		return LegalHold{}, ErrInvalid
	}
	normalized, err := sharedcrypto.NormalizeE164(msisdn)
	if err != nil {
		return LegalHold{}, err
	}
	identifier, err := id.New()
	if err != nil {
		return LegalHold{}, err
	}
	now := s.now()
	if expiresAt != nil && !expiresAt.After(now) {
		return LegalHold{}, ErrInvalid
	}
	hold := LegalHold{ID: identifier, SubjectLookupHMAC: s.Protector.LookupHMAC(normalized), OrganisationID: strings.TrimSpace(organisationID), Scope: strings.ToUpper(strings.TrimSpace(scope)), Status: HoldDraft, Reason: strings.TrimSpace(reason), CreatedBy: actor, CreatedAt: now, ExpiresAt: expiresAt, Version: 1}
	event, err := newLegalHoldEvent(identifier, "CREATED", actor, reason, hold.Version, map[string]any{"scope": hold.Scope, "expiresAt": hold.ExpiresAt}, now)
	if err != nil {
		return LegalHold{}, err
	}
	out, err := s.Repository.CreateLegalHold(ctx, hold, event)
	if err == nil {
		err = s.record(ctx, actor, "PRIVACY_LEGAL_HOLD_CREATED", out.ID, map[string]any{"scope": out.Scope, "status": out.Status}, reason, correlation, now)
	}
	return out, err
}

func (s *Service) SubmitLegalHold(ctx context.Context, identifier, reason, actor, correlation string, expected int64) (LegalHold, error) {
	if len(strings.TrimSpace(reason)) < 8 {
		return LegalHold{}, ErrInvalid
	}
	now := s.now()
	event, err := newLegalHoldEvent(identifier, "SUBMITTED", actor, reason, expected+1, nil, now)
	if err != nil {
		return LegalHold{}, err
	}
	out, err := s.Repository.SubmitLegalHold(ctx, identifier, actor, expected, now, event)
	if err == nil {
		err = s.record(ctx, actor, "PRIVACY_LEGAL_HOLD_SUBMITTED", out.ID, map[string]any{"status": out.Status}, reason, correlation, now)
	}
	return out, err
}

func (s *Service) DecideLegalHold(ctx context.Context, identifier string, approve bool, reason, actor, correlation string, expected int64) (LegalHold, error) {
	if len(strings.TrimSpace(reason)) < 8 {
		return LegalHold{}, ErrInvalid
	}
	currentEvents, err := s.Repository.ListLegalHoldEvents(ctx, identifier, 10)
	if err != nil {
		return LegalHold{}, err
	}
	for _, event := range currentEvents {
		if (event.Type == "CREATED" || event.Type == "SUBMITTED") && event.ActorID == actor {
			return LegalHold{}, ErrInvalid
		}
	}
	now := s.now()
	eventType := "REJECTED"
	if approve {
		eventType = "APPROVED"
	}
	event, err := newLegalHoldEvent(identifier, eventType, actor, reason, expected+1, nil, now)
	if err != nil {
		return LegalHold{}, err
	}
	out, err := s.Repository.DecideLegalHold(ctx, identifier, approve, reason, actor, expected, now, event)
	if err == nil {
		err = s.record(ctx, actor, "PRIVACY_LEGAL_HOLD_"+eventType, out.ID, map[string]any{"status": out.Status}, reason, correlation, now)
	}
	return out, err
}

func (s *Service) LegalHoldEvents(ctx context.Context, identifier string, limit int) ([]LegalHoldEvent, error) {
	return s.Repository.ListLegalHoldEvents(ctx, strings.TrimSpace(identifier), limit)
}

func (s *Service) ListLegalHolds(ctx context.Context, msisdn string, activeOnly bool, limit int) ([]LegalHold, error) {
	normalized, err := sharedcrypto.NormalizeE164(msisdn)
	if err != nil {
		return nil, err
	}
	return s.Repository.ListLegalHolds(ctx, s.Protector.LookupHMAC(normalized), activeOnly, limit)
}

func (s *Service) ReleaseLegalHold(ctx context.Context, identifier, reason, actor, correlation string, expected int64) (LegalHold, error) {
	if len(strings.TrimSpace(reason)) < 8 {
		return LegalHold{}, ErrInvalid
	}
	now := s.now()
	event, err := newLegalHoldEvent(identifier, "RELEASED", actor, reason, expected+1, nil, now)
	if err != nil {
		return LegalHold{}, err
	}
	out, err := s.Repository.ReleaseLegalHold(ctx, identifier, reason, actor, expected, now, event)
	if err == nil {
		err = s.record(ctx, actor, "PRIVACY_LEGAL_HOLD_RELEASED", out.ID, map[string]any{"releasedAt": out.ReleasedAt}, reason, correlation, now)
	}
	return out, err
}

func newLegalHoldEvent(holdID, eventType, actor, reason string, version int64, evidence any, now time.Time) (LegalHoldEvent, error) {
	identifier, err := id.New()
	if err != nil {
		return LegalHoldEvent{}, err
	}
	payload, err := json.Marshal(evidence)
	if err != nil {
		return LegalHoldEvent{}, err
	}
	if string(payload) == "null" {
		payload = []byte(`{}`)
	}
	return LegalHoldEvent{ID: identifier, LegalHoldID: holdID, Type: eventType, ActorID: actor, Reason: strings.TrimSpace(reason), HoldVersion: version, Evidence: payload, OccurredAt: now.UTC()}, nil
}

func newEvent(caseID, eventType, actor, reason string, version int64, evidence any, now time.Time) (Event, error) {
	identifier, err := id.New()
	if err != nil {
		return Event{}, err
	}
	payload, err := json.Marshal(evidence)
	if err != nil {
		return Event{}, err
	}
	if string(payload) == "null" {
		payload = []byte(`{}`)
	}
	return Event{ID: identifier, CaseID: caseID, Type: eventType, ActorID: actor, Reason: strings.TrimSpace(reason), CaseVersion: version, Evidence: payload, OccurredAt: now.UTC()}, nil
}

func (s *Service) record(ctx context.Context, actor, action, objectID string, after any, reason, correlation string, at time.Time) error {
	if s.Audit == nil {
		return nil
	}
	_, err := s.Audit.Record(ctx, audit.Input{ActorType: "USER", ActorID: actor, Action: action, ObjectType: "PRIVACY_CASE", ObjectID: objectID, Outcome: "SUCCESS", Sensitivity: "HIGH", After: after, Reason: reason, CorrelationID: correlation, OccurredAt: at})
	return err
}

func ValidateRequestedChanges(caseType CaseType, raw json.RawMessage) error {
	if caseType != CaseRectification {
		return nil
	}
	var changes map[string]any
	if err := json.Unmarshal(raw, &changes); err != nil {
		return err
	}
	allowed := map[string]struct{}{"reportedAge": {}, "ageRecordedAt": {}, "ageSource": {}, "ageVerified": {}, "genderCode": {}, "preferredLanguageCode": {}, "countryId": {}, "stateId": {}, "lgaId": {}, "status": {}}
	for key := range changes {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unsupported rectification field %q", key)
		}
	}
	return nil
}
