package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"campaign-platform/internal/audit"
	"campaign-platform/internal/delivery"
	"campaign-platform/internal/shared/id"
)

type Repository interface {
	Dashboard(context.Context, time.Time) (Dashboard, error)
	ListIncidents(context.Context, IncidentStatus, int) ([]Incident, error)
	GetIncident(context.Context, string) (Incident, error)
	CreateIncident(context.Context, Incident) (Incident, error)
	UpdateIncident(context.Context, Incident, int64) (Incident, error)
	CampaignReport(context.Context, string, time.Time) (CampaignReport, error)
	CampaignFinancialReconciliation(context.Context, string, time.Time) (CampaignFinancialReconciliation, error)
	OrganisationPerformanceReport(context.Context, string, time.Time) (OrganisationPerformanceReport, error)
	ListExceptions(context.Context, string, int) ([]DeliveryException, error)
	CreateExport(context.Context, ExportRequest) (ExportRequest, error)
	GetExport(context.Context, string) (ExportRequest, error)
	UpdateExport(context.Context, ExportRequest, int64) (ExportRequest, error)
}
type Service struct {
	Repo            Repository
	AuditRepository audit.Repository
	Audit           *audit.Recorder
	Deliveries      *delivery.Service
	Clock           func() time.Time
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) SearchAudit(ctx context.Context, after uint64, limit int) ([]audit.Event, error) {
	if s.AuditRepository == nil {
		return nil, errors.New("audit query is unavailable")
	}
	return s.AuditRepository.List(ctx, after, limit)
}
func (s *Service) Dashboard(ctx context.Context) (Dashboard, error) {
	return s.Repo.Dashboard(ctx, s.now())
}
func (s *Service) ListIncidents(ctx context.Context, status IncidentStatus, limit int) ([]Incident, error) {
	return s.Repo.ListIncidents(ctx, status, limit)
}
func (s *Service) CreateIncident(ctx context.Context, in Incident, actor, correlation string) (Incident, error) {
	if err := ValidateIncident(in); err != nil {
		return Incident{}, err
	}
	ident, err := id.New()
	if err != nil {
		return Incident{}, err
	}
	now := s.now()
	in.ID = ident
	in.Status = IncidentOpen
	in.CreatedAt = now
	in.UpdatedAt = now
	in.Version = 1
	out, err := s.Repo.CreateIncident(ctx, in)
	if err != nil {
		return Incident{}, err
	}
	if s.Audit != nil {
		_, err = s.Audit.Record(ctx, audit.Input{ActorType: "USER", ActorID: actor, Action: "OPERATIONS_INCIDENT_CREATED", ObjectType: "OPERATIONS_INCIDENT", ObjectID: out.ID, After: map[string]any{"severity": out.Severity, "category": out.Category, "campaignId": out.CampaignID}, CorrelationID: correlation, OccurredAt: now})
	}
	return out, err
}
func (s *Service) UpdateIncident(ctx context.Context, id string, expected int64, status IncidentStatus, owner, resolution, actor, correlation string) (Incident, error) {
	current, err := s.Repo.GetIncident(ctx, id)
	if err != nil {
		return Incident{}, err
	}
	current.Status = status
	current.OwnerID = strings.TrimSpace(owner)
	current.Resolution = strings.TrimSpace(resolution)
	current.UpdatedAt = s.now()
	if status == IncidentResolved {
		if current.Resolution == "" {
			return Incident{}, ErrInvalid
		}
		t := current.UpdatedAt
		current.ResolvedAt = &t
	}
	out, err := s.Repo.UpdateIncident(ctx, current, expected)
	if err != nil {
		return Incident{}, err
	}
	if s.Audit != nil {
		_, err = s.Audit.Record(ctx, audit.Input{ActorType: "USER", ActorID: actor, Action: "OPERATIONS_INCIDENT_UPDATED", ObjectType: "OPERATIONS_INCIDENT", ObjectID: id, After: map[string]any{"status": out.Status, "ownerId": out.OwnerID}, Reason: out.Resolution, CorrelationID: correlation, OccurredAt: out.UpdatedAt})
	}
	return out, err
}
func (s *Service) ListExceptions(ctx context.Context, campaignID string, limit int) ([]DeliveryException, error) {
	return s.Repo.ListExceptions(ctx, campaignID, limit)
}

