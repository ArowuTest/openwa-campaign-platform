package operations

import (
	"context"
	"errors"
	"strings"
	"time"

	"campaign-platform/internal/audit"
	"campaign-platform/internal/shared/id"
)

type Repository interface {
	Dashboard(context.Context, time.Time) (Dashboard, error)
	ListIncidents(context.Context, IncidentStatus, int) ([]Incident, error)
	GetIncident(context.Context, string) (Incident, error)
	CreateIncident(context.Context, Incident) (Incident, error)
	UpdateIncident(context.Context, Incident, int64) (Incident, error)
	CampaignReport(context.Context, string, time.Time) (CampaignReport, error)
	ListExceptions(context.Context, string, int) ([]DeliveryException, error)
	CreateExport(context.Context, ExportRequest) (ExportRequest, error)
	GetExport(context.Context, string) (ExportRequest, error)
	UpdateExport(context.Context, ExportRequest, int64) (ExportRequest, error)
}
type Service struct {
	Repo            Repository
	AuditRepository audit.Repository
	Audit           *audit.Recorder
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
