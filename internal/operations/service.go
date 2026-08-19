package operations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
	ListExports(context.Context, ExportQuery) (ExportPage, error)
	UpdateExport(context.Context, ExportRequest, int64) (ExportRequest, error)
	CreateDownloadGrant(context.Context, DownloadGrant) (DownloadGrant, error)
	ConsumeDownloadGrant(context.Context, string, string, string, time.Time) (ExportRequest, DownloadGrant, error)
	RevokeDownloadGrants(context.Context, string, time.Time) error
}
type IncidentEvidenceRepository interface {
	CreateIncidentWithEvents(context.Context, Incident, []IncidentEvent) (Incident, error)
	UpdateIncidentWithEvents(context.Context, Incident, int64, []IncidentEvent) (Incident, error)
}
type IncidentAtomicAuditRepository interface {
	CreateIncidentWithEventsAndAudit(context.Context, Incident, []IncidentEvent, audit.Input) (Incident, error)
	UpdateIncidentWithEventsAndAudit(context.Context, Incident, int64, []IncidentEvent, audit.Input) (Incident, error)
}
type ExportAtomicAuditRepository interface {
	CreateExportWithAudit(context.Context, ExportRequest, audit.Input) (ExportRequest, error)
	UpdateExportWithAudit(context.Context, ExportRequest, int64, audit.Input) (ExportRequest, error)
	RevokeExportWithAudit(context.Context, ExportRequest, int64, time.Time, audit.Input) (ExportRequest, error)
}
type DownloadAtomicAuditRepository interface {
	CreateDownloadGrantWithAudit(context.Context, DownloadGrant, audit.Input) (DownloadGrant, error)
	ConsumeDownloadGrantWithAudit(context.Context, string, string, string, time.Time, audit.Input) (ExportRequest, DownloadGrant, error)
}
type Service struct {
	Repo             Repository
	AuditRepository  audit.Repository
	Audit            *audit.Recorder
	Deliveries       *delivery.Service
	ReportingPrivacy *ReportingPrivacyAdministration
	Alerting         AlertStore
	Clock            func() time.Time
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) SearchAudit(ctx context.Context, query audit.Query) (audit.Page, error) {
	if s.AuditRepository == nil {
		return audit.Page{}, errors.New("audit query is unavailable")
	}
	return s.AuditRepository.Search(ctx, query)
}
func (s *Service) SearchAuditWithAccess(ctx context.Context, query audit.Query, actor, correlation string) (audit.Page, error) {
	page, err := s.SearchAudit(ctx, query)
	if err != nil {
		return page, err
	}
	if s.Audit != nil {
		_, err = s.Audit.Record(ctx, audit.Input{ActorType: "USER", ActorID: actor, Action: "AUDIT_LOG_SEARCHED", ObjectType: "AUDIT_LOG", ObjectID: "AUDIT_SEARCH", OrganisationID: query.OrganisationID, Outcome: "SUCCESS", Sensitivity: "HIGH", After: map[string]any{"actorId": query.ActorID, "action": query.Action, "objectType": query.ObjectType, "objectId": query.ObjectID, "organisationId": query.OrganisationID, "outcome": query.Outcome, "sensitivity": query.Sensitivity, "resultCount": len(page.Items)}, Reason: "authorised audit search", CorrelationID: correlation, OccurredAt: s.now()})
	}
	return page, err
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
	timeline := IncidentEvent{IncidentID: in.ID, EventType: "CREATED", ActorID: actor, Detail: in.Summary, Evidence: map[string]any{"category": in.Category, "severity": in.Severity}, OccurredAt: now}
	auditInput := audit.Input{ActorType: "USER", ActorID: actor, Action: "OPERATIONS_INCIDENT_CREATED", ObjectType: "OPERATIONS_INCIDENT", ObjectID: in.ID, Outcome: "SUCCESS", Sensitivity: "HIGH", After: map[string]any{"severity": in.Severity, "category": in.Category, "campaignId": in.CampaignID}, CorrelationID: correlation, OccurredAt: now}
	var out Incident
	atomicAudit := false
	if atomicRepo, ok := s.Repo.(IncidentAtomicAuditRepository); ok {
		out, err = atomicRepo.CreateIncidentWithEventsAndAudit(ctx, in, []IncidentEvent{timeline}, auditInput)
		atomicAudit = true
	} else if atomicRepo, ok := s.Repo.(IncidentEvidenceRepository); ok {
		out, err = atomicRepo.CreateIncidentWithEvents(ctx, in, []IncidentEvent{timeline})
	} else {
		out, err = s.Repo.CreateIncident(ctx, in)
		if err == nil && s.Alerting != nil {
			err = s.Alerting.AddIncidentEvent(ctx, timeline)
		}
	}
	if err != nil {
		return Incident{}, err
	}
	if s.Audit != nil && !atomicAudit {
		_, err = s.Audit.Record(ctx, auditInput)
	}
	return out, err
}
func (s *Service) UpdateIncident(ctx context.Context, id string, expected int64, status IncidentStatus, owner, resolution, actor, correlation string) (Incident, error) {
	current, err := s.Repo.GetIncident(ctx, id)
	if err != nil {
		return Incident{}, err
	}
	previousStatus := current.Status
	previousOwner := current.OwnerID
	owner = strings.TrimSpace(owner)
	detail := strings.TrimSpace(resolution)
	if !allowedIncidentTransition(previousStatus, status) {
		return Incident{}, ErrInvalid
	}
	current.Status = status
	current.OwnerID = owner
	current.UpdatedAt = s.now()
	switch status {
	case IncidentOpen, IncidentAcknowledged, IncidentInvestigating, IncidentMitigated:
		current.Resolution = ""
		current.ResolvedAt = nil
	case IncidentResolved, IncidentClosed:
		if detail == "" {
			return Incident{}, ErrInvalid
		}
		current.Resolution = detail
		t := current.UpdatedAt
		current.ResolvedAt = &t
	default:
		return Incident{}, ErrInvalid
	}
	events := make([]IncidentEvent, 0, 2)
	if previousOwner != owner {
		events = append(events, IncidentEvent{IncidentID: current.ID, EventType: "ASSIGNED", ActorID: actor, Detail: nonEmptyIncidentDetail(detail, "incident owner changed"), Evidence: map[string]any{"previousOwnerId": previousOwner, "ownerId": owner, "version": expected + 1}, OccurredAt: current.UpdatedAt})
	}
	if previousStatus != status {
		eventType := map[IncidentStatus]string{IncidentOpen: "REOPENED", IncidentAcknowledged: "ACKNOWLEDGED", IncidentInvestigating: "INVESTIGATION_NOTE", IncidentMitigated: "MITIGATION", IncidentResolved: "RESOLVED", IncidentClosed: "CLOSED"}[status]
		events = append(events, IncidentEvent{IncidentID: current.ID, EventType: eventType, ActorID: actor, Detail: nonEmptyIncidentDetail(detail, "incident status changed to "+string(status)), Evidence: map[string]any{"previousStatus": previousStatus, "status": status, "ownerId": owner, "version": expected + 1}, OccurredAt: current.UpdatedAt})
	} else if detail != "" && status == IncidentInvestigating {
		events = append(events, IncidentEvent{IncidentID: current.ID, EventType: "INVESTIGATION_NOTE", ActorID: actor, Detail: detail, Evidence: map[string]any{"ownerId": owner, "version": expected + 1}, OccurredAt: current.UpdatedAt})
	}
	if len(events) == 0 {
		return Incident{}, ErrInvalid
	}
	auditInput := audit.Input{ActorType: "USER", ActorID: actor, Action: "OPERATIONS_INCIDENT_UPDATED", ObjectType: "OPERATIONS_INCIDENT", ObjectID: id, Outcome: "SUCCESS", Sensitivity: "HIGH", After: map[string]any{"status": current.Status, "ownerId": current.OwnerID}, Reason: detail, CorrelationID: correlation, OccurredAt: current.UpdatedAt}
	var out Incident
	atomicAudit := false
	if atomicRepo, ok := s.Repo.(IncidentAtomicAuditRepository); ok {
		out, err = atomicRepo.UpdateIncidentWithEventsAndAudit(ctx, current, expected, events, auditInput)
		atomicAudit = true
	} else if atomicRepo, ok := s.Repo.(IncidentEvidenceRepository); ok {
		out, err = atomicRepo.UpdateIncidentWithEvents(ctx, current, expected, events)
	} else {
		out, err = s.Repo.UpdateIncident(ctx, current, expected)
		if err == nil && s.Alerting != nil {
			for _, event := range events {
				if eventErr := s.Alerting.AddIncidentEvent(ctx, event); eventErr != nil {
					err = eventErr
					break
				}
			}
		}
	}
	if err != nil {
		return Incident{}, err
	}
	if s.Audit != nil && !atomicAudit {
		_, err = s.Audit.Record(ctx, auditInput)
	}
	return out, err
}