func (s *Service) CampaignReport(ctx context.Context, id string) (CampaignReport, error) {
	return s.Repo.CampaignReport(ctx, id, s.now())
}

func (s *Service) CampaignFinancialReconciliation(ctx context.Context, id string) (CampaignFinancialReconciliation, error) {
	return s.Repo.CampaignFinancialReconciliation(ctx, id, s.now())
}

func (s *Service) OrganisationPerformanceReport(ctx context.Context, id string) (OrganisationPerformanceReport, error) {
	return s.Repo.OrganisationPerformanceReport(ctx, id, s.now())
}
func (s *Service) RequestExport(ctx context.Context, kind, objectID, format, reason, actor, correlation string) (ExportRequest, error) {
	kind = strings.ToUpper(strings.TrimSpace(kind))
	format = strings.ToUpper(strings.TrimSpace(format))
	if kind != "CAMPAIGN_REPORT" && kind != "AUDIT_LOG" {
		return ExportRequest{}, ErrInvalid
	}
	if format != "CSV" && format != "JSON" && format != "PDF" && format != "XLSX" {
		return ExportRequest{}, ErrInvalid
	}
	if strings.TrimSpace(reason) == "" {
		return ExportRequest{}, ErrInvalid
	}
	ident, err := id.New()
	if err != nil {
		return ExportRequest{}, err
	}
	now := s.now()
	in := ExportRequest{ID: ident, Kind: kind, ObjectID: objectID, Format: format, Status: ExportPending, RequestedBy: actor, Reason: reason, CreatedAt: now, UpdatedAt: now, Version: 1}
	out, err := s.Repo.CreateExport(ctx, in)
	if err == nil && s.Audit != nil {
		_, err = s.Audit.Record(ctx, audit.Input{ActorType: "USER", ActorID: actor, Action: "EXPORT_REQUESTED", ObjectType: "EXPORT_REQUEST", ObjectID: out.ID, After: map[string]any{"kind": kind, "format": format, "objectId": objectID}, Reason: reason, CorrelationID: correlation, OccurredAt: now})
	}
	return out, err
}
func (s *Service) DecideExport(ctx context.Context, id string, expected int64, approve bool, reason, actor, correlation string) (ExportRequest, error) {
	current, err := s.Repo.GetExport(ctx, id)
	if err != nil {
		return ExportRequest{}, err
	}
	if current.RequestedBy == actor {
		return ExportRequest{}, errors.New("maker-checker violation")
	}
	if current.Status != ExportPending {
		return ExportRequest{}, ErrConflict
	}
	now := s.now()
	current.UpdatedAt = now
	current.ApprovedBy = actor
	if approve {
		current.Status = ExportApproved
		current.ExpiresAt = nil
	} else {
		if strings.TrimSpace(reason) == "" {
			return ExportRequest{}, ErrInvalid
		}
		current.Status = ExportRejected
		current.RejectionReason = reason
	}
	out, err := s.Repo.UpdateExport(ctx, current, expected)
	if err == nil && s.Audit != nil {
		action := "EXPORT_REJECTED"
		if approve {
			action = "EXPORT_APPROVED"
		}
		_, err = s.Audit.Record(ctx, audit.Input{ActorType: "USER", ActorID: actor, Action: action, ObjectType: "EXPORT_REQUEST", ObjectID: id, After: map[string]any{"status": out.Status, "expiresAt": out.ExpiresAt}, Reason: reason, CorrelationID: correlation, OccurredAt: now})
	}
	return out, err
}

type DeliveryResolutionAction string