func allowedIncidentTransition(from, to IncidentStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case IncidentOpen, IncidentAcknowledged, IncidentInvestigating, IncidentMitigated:
		return to == IncidentOpen || to == IncidentAcknowledged || to == IncidentInvestigating || to == IncidentMitigated || to == IncidentResolved || to == IncidentClosed
	case IncidentResolved:
		return to == IncidentClosed
	case IncidentClosed:
		return false
	default:
		return false
	}
}

func nonEmptyIncidentDetail(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func (s *Service) IncidentTimeline(ctx context.Context, id string, limit int) ([]IncidentEvent, error) {
	if s.Alerting == nil {
		return nil, errors.New("incident timeline is unavailable")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	return s.Alerting.ListIncidentEvents(ctx, strings.TrimSpace(id), limit)
}

func (s *Service) ListExceptions(ctx context.Context, campaignID string, limit int) ([]DeliveryException, error) {
	return s.Repo.ListExceptions(ctx, campaignID, limit)
}

func (s *Service) CampaignReport(ctx context.Context, id string) (CampaignReport, error) {
	now := s.now()
	report, err := s.Repo.CampaignReport(ctx, id, now)
	if err != nil {
		return report, err
	}
	policy, err := s.reportingPrivacyPolicy(ctx, report.OrganisationID, now)
	if err != nil {
		return report, err
	}
	applyCampaignReportingPrivacy(&report, policy, now)
	return report, nil
}

func (s *Service) CampaignFinancialReconciliation(ctx context.Context, id string) (CampaignFinancialReconciliation, error) {
	return s.Repo.CampaignFinancialReconciliation(ctx, id, s.now())
}

func (s *Service) OrganisationPerformanceReport(ctx context.Context, id string) (OrganisationPerformanceReport, error) {
	now := s.now()
	report, err := s.Repo.OrganisationPerformanceReport(ctx, id, now)
	if err != nil {
		return report, err
	}
	policy, err := s.reportingPrivacyPolicy(ctx, report.OrganisationID, now)
	if err != nil {
		return report, err
	}
	applyOrganisationReportingPrivacy(&report, policy, now)
	return report, nil
}

func (s *Service) reportingPrivacyPolicy(ctx context.Context, organisationID string, at time.Time) (ReportingPrivacyPolicy, error) {
	if s.ReportingPrivacy == nil {
		return conservativeReportingPrivacyPolicy(at), nil
	}
	return s.ReportingPrivacy.Resolve(ctx, organisationID, at)
}

func applyCampaignReportingPrivacy(report *CampaignReport, policy ReportingPrivacyPolicy, at time.Time) {
	report.Breakdowns, report.Privacy = protectReportBreakdowns(report.RawBreakdowns, policy, at)
	report.RawBreakdowns = nil
}
func applyOrganisationReportingPrivacy(report *OrganisationPerformanceReport, policy ReportingPrivacyPolicy, at time.Time) {
	report.Breakdowns, report.Privacy = protectReportBreakdowns(report.RawBreakdowns, policy, at)
	report.RawBreakdowns = nil
}
func protectReportBreakdowns(raw map[string]map[string]int64, policy ReportingPrivacyPolicy, at time.Time) (map[string][]ReportBreakdownCell, ReportPrivacyEvidence) {
	protected := map[string][]ReportBreakdownCell{}
	suppressed := 0
	dimensions := make([]string, 0, len(raw))
	for dimension := range raw {
		dimensions = append(dimensions, dimension)
	}
	sort.Strings(dimensions)
	for _, dimension := range dimensions {
		if (dimension == "state" || dimension == "lga") && !policy.ApplyGeography {
			continue
		}
		if (dimension == "ageBand" || dimension == "gender") && !policy.ApplyDemographics {
			continue
		}
		if strings.HasPrefix(dimension, "attribute:") && !policy.ApplyAttributes {
			continue
		}
		labels := make([]string, 0, len(raw[dimension]))
		for label := range raw[dimension] {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		cells := make([]ReportBreakdownCell, 0, len(labels))
		for _, label := range labels {
			count := raw[dimension][label]
			cell := ReportBreakdownCell{Label: label}
			if count < int64(policy.MinimumCohortSize) {
				cell.Suppressed = true
				suppressed++
			} else {
				value := count
				cell.Count = &value
			}
			cells = append(cells, cell)
		}
		protected[dimension] = cells
	}
	return protected, ReportPrivacyEvidence{PolicyID: policy.ID, PolicyVersion: policy.Version, MinimumCohortSize: policy.MinimumCohortSize, SuppressionLabel: policy.SuppressionLabel, SuppressedCellCount: suppressed, AppliedAt: at.UTC()}
}

type ExportOptions struct {
	Criteria        any
	TemplateVersion string
	WatermarkText   string
	FrozenPayload   json.RawMessage
}

func (s *Service) RequestExport(ctx context.Context, kind, objectID, format, reason, actor, correlation string) (ExportRequest, error) {
	return s.RequestExportWithOptions(ctx, kind, objectID, format, reason, actor, correlation, ExportOptions{})
}

func (s *Service) RequestExportWithOptions(ctx context.Context, kind, objectID, format, reason, actor, correlation string, options ExportOptions) (ExportRequest, error) {
	kind = strings.ToUpper(strings.TrimSpace(kind))
	format = strings.ToUpper(strings.TrimSpace(format))
	if kind != "CAMPAIGN_REPORT" && kind != "AUDIT_LOG" && kind != "PRIVACY_PACKAGE" {
		return ExportRequest{}, ErrInvalid
	}
	if format != "CSV" && format != "JSON" && format != "PDF" && format != "XLSX" {
		return ExportRequest{}, ErrInvalid
	}
	if strings.TrimSpace(reason) == "" || strings.TrimSpace(actor) == "" {
		return ExportRequest{}, ErrInvalid
	}
	if (kind == "CAMPAIGN_REPORT" || kind == "PRIVACY_PACKAGE") && strings.TrimSpace(objectID) == "" {
		return ExportRequest{}, ErrInvalid
	}
	criteria, err := json.Marshal(options.Criteria)
	if err != nil {
		return ExportRequest{}, fmt.Errorf("marshal export criteria: %w", err)
	}
	if string(criteria) == "null" {
		criteria = json.RawMessage(`{}`)
	}
	if len(criteria) > 64<<10 {
		return ExportRequest{}, ErrInvalid
	}
	identifier, err := id.New()
	if err != nil {
		return ExportRequest{}, err
	}
	templateVersion := strings.TrimSpace(options.TemplateVersion)
	if templateVersion == "" {
		templateVersion = "EXPORT-V1"
	}
	now := s.now()
	frozenPayload := append(json.RawMessage(nil), options.FrozenPayload...)
	if kind == "PRIVACY_PACKAGE" && len(frozenPayload) == 0 {
		return ExportRequest{}, errors.New("privacy package payload is required")
	}
	in := ExportRequest{ID: identifier, Kind: kind, ObjectID: strings.TrimSpace(objectID), Format: format, Status: ExportPending, RequestedBy: actor, Reason: strings.TrimSpace(reason), Criteria: criteria, TemplateVersion: templateVersion, WatermarkText: strings.TrimSpace(options.WatermarkText), FrozenPayload: frozenPayload, CreatedAt: now, UpdatedAt: now, Version: 1}
	auditInput := audit.Input{ActorType: "USER", ActorID: actor, Action: "EXPORT_REQUESTED", ObjectType: "EXPORT_REQUEST", ObjectID: in.ID, Outcome: "SUCCESS", Sensitivity: "HIGH", After: map[string]any{"kind": kind, "format": format, "objectId": objectID, "templateVersion": templateVersion}, Reason: reason, CorrelationID: correlation, OccurredAt: now}
	var out ExportRequest
	atomicAudit := false
	if atomicRepo, ok := s.Repo.(ExportAtomicAuditRepository); ok {
		out, err = atomicRepo.CreateExportWithAudit(ctx, in, auditInput)
		atomicAudit = true
	} else {
		out, err = s.Repo.CreateExport(ctx, in)
	}
	if err == nil && s.Audit != nil && !atomicAudit {
		_, err = s.Audit.Record(ctx, auditInput)
	}
	return out, err
}

func (s *Service) GetExport(ctx context.Context, identifier string) (ExportRequest, error) {
	return s.Repo.GetExport(ctx, strings.TrimSpace(identifier))
}

func (s *Service) ListExports(ctx context.Context, query ExportQuery) (ExportPage, error) {
	return s.Repo.ListExports(ctx, query)
}

func (s *Service) DecideExport(ctx context.Context, identifier string, expected int64, approve bool, reason, actor, correlation string) (ExportRequest, error) {
	current, err := s.Repo.GetExport(ctx, identifier)
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
		current.AsOf = &now
		if strings.TrimSpace(current.WatermarkText) == "" {
			current.WatermarkText = "CONFIDENTIAL • export " + current.ID
		}
		switch current.Kind {
		case "CAMPAIGN_REPORT":
			report, reportErr := s.CampaignReport(ctx, current.ObjectID)
			if reportErr != nil {
				return ExportRequest{}, reportErr
			}
			payload, marshalErr := json.Marshal(report)
			if marshalErr != nil {
				return ExportRequest{}, marshalErr
			}
			current.FrozenPayload = payload
		case "AUDIT_LOG":
			if s.AuditRepository == nil {
				return ExportRequest{}, errors.New("audit repository unavailable")
			}
			sequence, hash, headErr := s.AuditRepository.Head(ctx)
			if headErr != nil {
				return ExportRequest{}, headErr
			}
			current.AuditHeadSequence = sequence
			current.AuditHeadHash = hash
		case "PRIVACY_PACKAGE":
			// The privacy service injects an encrypted, immutable package into
			// FrozenPayload before approval. Empty payloads fail closed.
			if len(current.FrozenPayload) == 0 {
				return ExportRequest{}, errors.New("privacy package is not frozen")
			}
		}
	} else {
		if strings.TrimSpace(reason) == "" {
			return ExportRequest{}, ErrInvalid
		}
		current.Status = ExportRejected
		current.RejectionReason = strings.TrimSpace(reason)
	}
	action := "EXPORT_REJECTED"
	if approve {
		action = "EXPORT_APPROVED"
	}
	auditInput := audit.Input{ActorType: "USER", ActorID: actor, Action: action, ObjectType: "EXPORT_REQUEST", ObjectID: identifier, Outcome: "SUCCESS", Sensitivity: "HIGH", After: map[string]any{"status": current.Status, "asOf": current.AsOf, "auditHeadSequence": current.AuditHeadSequence}, Reason: reason, CorrelationID: correlation, OccurredAt: now}
	var out ExportRequest
	atomicAudit := false
	if atomicRepo, ok := s.Repo.(ExportAtomicAuditRepository); ok {
		out, err = atomicRepo.UpdateExportWithAudit(ctx, current, expected, auditInput)
		atomicAudit = true
	} else {
		out, err = s.Repo.UpdateExport(ctx, current, expected)
	}
	if err == nil && s.Audit != nil && !atomicAudit {
		_, err = s.Audit.Record(ctx, auditInput)
	}
	return out, err
}

func (s *Service) AuthorizeDownload(ctx context.Context, identifier, actor, requestID string, ttl time.Duration) (DownloadAuthorization, error) {
	current, err := s.Repo.GetExport(ctx, identifier)
	if err != nil {
		return DownloadAuthorization{}, err
	}
	now := s.now()
	if current.Status != ExportReady || current.RevokedAt != nil || current.ExpiresAt == nil || !current.ExpiresAt.After(now) {
		return DownloadAuthorization{}, ErrConflict
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if ttl > 15*time.Minute {
		return DownloadAuthorization{}, ErrInvalid
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return DownloadAuthorization{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	grantID, err := id.New()
	if err != nil {
		return DownloadAuthorization{}, err
	}
	grant := DownloadGrant{ID: grantID, ExportID: current.ID, ActorID: strings.TrimSpace(actor), TokenHash: hex.EncodeToString(digest[:]), RequestID: strings.TrimSpace(requestID), ExpiresAt: now.Add(ttl), CreatedAt: now}
	auditInput := audit.Input{ActorType: "USER", ActorID: actor, Action: "EXPORT_DOWNLOAD_AUTHORISED", ObjectType: "EXPORT_REQUEST", ObjectID: current.ID, Outcome: "SUCCESS", Sensitivity: "HIGH", After: map[string]any{"grantId": grant.ID, "expiresAt": grant.ExpiresAt}, CorrelationID: requestID, OccurredAt: now}
	var stored DownloadGrant
	atomicAudit := false
	if atomicRepo, ok := s.Repo.(DownloadAtomicAuditRepository); ok {
		stored, err = atomicRepo.CreateDownloadGrantWithAudit(ctx, grant, auditInput)
		atomicAudit = true
	} else {
		stored, err = s.Repo.CreateDownloadGrant(ctx, grant)
	}
	if err != nil {
		return DownloadAuthorization{}, err
	}
	if s.Audit != nil && !atomicAudit {
		_, err = s.Audit.Record(ctx, auditInput)
		if err != nil {
			return DownloadAuthorization{}, err
		}
	}
	return DownloadAuthorization{Grant: stored, Token: token}, nil
}

func (s *Service) ConsumeDownload(ctx context.Context, identifier, token, actor, requestID string) (ExportRequest, DownloadGrant, error) {
	digest := sha256.Sum256([]byte(strings.TrimSpace(token)))
	now := s.now()
	identifier, actor = strings.TrimSpace(identifier), strings.TrimSpace(actor)
	auditInput := audit.Input{ActorType: "USER", ActorID: actor, Action: "EXPORT_DOWNLOAD_AUTHENTICATED", ObjectType: "EXPORT_REQUEST", ObjectID: identifier, Outcome: "SUCCESS", Sensitivity: "HIGH", CorrelationID: requestID, OccurredAt: now}
	var export ExportRequest
	var grant DownloadGrant
	var err error
	atomicAudit := false
	if atomicRepo, ok := s.Repo.(DownloadAtomicAuditRepository); ok {
		export, grant, err = atomicRepo.ConsumeDownloadGrantWithAudit(ctx, identifier, hex.EncodeToString(digest[:]), actor, now, auditInput)
		atomicAudit = true
	} else {
		export, grant, err = s.Repo.ConsumeDownloadGrant(ctx, identifier, hex.EncodeToString(digest[:]), actor, now)
	}
	if err != nil {
		return ExportRequest{}, DownloadGrant{}, err
	}
	if s.Audit != nil && !atomicAudit {
		auditInput.After = map[string]any{"grantId": grant.ID, "sha256": export.SHA256, "sizeBytes": export.SizeBytes}
		if _, auditErr := s.Audit.Record(ctx, auditInput); auditErr != nil {
			return ExportRequest{}, DownloadGrant{}, auditErr
		}
	}
	return export, grant, nil
}

func (s *Service) RecordDownloadOutcome(ctx context.Context, identifier, actor, requestID, outcome, reasonCode string, detail map[string]any) error {
	if s == nil || s.Audit == nil {
		return nil
	}
	outcome = strings.ToUpper(strings.TrimSpace(outcome))
	if outcome != "SUCCESS" && outcome != "FAILURE" {
		return ErrInvalid
	}
	action := "EXPORT_DOWNLOAD_COMPLETED"
	if outcome == "FAILURE" {
		action = "EXPORT_DOWNLOAD_FAILED"
	}
	_, err := s.Audit.Record(ctx, audit.Input{
		ActorType: "USER", ActorID: strings.TrimSpace(actor), Action: action,
		ObjectType: "EXPORT_REQUEST", ObjectID: strings.TrimSpace(identifier),
		Outcome: outcome, Sensitivity: "HIGH", After: detail,
		ReasonCode: strings.TrimSpace(reasonCode), CorrelationID: strings.TrimSpace(requestID), OccurredAt: s.now(),
	})
	return err
}

func (s *Service) RevokeExport(ctx context.Context, identifier string, expected int64, reason, actor, correlation string) (ExportRequest, error) {
	current, err := s.Repo.GetExport(ctx, identifier)
	if err != nil {
		return ExportRequest{}, err
	}
	if current.Status == ExportExpired || current.Status == ExportRejected || current.Status == ExportRevoked {
		return ExportRequest{}, ErrConflict
	}
	if len(strings.TrimSpace(reason)) < 8 {
		return ExportRequest{}, ErrInvalid
	}
	now := s.now()
	current.Status = ExportRevoked
	current.RevokedAt = &now
	current.RevokedBy = actor
	current.RevocationReason = strings.TrimSpace(reason)
	current.UpdatedAt = now
	auditInput := audit.Input{ActorType: "USER", ActorID: actor, Action: "EXPORT_REVOKED", ObjectType: "EXPORT_REQUEST", ObjectID: identifier, Outcome: "SUCCESS", Sensitivity: "HIGH", After: map[string]any{"status": current.Status}, Reason: reason, CorrelationID: correlation, OccurredAt: now}
	var out ExportRequest
	atomicAudit := false
	if atomicRepo, ok := s.Repo.(ExportAtomicAuditRepository); ok {
		out, err = atomicRepo.RevokeExportWithAudit(ctx, current, expected, now, auditInput)
		atomicAudit = true
	} else {
		out, err = s.Repo.UpdateExport(ctx, current, expected)
		if err == nil {
			err = s.Repo.RevokeDownloadGrants(ctx, identifier, now)
		}
	}
	if err != nil {
		return ExportRequest{}, err
	}
	if s.Audit != nil && !atomicAudit {
		_, err = s.Audit.Record(ctx, auditInput)
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
	auditInput := audit.Input{ActorType: "USER", ActorID: actor, Action: "DELIVERY_EXCEPTION_RESOLVED", ObjectType: "CAMPAIGN_RECIPIENT", ObjectID: recipientID, Outcome: "SUCCESS", Sensitivity: "HIGH", After: map[string]any{"action": action, "evidenceRef": evidenceRef, "resultStatus": result.Status}, Reason: reason, CorrelationID: correlation, OccurredAt: now}
	var atomicAudit bool
	writer := func(writeCtx context.Context, exec delivery.TransactionExecer) error {
		_, writeErr := audit.EnqueueTx(writeCtx, exec, auditInput)
		return writeErr
	}
	result, atomicAudit, err = s.Deliveries.ResolveReconciliationWithEvidence(ctx, recipientID, result.Status, actor, string(action), evidenceRef, reason, now, writer)
	if err != nil {
		return DeliveryResolution{}, err
	}
	resolution := DeliveryResolution{RecipientID: recipientID, Action: action, EvidenceRef: evidenceRef, Reason: reason, ActorID: actor, ResolvedAt: now, ResultStatus: result.Status}
	if s.Audit != nil && !atomicAudit {
		_, err = s.Audit.Record(ctx, auditInput)
		if err != nil {
			return DeliveryResolution{}, err
		}
	}
	return resolution, nil
}