const (
	ResolutionConfirmSent         DeliveryResolutionAction = "CONFIRM_SENT"
	ResolutionConfirmDelivered    DeliveryResolutionAction = "CONFIRM_DELIVERED"
	ResolutionConfirmRead         DeliveryResolutionAction = "CONFIRM_READ"
	ResolutionMarkFailedPermanent DeliveryResolutionAction = "MARK_FAILED_PERMANENT"
	ResolutionConfirmNotSubmitted DeliveryResolutionAction = "CONFIRM_NOT_SUBMITTED"
)

type DeliveryResolution struct {
	RecipientID  string                   `json:"recipientId"`
	Action       DeliveryResolutionAction `json:"action"`
	EvidenceRef  string                   `json:"evidenceRef"`
	Reason       string                   `json:"reason"`
	ActorID      string                   `json:"actorId"`
	ResolvedAt   time.Time                `json:"resolvedAt"`
	ResultStatus delivery.Status          `json:"resultStatus"`
}

func (s *Service) ResolveDeliveryException(ctx context.Context, recipientID string, action DeliveryResolutionAction, evidenceRef, reason, actor, correlation string) (DeliveryResolution, error) {
	if s == nil || s.Deliveries == nil {
		return DeliveryResolution{}, errors.New("delivery reconciliation is unavailable")
	}
	recipientID, evidenceRef, reason, actor = strings.TrimSpace(recipientID), strings.TrimSpace(evidenceRef), strings.TrimSpace(reason), strings.TrimSpace(actor)
	if recipientID == "" || actor == "" || len(evidenceRef) < 6 || len(reason) < 8 {
		return DeliveryResolution{}, ErrInvalid
	}
	current, err := s.Deliveries.Get(ctx, recipientID)
	if err != nil {
		return DeliveryResolution{}, err
	}
	if current.Status != delivery.StatusUnknown && !current.ReconciliationRequired {
		return DeliveryResolution{}, ErrConflict
	}
	var eventType delivery.EventType
	switch action {
	case ResolutionConfirmSent:
		eventType = delivery.EventSent
	case ResolutionConfirmDelivered:
		eventType = delivery.EventDelivered
	case ResolutionConfirmRead:
		eventType = delivery.EventRead
	case ResolutionMarkFailedPermanent:
		eventType = delivery.EventFailedPermanent
	case ResolutionConfirmNotSubmitted:
		if current.ProviderMessageID != "" || current.HighestAcknowledgement != "" {
			return DeliveryResolution{}, ErrConflict
		}
		eventType = delivery.EventFailedRetryable
	default:
		return DeliveryResolution{}, ErrInvalid
	}
	now := s.now()
	sum := sha256.Sum256([]byte(strings.Join([]string{recipientID, string(action), evidenceRef}, "")))
	key := "operator-resolution:" + hex.EncodeToString(sum[:])
	result, _, err := s.Deliveries.ApplyEvent(ctx, recipientID, delivery.Event{DeduplicationKey: key, Type: eventType, ProviderMessageID: current.ProviderMessageID, ErrorCode: "OPERATOR_RECONCILIATION", ErrorDetail: reason, OccurredAt: now})
	if err != nil {
		return DeliveryResolution{}, err
	}
	result, err = s.Deliveries.ResolveReconciliation(ctx, recipientID, result.Status, actor, string(action), evidenceRef, reason, now)
	if err != nil {
		return DeliveryResolution{}, err
	}
	resolution := DeliveryResolution{RecipientID: recipientID, Action: action, EvidenceRef: evidenceRef, Reason: reason, ActorID: actor, ResolvedAt: now, ResultStatus: result.Status}
	if s.Audit != nil {
		_, err = s.Audit.Record(ctx, audit.Input{ActorType: "USER", ActorID: actor, Action: "DELIVERY_EXCEPTION_RESOLVED", ObjectType: "CAMPAIGN_RECIPIENT", ObjectID: recipientID, After: map[string]any{"action": action, "evidenceRef": evidenceRef, "resultStatus": result.Status}, Reason: reason, CorrelationID: correlation, OccurredAt: now})
		if err != nil {
			return DeliveryResolution{}, err
		}
	}
	return resolution, nil
}
