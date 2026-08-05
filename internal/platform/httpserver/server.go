package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/audience/importer"
	"campaign-platform/internal/audience/materialisation"
	"campaign-platform/internal/campaign"
	"campaign-platform/internal/commercial"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/delivery"
	"campaign-platform/internal/execution"
	"campaign-platform/internal/gateway"
	"campaign-platform/internal/geography"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/inbound"
	"campaign-platform/internal/jobs"
	"campaign-platform/internal/message"
	"campaign-platform/internal/operations"
	"campaign-platform/internal/orchestration"
	"campaign-platform/internal/organisation"
	"campaign-platform/internal/segment"
	"campaign-platform/internal/sender"
	sharedcrypto "campaign-platform/internal/shared/crypto"
	"campaign-platform/internal/shared/httpx"
	"campaign-platform/internal/shared/id"
	"campaign-platform/internal/storage"
)

type ReadinessCheck struct {
	Name  string
	Check func(context.Context) error
}

type Dependencies struct {
	Registry                 *audiencefilter.Registry
	FilterDefinitions        *audiencefilter.AdministrationService
	Compiler                 *cohort.Compiler
	Cohorts                  *cohort.ExecutionService
	Organisations            *organisation.Service
	OrganisationPolicies     *organisation.PolicyAdministration
	ConsentReviews           *consent.Service
	ConsentLedger            *consent.LedgerService
	OptOutProcessor          *consent.OptOutProcessor
	OptOutPolicies           *consent.OptOutPolicyAdministration
	InboundReplies           *inbound.Service
	InboundRetentionPolicies *inbound.RetentionPolicyAdministration
	InboundRotation          *inbound.RotationService
	Campaigns                *campaign.Service
	Commercial               *commercial.Service
	Geography                *geography.Catalogue
	MaxImportPreviewRows     int
	Identity                 *identity.Service
	IdentityAdministration   *identity.AdministrationService
	SecureCookies            bool
	MSISDNProtector          *sharedcrypto.MSISDNProtector
	Messages                 *message.Service
	Snapshots                *segment.Service
	SegmentDefinitions       *segment.DefinitionService
	AudienceMaterialisations *materialisation.MaterialisationService
	Releases                 *orchestration.ReleaseService
	SenderGovernance         *sender.GovernanceService
	DeliveryMetrics          *delivery.MetricsService
	Execution                *execution.Coordinator
	JobOperations            *jobs.AdministrationService
	Operations               *operations.Service
	AudienceImports          *importer.ImportService
	AudienceConflicts        *importer.ConflictService
	AudienceReconciliation   *importer.ReconciliationService
	AudienceSourceTrust      *importer.SourceTrustService
	AudienceImportIntake     *importer.IntakeService
	MaxImportFileBytes       int64
	DeliveryEvents           *delivery.Service
	GatewayCallbackSecret    []byte
	GatewayCallbackMaxSkew   time.Duration
	GatewayCallbackMaxBody   int64
	MediaObjects             storage.ObjectStore
	MediaDownloadSecret      []byte
	ReadinessChecks          []ReadinessCheck
	NetworkPolicy            NetworkPolicy
}

type Server struct {
	logger  *slog.Logger
	deps    Dependencies
	started time.Time
}

func New(logger *slog.Logger, deps Dependencies) *Server {
	if deps.MaxImportPreviewRows <= 0 {
		deps.MaxImportPreviewRows = 100_000
	}
	if deps.GatewayCallbackMaxSkew <= 0 {
		deps.GatewayCallbackMaxSkew = 5 * time.Minute
	}
	if deps.GatewayCallbackMaxBody <= 0 {
		deps.GatewayCallbackMaxBody = 1 << 20
	}
	if deps.MaxImportFileBytes <= 0 {
		deps.MaxImportFileBytes = 512 << 20
	}
	return &Server{logger: logger, deps: deps, started: time.Now().UTC()}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/internal/gateway/events", s.ingestGatewayEvent)
	mux.HandleFunc("POST /api/v1/internal/gateway/inbound", s.ingestGatewayInboundMessage)
	mux.HandleFunc("GET /api/v1/internal/media", s.downloadSignedMedia)
	mux.HandleFunc("POST /api/v1/auth/mfa/verify", s.verifyMFA)
	mux.Handle("POST /api/v1/auth/step-up", s.require("", s.stepUp))
	mux.Handle("GET /api/v1/auth/sessions", s.require("", s.activeSessions))
	mux.Handle("POST /api/v1/auth/sessions/revoke-all", s.require("", s.revokeAllSessions))
	mux.Handle("POST /api/v1/auth/logout", s.require("", s.logout))
	mux.Handle("GET /api/v1/auth/me", s.require("", s.me))
	mux.Handle("GET /api/v1/filter-definitions", s.require("audience.read", s.listFilterDefinitions))
	mux.Handle("GET /api/v1/admin/users", s.require("identity.admin", s.listInternalUsers))
	mux.Handle("POST /api/v1/admin/users", s.require("identity.admin", s.createInternalUser))
	mux.Handle("PUT /api/v1/admin/users/{id}", s.require("identity.admin", s.updateInternalUser))
	mux.Handle("POST /api/v1/admin/users/{id}/reset-credentials", s.require("identity.admin", s.resetInternalUserCredentials))
	mux.Handle("POST /api/v1/admin/users/{id}/unlock", s.require("identity.admin", s.unlockInternalUser))
	mux.Handle("POST /api/v1/admin/users/{id}/revoke-sessions", s.require("identity.admin", s.revokeInternalUserSessions))
	mux.Handle("GET /api/v1/admin/opt-out-policies", s.require("configuration.write", s.listOptOutPolicies))
	mux.Handle("POST /api/v1/admin/opt-out-policies", s.require("configuration.write", s.createOptOutPolicy))
	mux.Handle("POST /api/v1/admin/opt-out-policies/{id}/submit", s.require("configuration.write", s.submitOptOutPolicy))
	mux.Handle("POST /api/v1/admin/opt-out-policies/{id}/decision", s.require("configuration.approve", s.decideOptOutPolicy))
	mux.Handle("GET /api/v1/admin/filter-definitions", s.require("configuration.write", s.listAllFilterDefinitions))
	mux.Handle("POST /api/v1/admin/filter-definitions", s.require("configuration.write", s.createFilterDefinition))
	mux.Handle("PUT /api/v1/admin/filter-definitions/{code}", s.require("configuration.write", s.updateFilterDefinition))
	mux.Handle("POST /api/v1/cohorts/validate", s.require("audience.read", s.validateCohort))
	mux.Handle("POST /api/v1/cohorts/compile", s.require("audience.read", s.compileCohort))
	mux.Handle("POST /api/v1/cohorts/estimate", s.require("audience.read", s.estimateCohort))
	mux.Handle("GET /api/v1/segments", s.require("audience.read", s.listSegments))
	mux.Handle("POST /api/v1/segments", s.require("audience.write", s.createSegment))
	mux.Handle("GET /api/v1/segments/{id}", s.require("audience.read", s.getSegment))
	mux.Handle("PUT /api/v1/segments/{id}", s.require("audience.write", s.updateSegment))
	mux.Handle("POST /api/v1/segments/{id}/archive", s.require("audience.approve", s.archiveSegment))
	mux.Handle("GET /api/v1/segments/{id}/versions", s.require("audience.read", s.listSegmentVersions))
	mux.Handle("POST /api/v1/segments/{id}/clone", s.require("audience.write", s.cloneSegment))
	mux.Handle("GET /api/v1/segments/{id}/compare", s.require("audience.read", s.compareSegmentVersions))
	mux.Handle("GET /api/v1/geography/countries", s.require("audience.read", s.listCountries))
	mux.Handle("GET /api/v1/geography/areas", s.require("audience.read", s.listAreas))
	mux.Handle("GET /api/v1/organisations", s.require("organisation.read", s.listOrganisations))
	mux.Handle("POST /api/v1/organisations", s.require("organisation.write", s.createOrganisation))
	mux.Handle("GET /api/v1/organisations/{id}", s.require("organisation.read", s.getOrganisation))
	mux.Handle("PUT /api/v1/organisations/{id}", s.require("organisation.write", s.updateOrganisation))
	mux.Handle("POST /api/v1/organisations/{id}/status", s.require("organisation.approve", s.setOrganisationStatus))
	mux.Handle("GET /api/v1/organisations/{id}/events", s.require("organisation.read", s.listOrganisationEvents))
	mux.Handle("GET /api/v1/organisations/{id}/policies", s.require("organisation.read", s.listOrganisationPolicies))
	mux.Handle("POST /api/v1/organisations/{id}/policies", s.require("organisation.write", s.createOrganisationPolicy))
	mux.Handle("POST /api/v1/organisation-policies/{id}/submit", s.require("organisation.write", s.submitOrganisationPolicy))
	mux.Handle("POST /api/v1/organisation-policies/{id}/decision", s.require("organisation.approve", s.decideOrganisationPolicy))
	mux.Handle("GET /api/v1/consent-reviews", s.require("consent.read", s.listConsentReviews))
	mux.Handle("POST /api/v1/consent-reviews", s.require("consent.write", s.createConsentReview))
	mux.Handle("POST /api/v1/consent-reviews/{id}/decision", s.require("consent.review", s.decideConsentReview))
	mux.Handle("POST /api/v1/consent-grants", s.require("consent.write", s.createConsentGrant))
	mux.Handle("POST /api/v1/consent-grants/{id}/withdraw", s.require("consent.write", s.withdrawConsentGrant))
	mux.Handle("POST /api/v1/suppressions", s.require("consent.write", s.createSuppression))
	mux.Handle("POST /api/v1/suppressions/{id}/revoke", s.require("consent.review", s.revokeSuppression))
	mux.Handle("GET /api/v1/inbound-replies", s.require("inbound.read", s.listInboundReplies))
	mux.Handle("GET /api/v1/inbound-replies/{id}/content", s.require("inbound.content.read", s.getInboundReplyContent))
	mux.Handle("POST /api/v1/admin/inbound-replies/retention-sweep", s.require("privacy.admin", s.runInboundReplyRetentionSweep))
	mux.Handle("GET /api/v1/admin/inbound-retention-policies", s.require("configuration.write", s.listInboundRetentionPolicies))
	mux.Handle("POST /api/v1/admin/inbound-retention-policies", s.require("configuration.write", s.createInboundRetentionPolicy))
	mux.Handle("POST /api/v1/admin/inbound-retention-policies/{id}/submit", s.require("configuration.write", s.submitInboundRetentionPolicy))
	mux.Handle("POST /api/v1/admin/inbound-retention-policies/{id}/decision", s.require("configuration.approve", s.decideInboundRetentionPolicy))
	mux.Handle("POST /api/v1/admin/inbound-replies/reencrypt", s.require("security.admin", s.reencryptInboundContent))
	mux.Handle("GET /api/v1/admin/inbound-reencryption-runs", s.require("security.admin", s.listInboundReencryptionRuns))
	mux.Handle("GET /api/v1/admin/inbound-reencryption-runs/{id}", s.require("security.admin", s.getInboundReencryptionRun))
	mux.Handle("POST /api/v1/inbound-replies/{id}/review", s.require("inbound.review", s.reviewInboundReply))
	mux.Handle("POST /api/v1/inbound-replies/{id}/legal-hold", s.require("privacy.admin", s.setInboundReplyLegalHold))
	mux.Handle("GET /api/v1/consent-events", s.require("consent.read", s.listConsentEvents))
	mux.Handle("POST /api/v1/audience-imports/preview", s.require("audience.write", s.previewAudienceImport))
	mux.Handle("POST /api/v1/audience-imports", s.require("audience.write", s.intakeAudienceImport))
	mux.Handle("GET /api/v1/audience-imports/{id}", s.require("audience.read", s.getAudienceImport))
	mux.Handle("POST /api/v1/audience-imports/{id}/approve", s.require("audience.approve", s.approveAudienceImport))
	mux.Handle("GET /api/v1/audience-imports/{id}/conflicts", s.require("audience.read", s.listAudienceImportConflicts))
	mux.Handle("GET /api/v1/audience-imports/{id}/reconciliation", s.require("audience.read", s.getAudienceImportReconciliation))
	mux.Handle("POST /api/v1/audience-imports/{id}/reconciliation", s.require("audience.approve", s.closeAudienceImportReconciliation))
	mux.Handle("POST /api/v1/audience-import-conflicts/{id}/resolve", s.require("audience.approve", s.resolveAudienceImportConflict))
	mux.Handle("POST /api/v1/audience-import-conflicts/batch-resolve", s.require("audience.approve", s.resolveAudienceImportConflictsBatch))
	mux.Handle("GET /api/v1/organisations/{id}/audience-source-trust", s.require("audience.read", s.listAudienceSourceTrust))
	mux.Handle("PUT /api/v1/organisations/{id}/audience-source-trust/{source}", s.require("audience.write", s.upsertAudienceSourceTrust))
	mux.Handle("GET /api/v1/commercial-records", s.require("finance.read", s.listCommercialRecords))
	mux.Handle("POST /api/v1/campaigns/{id}/commercial-record", s.require("finance.write", s.createCommercialRecord))
	mux.Handle("POST /api/v1/commercial-records/{id}/submit", s.require("finance.write", s.submitCommercialRecord))
	mux.Handle("POST /api/v1/commercial-records/{id}/decision", s.require("finance.approve", s.decideCommercialRecord))
	mux.Handle("POST /api/v1/commercial-records/{id}/revoke", s.require("finance.approve", s.revokeCommercialRecord))
	mux.Handle("GET /api/v1/campaigns", s.require("campaign.read", s.listCampaigns))
	mux.Handle("POST /api/v1/campaigns", s.require("campaign.write", s.createCampaign))
	mux.Handle("POST /api/v1/campaigns/{id}/transition", s.require("campaign.write", s.transitionCampaign))
	mux.Handle("POST /api/v1/campaigns/{id}/material-amendment", s.require("campaign.write", s.amendCampaignMaterial))
	mux.Handle("GET /api/v1/campaigns/{id}/material-changes", s.require("campaign.read", s.listCampaignMaterialChanges))
	mux.Handle("GET /api/v1/campaigns/{id}/message-versions", s.require("campaign.read", s.listMessageVersions))
	mux.Handle("POST /api/v1/campaigns/{id}/message-versions", s.require("campaign.write", s.createMessageVersion))
	mux.Handle("POST /api/v1/message-versions/{id}/approve", s.require("campaign.approve", s.approveMessageVersion))
	mux.Handle("POST /api/v1/campaigns/{id}/audience-snapshots", s.require("audience.write", s.createAudienceSnapshot))
	mux.Handle("POST /api/v1/campaigns/{id}/audience-snapshots/materialise", s.require("audience.write", s.materialiseAudienceSnapshot))
	mux.Handle("POST /api/v1/campaigns/{id}/audience-materialisations", s.require("audience.write", s.scheduleAudienceMaterialisation))
	mux.Handle("GET /api/v1/campaigns/{id}/audience-materialisations", s.require("audience.read", s.listAudienceMaterialisations))
	mux.Handle("GET /api/v1/audience-materialisations/{id}", s.require("audience.read", s.getAudienceMaterialisation))
	mux.Handle("POST /api/v1/audience-materialisations/{id}/cancel", s.require("audience.approve", s.cancelAudienceMaterialisation))
	mux.Handle("POST /api/v1/campaigns/{id}/release", s.require("campaign.operate", s.releaseCampaignAudience))
	mux.Handle("GET /api/v1/campaigns/{id}/metrics", s.require("campaign.read", s.getCampaignMetrics))
	mux.Handle("GET /api/v1/campaigns/{id}/execution-plan", s.require("campaign.read", s.getCampaignExecutionPlan))
	mux.Handle("GET /api/v1/operations/dashboard", s.require("operations.read", s.operationsDashboard))
	mux.Handle("GET /api/v1/operations/incidents", s.require("operations.read", s.listOperationsIncidents))
	mux.Handle("GET /api/v1/operations/audit-events", s.require("audit.read", s.searchAuditEvents))
	mux.Handle("GET /api/v1/operations/delivery-exceptions", s.require("operations.read", s.listDeliveryExceptions))
	mux.Handle("GET /api/v1/operations/jobs", s.require("operations.read", s.listOperationalJobs))
	mux.Handle("GET /api/v1/operations/jobs/summary", s.require("operations.read", s.getOperationalJobSummary))
	mux.Handle("GET /api/v1/operations/jobs/{id}", s.require("operations.read", s.getOperationalJob))
	mux.Handle("GET /api/v1/operations/jobs/{id}/events", s.require("operations.read", s.listOperationalJobEvents))
	mux.Handle("POST /api/v1/operations/jobs/{id}/retry", s.require("operations.write", s.retryOperationalJob))
	mux.Handle("POST /api/v1/operations/jobs/{id}/cancel", s.require("operations.write", s.cancelOperationalJob))
	mux.Handle("POST /api/v1/operations/incidents", s.require("operations.write", s.createOperationsIncident))
	mux.Handle("PUT /api/v1/operations/incidents/{id}", s.require("operations.write", s.updateOperationsIncident))
	mux.Handle("GET /api/v1/campaigns/{id}/report", s.require("report.read", s.getCampaignReport))
	mux.Handle("POST /api/v1/exports", s.require("export.request", s.requestExport))
	mux.Handle("POST /api/v1/exports/{id}/decision", s.require("export.approve", s.decideExport))
	mux.Handle("POST /api/v1/campaigns/{id}/execution/{action}", s.require("campaign.operate", s.executeCampaignAction))
	mux.Handle("GET /api/v1/campaigns/{id}/inbound-metrics", s.require("campaign.read", s.getCampaignInboundMetrics))
	mux.Handle("GET /api/v1/audience-snapshots/{id}", s.require("audience.read", s.getAudienceSnapshot))
	mux.Handle("GET /api/v1/audience-snapshots/overlap", s.require("audience.read", s.getAudienceSnapshotOverlap))
	mux.Handle("GET /api/v1/sender-pools", s.require("sender.read", s.listSenderPools))
	mux.Handle("POST /api/v1/sender-pools", s.require("sender.admin", s.createSenderPool))
	mux.Handle("PUT /api/v1/sender-pools/{id}", s.require("sender.admin", s.updateSenderPool))
	mux.Handle("GET /api/v1/sender-pools/{id}/capacity", s.require("sender.read", s.getSenderPoolCapacity))
	mux.Handle("GET /api/v1/sender-nodes", s.require("sender.read", s.listSenderNodes))
	mux.Handle("POST /api/v1/sender-nodes", s.require("sender.admin", s.registerSenderNode))
	mux.Handle("POST /api/v1/internal/sender-nodes/{id}/heartbeat", s.require("sender.operate", s.heartbeatSenderNode))
	mux.Handle("GET /api/v1/sender-sessions", s.require("sender.read", s.listSenderSessions))
	mux.Handle("POST /api/v1/sender-sessions", s.require("sender.admin", s.registerSenderSession))
	mux.Handle("POST /api/v1/sender-sessions/{id}/transition", s.require("sender.operate", s.transitionSenderSession))
	mux.Handle("POST /api/v1/internal/sender-sessions/{id}/heartbeat", s.require("sender.operate", s.heartbeatSenderSession))

	var handler http.Handler = mux
	handler = s.recoverPanic(handler)
	handler = s.securityHeaders(handler)
	handler = s.networkAdmission(handler)
	handler = s.requestID(handler)
	handler = s.requestLogging(handler)
	return handler
}

func (s *Server) ingestGatewayInboundMessage(w http.ResponseWriter, r *http.Request) {
	if s.deps.OptOutProcessor == nil || len(s.deps.GatewayCallbackSecret) < 32 {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "GATEWAY_INBOUND_UNAVAILABLE", "Gateway inbound processing is not configured.", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.deps.GatewayCallbackMaxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpx.WriteError(w, r, http.StatusRequestEntityTooLarge, "GATEWAY_INBOUND_TOO_LARGE", "The inbound message exceeded the permitted size.", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusBadRequest, "GATEWAY_INBOUND_UNREADABLE", "The inbound message body could not be read.", nil)
		return
	}
	now := time.Now().UTC()
	if err := gateway.VerifyCallback(s.deps.GatewayCallbackSecret, r.Header.Get(gateway.TimestampHeader), r.Header.Get(gateway.SignatureHeader), body, now, s.deps.GatewayCallbackMaxSkew); err != nil {
		httpx.WriteError(w, r, http.StatusUnauthorized, "GATEWAY_SIGNATURE_INVALID", "The gateway callback could not be authenticated.", nil)
		return
	}
	event, err := gateway.DecodeInboundMessage(body)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "GATEWAY_INBOUND_INVALID", "The inbound message contract is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	if err := event.Validate(now, s.deps.GatewayCallbackMaxSkew); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "GATEWAY_INBOUND_REJECTED", "The inbound message evidence is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	result, err := s.deps.OptOutProcessor.Process(r.Context(), event.ClientReference, event.EventID, event.MessageText, "gateway:"+event.EventID)
	if errors.Is(err, delivery.ErrRecipientNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_RECIPIENT_NOT_FOUND", "The inbound message referenced an unknown campaign recipient.", nil)
		return
	}
	if errors.Is(err, consent.ErrLedgerReplayConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "GATEWAY_INBOUND_REPLAY_CONFLICT", "The inbound event identifier was reused with different evidence.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"accepted": true, "recognisedOptOut": result.Recognised, "replayed": result.Replayed,
		"contactId": result.ContactID, "suppressionId": result.Suppression.ID,
	})
}

func (s *Server) ingestGatewayEvent(w http.ResponseWriter, r *http.Request) {
	if s.deps.DeliveryEvents == nil || len(s.deps.GatewayCallbackSecret) < 32 {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "GATEWAY_CALLBACK_UNAVAILABLE", "Gateway callback ingestion is not configured.", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.deps.GatewayCallbackMaxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpx.WriteError(w, r, http.StatusRequestEntityTooLarge, "GATEWAY_EVENT_TOO_LARGE", "The gateway event exceeded the permitted size.", nil)
			return
		}
		httpx.WriteError(w, r, http.StatusBadRequest, "GATEWAY_EVENT_UNREADABLE", "The gateway event body could not be read.", nil)
		return
	}
	now := time.Now().UTC()
	if err := gateway.VerifyCallback(s.deps.GatewayCallbackSecret, r.Header.Get(gateway.TimestampHeader), r.Header.Get(gateway.SignatureHeader), body, now, s.deps.GatewayCallbackMaxSkew); err != nil {
		// Deliberately do not disclose whether the timestamp or MAC was incorrect.
		httpx.WriteError(w, r, http.StatusUnauthorized, "GATEWAY_SIGNATURE_INVALID", "The gateway callback could not be authenticated.", nil)
		return
	}
	event, err := gateway.DecodeCallback(body)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "GATEWAY_EVENT_INVALID", "The gateway event contract is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	if err := event.Validate(now, s.deps.GatewayCallbackMaxSkew); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "GATEWAY_EVENT_REJECTED", "The gateway event evidence is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	recipientID := strings.TrimSpace(event.ClientReference)
	if recipientID == "" && strings.TrimSpace(event.ProviderMessageID) != "" {
		resolved, resolveErr := s.deps.DeliveryEvents.GetByProviderMessageID(r.Context(), event.ProviderMessageID)
		if resolveErr != nil {
			err = resolveErr
		} else {
			recipientID = resolved.ID
		}
	}
	var recipient delivery.Recipient
	var changed bool
	if err == nil {
		recipient, changed, err = s.deps.DeliveryEvents.ApplyEvent(r.Context(), recipientID, event.DeliveryEvent())
	}
	if errors.Is(err, delivery.ErrRecipientNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_RECIPIENT_NOT_FOUND", "The callback referenced an unknown campaign recipient.", nil)
		return
	}
	if errors.Is(err, delivery.ErrEventDedupMismatch) {
		httpx.WriteError(w, r, http.StatusConflict, "GATEWAY_EVENT_REPLAY_CONFLICT", "The provider event identifier was reused with different evidence.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"accepted": true, "replayed": !changed, "recipientId": recipient.ID,
		"status": recipient.Status, "highestAcknowledgement": recipient.HighestAcknowledgement,
		"reconciliationRequired": recipient.ReconciliationRequired,
	})
}

func (s *Server) downloadSignedMedia(w http.ResponseWriter, r *http.Request) {
	if s.deps.MediaObjects == nil || len(s.deps.MediaDownloadSecret) < 32 {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "MEDIA_DOWNLOAD_UNAVAILABLE", "Media delivery is not configured.", nil)
		return
	}
	query := r.URL.Query()
	key := query.Get("key")
	if err := storage.VerifySignedURL(s.deps.MediaDownloadSecret, key, query.Get("expires"), query.Get("signature"), time.Now().UTC()); err != nil {
		httpx.WriteError(w, r, http.StatusUnauthorized, "MEDIA_DOWNLOAD_INVALID", "The media link is invalid or expired.", nil)
		return
	}
	object, metadata, err := s.deps.MediaObjects.Open(r.Context(), key)
	if errors.Is(err, storage.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "MEDIA_NOT_FOUND", "The requested media object was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	defer object.Close()
	contentType := mime.TypeByExtension(filepath.Ext(key))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(metadata.Size, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `inline; filename="`+strings.ReplaceAll(filepath.Base(key), `"`, "")+`"`)
	http.ServeContent(w, r, filepath.Base(key), metadata.CreatedAt, object)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "startedAt": s.started})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{
		"filterRegistry":       readiness(s.deps.Registry != nil && s.deps.FilterDefinitions != nil),
		"cohortCompiler":       readiness(s.deps.Compiler != nil),
		"organisationService":  readiness(s.deps.Organisations != nil),
		"consentReviewService": readiness(s.deps.ConsentReviews != nil),
		"consentLedgerService": readiness(s.deps.ConsentLedger != nil),
		"identityService":      readiness(s.deps.Identity != nil),
	}
	ready := true
	for name, state := range checks {
		if state != "ok" {
			ready = false
		}
		checks[name] = state
	}
	for _, check := range s.deps.ReadinessChecks {
		if check.Name == "" || check.Check == nil {
			continue
		}
		if err := check.Check(r.Context()); err != nil {
			checks[check.Name] = "failed"
			ready = false
			continue
		}
		checks[check.Name] = "ok"
	}
	status := http.StatusOK
	label := "ready"
	if !ready {
		status = http.StatusServiceUnavailable
		label = "not_ready"
	}
	httpx.WriteJSON(w, status, map[string]any{"status": label, "checks": checks})
}

func (s *Server) listFilterDefinitions(w http.ResponseWriter, r *http.Request) {
	principal, _ := identity.PrincipalFromContext(r.Context())
	if s.deps.FilterDefinitions != nil {
		items, err := s.deps.FilterDefinitions.ListForPermissions(r.Context(), principal.User.HasPermission)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": s.deps.Registry.ListForPermissions(principal.User.HasPermission)})
}

func (s *Server) listAllFilterDefinitions(w http.ResponseWriter, r *http.Request) {
	if s.deps.FilterDefinitions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "FILTER_ADMIN_UNAVAILABLE", "Filter administration is unavailable.", nil)
		return
	}
	items, err := s.deps.FilterDefinitions.List(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) createFilterDefinition(w http.ResponseWriter, r *http.Request) {
	if s.deps.FilterDefinitions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "FILTER_ADMIN_UNAVAILABLE", "Filter administration is unavailable.", nil)
		return
	}
	var input audiencefilter.CreateDefinitionInput
	if err := httpx.DecodeJSON(w, r, 128<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The filter definition is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.ActorID = principal.User.ID
	item, err := s.deps.FilterDefinitions.Create(r.Context(), input)
	if err != nil {
		writeFilterDefinitionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}
func (s *Server) updateFilterDefinition(w http.ResponseWriter, r *http.Request) {
	if s.deps.FilterDefinitions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "FILTER_ADMIN_UNAVAILABLE", "Filter administration is unavailable.", nil)
		return
	}
	var input audiencefilter.UpdateDefinitionInput
	if err := httpx.DecodeJSON(w, r, 128<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The filter definition update is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.ActorID = principal.User.ID
	item, err := s.deps.FilterDefinitions.Update(r.Context(), r.PathValue("code"), input)
	if err != nil {
		writeFilterDefinitionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func writeFilterDefinitionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, audiencefilter.ErrDefinitionNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "FILTER_DEFINITION_NOT_FOUND", "The filter definition was not found.", nil)
	case errors.Is(err, audiencefilter.ErrDefinitionConflict):
		httpx.WriteError(w, r, http.StatusConflict, "FILTER_DEFINITION_CONFLICT", "The filter definition changed; reload before updating.", nil)
	case errors.Is(err, audiencefilter.ErrDefinitionDuplicate):
		httpx.WriteError(w, r, http.StatusConflict, "FILTER_DEFINITION_DUPLICATE", "The filter definition code already exists.", nil)
	case errors.Is(err, audiencefilter.ErrDefinitionInvalid):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "FILTER_DEFINITION_INVALID", "The filter definition failed governance validation.", map[string]any{"detail": err.Error()})
	default:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "FILTER_DEFINITION_REJECTED", "The filter definition could not be saved.", map[string]any{"detail": err.Error()})
	}
}

func (s *Server) validateCohort(w http.ResponseWriter, r *http.Request) {
	if s.deps.FilterDefinitions != nil {
		if err := s.deps.FilterDefinitions.Refresh(r.Context()); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	var group audiencefilter.Group
	if err := httpx.DecodeJSON(w, r, 1<<20, &group); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The cohort definition is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if err := group.ValidateForPermissions(s.deps.Registry, principal.User.HasPermission); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COHORT_VALIDATION_FAILED", "The cohort definition failed validation.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"valid": true, "definition": group})
}

type compileCohortRequest struct {
	Definition     audiencefilter.Group `json:"definition"`
	OrganisationID string               `json:"organisationId"`
	PurposeID      string               `json:"purposeId"`
	Channel        string               `json:"channel"`
	AsOf           *time.Time           `json:"asOf"`
}

func (s *Server) compileCohort(w http.ResponseWriter, r *http.Request) {
	if s.deps.FilterDefinitions != nil {
		if err := s.deps.FilterDefinitions.Refresh(r.Context()); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	var input compileCohortRequest
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The cohort compile request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	asOf := time.Now().UTC()
	if input.AsOf != nil {
		asOf = input.AsOf.UTC()
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	compiled, err := s.deps.Compiler.CompileForPermissions(input.Definition, cohort.EligibilityContext{
		OrganisationID: input.OrganisationID, PurposeID: input.PurposeID, Channel: input.Channel, AsOf: asOf,
	}, principal.User.HasPermission)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COHORT_COMPILE_FAILED", "The cohort could not be compiled.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"valid": true, "compiled": compiled})
}

func (s *Server) estimateCohort(w http.ResponseWriter, r *http.Request) {
	if s.deps.Cohorts == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "COHORT_EXECUTION_UNAVAILABLE", "Cohort execution is unavailable.", nil)
		return
	}
	if s.deps.FilterDefinitions != nil {
		if err := s.deps.FilterDefinitions.Refresh(r.Context()); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	var input compileCohortRequest
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The cohort estimate request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	asOf := time.Now().UTC()
	if input.AsOf != nil {
		asOf = input.AsOf.UTC()
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	estimate, err := s.deps.Cohorts.Estimate(r.Context(), input.Definition, cohort.EligibilityContext{OrganisationID: input.OrganisationID, PurposeID: input.PurposeID, Channel: input.Channel, AsOf: asOf}, principal.User.HasPermission)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COHORT_ESTIMATE_FAILED", "The cohort could not be estimated.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, estimate)
}

func (s *Server) listSegments(w http.ResponseWriter, r *http.Request) {
	if s.deps.SegmentDefinitions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "SEGMENTS_UNAVAILABLE", "Saved segments are unavailable.", nil)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.deps.SegmentDefinitions.List(r.Context(), r.URL.Query().Get("organisationId"), limit)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) createSegment(w http.ResponseWriter, r *http.Request) {
	if s.deps.SegmentDefinitions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "SEGMENTS_UNAVAILABLE", "Saved segments are unavailable.", nil)
		return
	}
	var input segment.CreateDefinitionInput
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The segment request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.ActorID = principal.User.ID
	item, err := s.deps.SegmentDefinitions.Create(r.Context(), input, principal.User.HasPermission)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "SEGMENT_REJECTED", "The segment could not be created.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}
func (s *Server) getSegment(w http.ResponseWriter, r *http.Request) {
	if s.deps.SegmentDefinitions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "SEGMENTS_UNAVAILABLE", "Saved segments are unavailable.", nil)
		return
	}
	item, err := s.deps.SegmentDefinitions.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, segment.ErrDefinitionNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "SEGMENT_NOT_FOUND", "The segment was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (s *Server) updateSegment(w http.ResponseWriter, r *http.Request) {
	if s.deps.SegmentDefinitions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "SEGMENTS_UNAVAILABLE", "Saved segments are unavailable.", nil)
		return
	}
	var input segment.UpdateDefinitionInput
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The segment update is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.ActorID = principal.User.ID
	item, err := s.deps.SegmentDefinitions.Update(r.Context(), r.PathValue("id"), input, principal.User.HasPermission)
	writeSegmentResult(w, r, item, err)
}

type archiveSegmentRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason"`
}

func (s *Server) archiveSegment(w http.ResponseWriter, r *http.Request) {
	if s.deps.SegmentDefinitions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "SEGMENTS_UNAVAILABLE", "Saved segments are unavailable.", nil)
		return
	}
	var input archiveSegmentRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The archive request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required to archive a segment.", nil)
		return
	}
	item, err := s.deps.SegmentDefinitions.Archive(r.Context(), r.PathValue("id"), principal.User.ID, input.Reason, input.ExpectedVersion)
	writeSegmentResult(w, r, item, err)
}
func (s *Server) listSegmentVersions(w http.ResponseWriter, r *http.Request) {
	if s.deps.SegmentDefinitions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "SEGMENTS_UNAVAILABLE", "Saved segments are unavailable.", nil)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.deps.SegmentDefinitions.Versions(r.Context(), r.PathValue("id"), limit)
	if errors.Is(err, segment.ErrDefinitionNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "SEGMENT_NOT_FOUND", "The segment was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) cloneSegment(w http.ResponseWriter, r *http.Request) {
	if s.deps.SegmentDefinitions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "SEGMENTS_UNAVAILABLE", "Saved segments are unavailable.", nil)
		return
	}
	var input segment.CloneDefinitionInput
	if err := httpx.DecodeJSON(w, r, 128<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The segment clone request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.ActorID = principal.User.ID
	item, err := s.deps.SegmentDefinitions.Clone(r.Context(), r.PathValue("id"), input, principal.User.HasPermission)
	if errors.Is(err, segment.ErrDefinitionNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "SEGMENT_NOT_FOUND", "The source segment was not found.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "SEGMENT_CLONE_REJECTED", "The segment could not be cloned.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}

func (s *Server) compareSegmentVersions(w http.ResponseWriter, r *http.Request) {
	if s.deps.SegmentDefinitions == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "SEGMENTS_UNAVAILABLE", "Saved segments are unavailable.", nil)
		return
	}
	fromVersion, fromErr := strconv.ParseInt(r.URL.Query().Get("fromVersion"), 10, 64)
	toVersion, toErr := strconv.ParseInt(r.URL.Query().Get("toVersion"), 10, 64)
	if fromErr != nil || toErr != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "SEGMENT_VERSIONS_INVALID", "fromVersion and toVersion must be positive integers.", nil)
		return
	}
	result, err := s.deps.SegmentDefinitions.CompareVersions(r.Context(), r.PathValue("id"), fromVersion, toVersion)
	if errors.Is(err, segment.ErrDefinitionNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "SEGMENT_NOT_FOUND", "The segment was not found.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "SEGMENT_COMPARISON_REJECTED", "The segment versions could not be compared.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func writeSegmentResult(w http.ResponseWriter, r *http.Request, item segment.Definition, err error) {
	switch {
	case errors.Is(err, segment.ErrDefinitionNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "SEGMENT_NOT_FOUND", "The segment was not found.", nil)
	case errors.Is(err, segment.ErrDefinitionConflict):
		httpx.WriteError(w, r, http.StatusConflict, "SEGMENT_VERSION_CONFLICT", "The segment changed; reload before retrying.", nil)
	case errors.Is(err, segment.ErrDefinitionState):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "SEGMENT_STATE_INVALID", "The segment state does not allow this operation.", nil)
	case err != nil:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "SEGMENT_REJECTED", "The segment operation was rejected.", map[string]any{"detail": err.Error()})
	default:
		httpx.WriteJSON(w, http.StatusOK, item)
	}
}

func (s *Server) listCountries(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": s.deps.Geography.Countries()})
}

func (s *Server) listAreas(w http.ResponseWriter, r *http.Request) {
	level := 0
	if raw := r.URL.Query().Get("level"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 4 {
			httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_LEVEL", "Geography level must be between 1 and 4.", nil)
			return
		}
		level = parsed
	}
	items := s.deps.Geography.Areas(
		strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("country"))),
		strings.TrimSpace(r.URL.Query().Get("parent")),
		level,
	)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listOrganisations(w http.ResponseWriter, r *http.Request) {
	items, err := s.deps.Organisations.List(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createOrganisation(w http.ResponseWriter, r *http.Request) {
	var input organisation.CreateInput
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The organisation request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	entity, err := s.deps.Organisations.Create(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "ORGANISATION_INVALID", "The organisation could not be created.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, entity)
}

func (s *Server) getOrganisation(w http.ResponseWriter, r *http.Request) {
	entity, err := s.deps.Organisations.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, organisation.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "ORGANISATION_NOT_FOUND", "The organisation was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, entity)
}

func (s *Server) updateOrganisation(w http.ResponseWriter, r *http.Request) {
	var input organisation.UpdateInput
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The organisation update is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.ActorID = principal.User.ID
	entity, err := s.deps.Organisations.Update(r.Context(), r.PathValue("id"), input)
	if errors.Is(err, organisation.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "ORGANISATION_NOT_FOUND", "The organisation was not found.", nil)
		return
	}
	if errors.Is(err, organisation.ErrConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "ORGANISATION_VERSION_CONFLICT", "The organisation changed; reload before updating.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "ORGANISATION_UPDATE_INVALID", "The organisation update was rejected.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, entity)
}

func (s *Server) setOrganisationStatus(w http.ResponseWriter, r *http.Request) {
	var input organisation.StatusInput
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The organisation status request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.ActorID = principal.User.ID
	entity, err := s.deps.Organisations.SetStatus(r.Context(), r.PathValue("id"), input)
	if errors.Is(err, organisation.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "ORGANISATION_NOT_FOUND", "The organisation was not found.", nil)
		return
	}
	if errors.Is(err, organisation.ErrConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "ORGANISATION_VERSION_CONFLICT", "The organisation changed; reload before changing status.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "ORGANISATION_STATUS_INVALID", "The organisation status change was rejected.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, entity)
}

func (s *Server) listOrganisationEvents(w http.ResponseWriter, r *http.Request) {
	items, err := s.deps.Organisations.ListEvents(r.Context(), r.PathValue("id"))
	if errors.Is(err, organisation.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "ORGANISATION_NOT_FOUND", "The organisation was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listOrganisationPolicies(w http.ResponseWriter, r *http.Request) {
	if s.deps.OrganisationPolicies == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "ORGANISATION_POLICY_UNAVAILABLE", "Organisation policy administration is not configured.", nil)
		return
	}
	items, err := s.deps.OrganisationPolicies.List(r.Context(), r.PathValue("id"))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) createOrganisationPolicy(w http.ResponseWriter, r *http.Request) {
	if s.deps.OrganisationPolicies == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "ORGANISATION_POLICY_UNAVAILABLE", "Organisation policy administration is not configured.", nil)
		return
	}
	var input organisation.Policy
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The organisation policy request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.OrganisationID = r.PathValue("id")
	reason := input.Reason
	value, err := s.deps.OrganisationPolicies.CreateDraft(r.Context(), input, principal.User.ID, reason)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "ORGANISATION_POLICY_INVALID", "The organisation policy could not be created.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, value)
}
func (s *Server) submitOrganisationPolicy(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		Reason          string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The organisation policy submission is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	value, err := s.deps.OrganisationPolicies.Submit(r.Context(), r.PathValue("id"), input.ExpectedVersion, principal.User.ID, input.Reason)
	if errors.Is(err, organisation.ErrPolicyConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "ORGANISATION_POLICY_CONFLICT", "The organisation policy changed; reload before submitting.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "ORGANISATION_POLICY_INVALID", "The organisation policy submission was rejected.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
func (s *Server) decideOrganisationPolicy(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		Approve         bool   `json:"approve"`
		Reason          string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The organisation policy decision is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "MFA_STEP_UP_REQUIRED", "Recent MFA verification is required.", nil)
		return
	}
	value, err := s.deps.OrganisationPolicies.Decide(r.Context(), r.PathValue("id"), input.ExpectedVersion, input.Approve, principal.User.ID, input.Reason)
	if errors.Is(err, organisation.ErrPolicyConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "ORGANISATION_POLICY_CONFLICT", "The organisation policy changed; reload before deciding.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "ORGANISATION_POLICY_INVALID", "The organisation policy decision was rejected.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (s *Server) listCommercialRecords(w http.ResponseWriter, r *http.Request) {
	if s.deps.Commercial == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "COMMERCIAL_UNAVAILABLE", "Commercial governance is not configured.", nil)
		return
	}
	items, err := s.deps.Commercial.List(r.Context(), strings.TrimSpace(r.URL.Query().Get("organisationId")))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) createCommercialRecord(w http.ResponseWriter, r *http.Request) {
	if s.deps.Commercial == nil || s.deps.Campaigns == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "COMMERCIAL_UNAVAILABLE", "Commercial governance is not configured.", nil)
		return
	}
	var input commercial.Record
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The commercial record request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	campaignValue, err := s.deps.Campaigns.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.CampaignID = campaignValue.ID
	input.OrganisationID = campaignValue.OrganisationID
	value, err := s.deps.Commercial.CreateDraft(r.Context(), input, principal.User.ID, input.Reason)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COMMERCIAL_INVALID", "The commercial record could not be created.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, value)
}
func (s *Server) submitCommercialRecord(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		Reason          string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The commercial submission is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	value, err := s.deps.Commercial.Submit(r.Context(), r.PathValue("id"), input.ExpectedVersion, principal.User.ID, input.Reason)
	if errors.Is(err, commercial.ErrConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "COMMERCIAL_CONFLICT", "The commercial record changed; reload before submitting.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COMMERCIAL_INVALID", "The commercial submission was rejected.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
func (s *Server) decideCommercialRecord(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		Approve         bool   `json:"approve"`
		Reason          string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The commercial decision is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "MFA_STEP_UP_REQUIRED", "Recent MFA verification is required.", nil)
		return
	}
	value, err := s.deps.Commercial.Decide(r.Context(), r.PathValue("id"), input.ExpectedVersion, input.Approve, principal.User.ID, input.Reason)
	if errors.Is(err, commercial.ErrConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "COMMERCIAL_CONFLICT", "The commercial record changed; reload before deciding.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COMMERCIAL_INVALID", "The commercial decision was rejected.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
func (s *Server) revokeCommercialRecord(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		Reason          string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The commercial revocation is invalid.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "MFA_STEP_UP_REQUIRED", "Recent MFA verification is required.", nil)
		return
	}
	value, err := s.deps.Commercial.Revoke(r.Context(), r.PathValue("id"), input.ExpectedVersion, principal.User.ID, input.Reason)
	if errors.Is(err, commercial.ErrConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "COMMERCIAL_CONFLICT", "The commercial record changed; reload before revoking.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COMMERCIAL_INVALID", "The commercial revocation was rejected.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (s *Server) listConsentReviews(w http.ResponseWriter, r *http.Request) {
	items, err := s.deps.ConsentReviews.List(r.Context(), strings.TrimSpace(r.URL.Query().Get("organisationId")))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createConsentReview(w http.ResponseWriter, r *http.Request) {
	var input consent.CreateInput
	if err := httpx.DecodeJSON(w, r, 2<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The consent-review request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	entity, err := s.deps.ConsentReviews.Create(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CONSENT_REVIEW_INVALID", "The consent review could not be created.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, entity)
}

func (s *Server) decideConsentReview(w http.ResponseWriter, r *http.Request) {
	var input consent.DecisionInput
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The consent decision is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.ReviewerID = principal.User.ID
	entity, err := s.deps.ConsentReviews.Decide(r.Context(), r.PathValue("id"), input)
	if errors.Is(err, consent.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CONSENT_REVIEW_NOT_FOUND", "The consent review was not found.", nil)
		return
	}
	if errors.Is(err, consent.ErrConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "CONSENT_VERSION_CONFLICT", "The consent review changed; reload before deciding.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CONSENT_DECISION_INVALID", "The consent review decision was rejected.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, entity)
}

func (s *Server) listCampaigns(w http.ResponseWriter, r *http.Request) {
	items, err := s.deps.Campaigns.List(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createCampaign(w http.ResponseWriter, r *http.Request) {
	var input campaign.CreateInput
	if err := httpx.DecodeJSON(w, r, 2<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The campaign request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.CreatedBy = principal.User.ID
	review, err := s.deps.ConsentReviews.Get(r.Context(), input.ConsentReviewID)
	if errors.Is(err, consent.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CONSENT_REVIEW_NOT_FOUND", "The selected consent review does not exist.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if review.OrganisationID != input.OrganisationID {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CONSENT_ORGANISATION_MISMATCH", "The consent review does not belong to the selected organisation.", nil)
		return
	}
	entity, err := s.deps.Campaigns.Create(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CAMPAIGN_INVALID", "The campaign could not be created.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, entity)
}

func (s *Server) transitionCampaign(w http.ResponseWriter, r *http.Request) {
	var input campaign.TransitionInput
	if err := httpx.DecodeJSON(w, r, 2<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The campaign transition request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.ActorID = principal.User.ID
	if input.Action == campaign.ActionApproveFinal && !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		s.deps.Identity.RecordStepUpRequired(r.Context(), principal.User.ID, "campaign.final_approval", authenticationAttempt(r, principal.User.Email))
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for final campaign approval.", nil)
		return
	}
	if !authorisedForCampaignAction(principal.User, input.Action) {
		httpx.WriteError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "The user is not authorised for this campaign action.", nil)
		return
	}
	current, err := s.deps.Campaigns.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, campaign.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.resolveCampaignEvidence(r.Context(), current, &input); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CAMPAIGN_EVIDENCE_INVALID", "Authoritative campaign evidence did not satisfy the requested transition.", map[string]any{"detail": err.Error()})
		return
	}
	entity, err := s.deps.Campaigns.Transition(r.Context(), r.PathValue("id"), input)
	if errors.Is(err, campaign.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	if errors.Is(err, campaign.ErrConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "CAMPAIGN_VERSION_CONFLICT", "The campaign changed; reload before retrying.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CAMPAIGN_TRANSITION_REJECTED", "The campaign transition was rejected.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, entity)
}

func (s *Server) amendCampaignMaterial(w http.ResponseWriter, r *http.Request) {
	var input campaign.MaterialAmendmentInput
	if err := httpx.DecodeJSON(w, r, 2<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The material amendment request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		s.deps.Identity.RecordStepUpRequired(r.Context(), principal.User.ID, "campaign.material_amendment", authenticationAttempt(r, principal.User.Email))
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for a material campaign amendment.", nil)
		return
	}
	input.ActorID = principal.User.ID
	current, err := s.deps.Campaigns.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, campaign.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if input.ChangeAudience {
		snapshot, err := s.deps.Snapshots.Get(r.Context(), input.AudienceSnapshotID)
		if err != nil || snapshot.CampaignID != current.ID {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "AUDIENCE_SNAPSHOT_INVALID", "The replacement audience snapshot is not authoritative for this campaign.", nil)
			return
		}
		input.AudienceSnapshotHash = snapshot.SnapshotHash
		input.EligibleAudienceCount = snapshot.EligibleCount
	}
	if input.ChangeMessage {
		version, err := s.deps.Messages.Get(r.Context(), input.MessageVersionID)
		if err != nil || version.CampaignID != current.ID || version.Status != message.StatusApproved {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "MESSAGE_VERSION_INVALID", "The replacement message version is not an approved version for this campaign.", nil)
			return
		}
		input.MessageContentHash = version.ContentHash
	}
	entity, err := s.deps.Campaigns.AmendMaterial(r.Context(), current.ID, input)
	if errors.Is(err, campaign.ErrConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "CAMPAIGN_VERSION_CONFLICT", "The campaign changed; reload before retrying.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CAMPAIGN_AMENDMENT_REJECTED", "The material amendment was rejected.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, entity)
}

func (s *Server) listCampaignMaterialChanges(w http.ResponseWriter, r *http.Request) {
	items, err := s.deps.Campaigns.ListMaterialChanges(r.Context(), r.PathValue("id"))
	if errors.Is(err, campaign.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

type audienceImportMetadata struct {
	OrganisationID     string                 `json:"organisationId"`
	ConsentReviewID    string                 `json:"consentReviewId"`
	PurposeID          string                 `json:"purposeId"`
	Channel            string                 `json:"channel"`
	WordingVersion     string                 `json:"wordingVersion"`
	SourceName         string                 `json:"sourceName"`
	SourceSystem       string                 `json:"sourceSystem,omitempty"`
	DefaultCountryISO2 string                 `json:"defaultCountryIso2,omitempty"`
	TemplateVersion    string                 `json:"templateVersion"`
	Mapping            importer.ColumnMapping `json:"mapping"`
	UpdatePolicy       importer.UpdatePolicy  `json:"updatePolicy"`
}

func (s *Server) intakeAudienceImport(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceImportIntake == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_IMPORT_INTAKE_UNAVAILABLE", "Secure audience-import intake is not configured.", nil)
		return
	}
	requestKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if err := importer.ValidateIdempotencyKey(requestKey); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "A valid Idempotency-Key header is required.", map[string]any{"detail": err.Error()})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.deps.MaxImportFileBytes+(256<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "MULTIPART_REQUIRED", "The import must use multipart/form-data.", nil)
		return
	}
	metadataPart, err := reader.NextPart()
	if err != nil || metadataPart.FormName() != "metadata" || metadataPart.FileName() != "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "IMPORT_METADATA_FIRST", "The first multipart part must be a metadata JSON field.", nil)
		return
	}
	metadataBytes, err := io.ReadAll(io.LimitReader(metadataPart, (64<<10)+1))
	_ = metadataPart.Close()
	if err != nil || len(metadataBytes) > 64<<10 {
		httpx.WriteError(w, r, http.StatusBadRequest, "IMPORT_METADATA_INVALID", "Import metadata is unreadable or too large.", nil)
		return
	}
	var metadata audienceImportMetadata
	decoder := json.NewDecoder(strings.NewReader(string(metadataBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "IMPORT_METADATA_INVALID", "Import metadata is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "IMPORT_METADATA_INVALID", "Import metadata contains trailing JSON.", nil)
		return
	}
	filePart, err := reader.NextPart()
	if err != nil || filePart.FormName() != "file" || strings.TrimSpace(filePart.FileName()) == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "IMPORT_FILE_REQUIRED", "The metadata part must be followed by exactly one file part.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	batch, created, intakeErr := s.deps.AudienceImportIntake.Intake(r.Context(), importer.IntakeInput{
		OrganisationID: metadata.OrganisationID, ConsentReviewID: metadata.ConsentReviewID, PurposeID: metadata.PurposeID,
		Channel: metadata.Channel, WordingVersion: metadata.WordingVersion, SourceName: metadata.SourceName,
		SourceSystem: metadata.SourceSystem, DefaultCountryISO2: metadata.DefaultCountryISO2,
		OriginalFilename: filePart.FileName(), TemplateVersion: metadata.TemplateVersion, Mapping: metadata.Mapping,
		UpdatePolicy: metadata.UpdatePolicy, UploadedBy: principal.User.ID, ClientRequestID: requestKey,
	}, filePart)
	_ = filePart.Close()
	if extra, extraErr := reader.NextPart(); extraErr == nil {
		_ = extra.Close()
		httpx.WriteError(w, r, http.StatusBadRequest, "IMPORT_MULTIPART_EXTRA", "Only metadata and one file part are permitted.", nil)
		return
	}
	if intakeErr != nil {
		status := http.StatusUnprocessableEntity
		code := "AUDIENCE_IMPORT_REJECTED"
		switch {
		case errors.Is(intakeErr, importer.ErrImportReplayConflict), errors.Is(intakeErr, importer.ErrDuplicateImportFile):
			status, code = http.StatusConflict, "AUDIENCE_IMPORT_REPLAY_CONFLICT"
		case errors.Is(intakeErr, storage.ErrKeyConflict):
			status, code = http.StatusConflict, "AUDIENCE_IMPORT_OBJECT_CONFLICT"
		case errors.Is(intakeErr, storage.ErrTooLarge):
			status, code = http.StatusRequestEntityTooLarge, "AUDIENCE_IMPORT_TOO_LARGE"
		default:
			if batch.ID != "" && batch.MalwareStatus == importer.MalwareFailed {
				status, code = http.StatusBadGateway, "MALWARE_SCANNER_UNAVAILABLE"
			}
		}
		details := map[string]any{"detail": intakeErr.Error()}
		if batch.ID != "" {
			details["importId"], details["status"] = batch.ID, batch.Status
		}
		httpx.WriteError(w, r, status, code, "The audience import could not be accepted.", details)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, map[string]any{"created": created, "import": batch})
}

func (s *Server) getAudienceImportReconciliation(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceReconciliation == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "IMPORT_RECONCILIATION_UNAVAILABLE", "Audience import reconciliation is unavailable.", nil)
		return
	}
	if r.URL.Query().Get("closed") == "true" {
		record, err := s.deps.AudienceReconciliation.Get(r.Context(), r.PathValue("id"))
		if errors.Is(err, importer.ErrImportNotFound) {
			httpx.WriteError(w, r, http.StatusNotFound, "IMPORT_RECONCILIATION_NOT_FOUND", "No closed reconciliation exists for this import.", nil)
			return
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, record)
		return
	}
	result, err := s.deps.AudienceReconciliation.Preview(r.Context(), r.PathValue("id"))
	if errors.Is(err, importer.ErrImportNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "AUDIENCE_IMPORT_NOT_FOUND", "The audience import was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

type closeImportReconciliationRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) closeAudienceImportReconciliation(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceReconciliation == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "IMPORT_RECONCILIATION_UNAVAILABLE", "Audience import reconciliation is unavailable.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required to close import reconciliation.", nil)
		return
	}
	var input closeImportReconciliationRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The reconciliation closure request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	record, created, err := s.deps.AudienceReconciliation.Close(r.Context(), r.PathValue("id"), principal.User.ID, input.Reason)
	switch {
	case errors.Is(err, importer.ErrImportNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "AUDIENCE_IMPORT_NOT_FOUND", "The audience import was not found.", nil)
	case errors.Is(err, importer.ErrReconciliationNotReady):
		httpx.WriteError(w, r, http.StatusConflict, "IMPORT_RECONCILIATION_NOT_READY", "The import cannot be closed until totals balance and pending conflicts are resolved.", nil)
	case errors.Is(err, importer.ErrReconciliationConflict):
		httpx.WriteError(w, r, http.StatusConflict, "IMPORT_RECONCILIATION_CONFLICT", "A different reconciliation record already exists.", nil)
	case err != nil:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IMPORT_RECONCILIATION_REJECTED", "The import reconciliation could not be closed.", map[string]any{"detail": err.Error()})
	default:
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		httpx.WriteJSON(w, status, record)
	}
}

func (s *Server) listAudienceImportConflicts(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceConflicts == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_CONFLICTS_UNAVAILABLE", "Audience conflict review is not configured.", nil)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.deps.AudienceConflicts.List(r.Context(), r.PathValue("id"), importer.ConflictStatus(strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))), limit)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "AUDIENCE_CONFLICT_QUERY_INVALID", "The conflict query is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

type resolveAudienceConflictsBatchRequest struct {
	Decisions []importer.ConflictDecision `json:"decisions"`
}

func (s *Server) resolveAudienceImportConflictsBatch(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceConflicts == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_CONFLICTS_UNAVAILABLE", "Audience conflict review is not configured.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required to resolve profile conflicts.", nil)
		return
	}
	var input resolveAudienceConflictsBatchRequest
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The batch conflict resolution request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	result, err := s.deps.AudienceConflicts.ResolveBatch(r.Context(), input.Decisions, principal.User.ID)
	switch {
	case errors.Is(err, importer.ErrConflictNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "AUDIENCE_CONFLICT_NOT_FOUND", "A conflict in the batch was not found.", nil)
	case errors.Is(err, importer.ErrConflictVersion):
		httpx.WriteError(w, r, http.StatusConflict, "AUDIENCE_CONFLICT_VERSION", "A conflict changed; reload before retrying the batch.", nil)
	case errors.Is(err, importer.ErrConflictState):
		httpx.WriteError(w, r, http.StatusConflict, "AUDIENCE_CONFLICT_STATE", "A conflict in the batch is no longer pending.", nil)
	case err != nil:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "AUDIENCE_CONFLICT_BATCH_REJECTED", "The batch conflict resolution was rejected.", map[string]any{"detail": err.Error()})
	default:
		httpx.WriteJSON(w, http.StatusOK, result)
	}
}

type resolveAudienceConflictRequest struct {
	Resolution      importer.ConflictResolution `json:"resolution"`
	Reason          string                      `json:"reason"`
	ExpectedVersion int64                       `json:"expectedVersion"`
}

func (s *Server) resolveAudienceImportConflict(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceConflicts == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_CONFLICTS_UNAVAILABLE", "Audience conflict review is not configured.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "MFA_STEP_UP_REQUIRED", "Recent MFA verification is required.", nil)
		return
	}
	var input resolveAudienceConflictRequest
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	value, err := s.deps.AudienceConflicts.Resolve(r.Context(), r.PathValue("id"), input.Resolution, input.Reason, principal.User.ID, input.ExpectedVersion)
	switch {
	case errors.Is(err, importer.ErrConflictNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "AUDIENCE_CONFLICT_NOT_FOUND", "The audience conflict was not found.", nil)
	case errors.Is(err, importer.ErrConflictVersion):
		httpx.WriteError(w, r, http.StatusConflict, "AUDIENCE_CONFLICT_VERSION", "The conflict changed; reload before retrying.", nil)
	case errors.Is(err, importer.ErrConflictState):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "AUDIENCE_CONFLICT_STATE", "The conflict is no longer pending.", nil)
	case err != nil:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "AUDIENCE_CONFLICT_RESOLUTION_REJECTED", "The conflict resolution was rejected.", map[string]any{"detail": err.Error()})
	default:
		httpx.WriteJSON(w, http.StatusOK, value)
	}
}

func (s *Server) listAudienceSourceTrust(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceSourceTrust == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_SOURCE_TRUST_UNAVAILABLE", "Audience source-trust governance is not configured.", nil)
		return
	}
	items, err := s.deps.AudienceSourceTrust.List(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "AUDIENCE_SOURCE_TRUST_QUERY_INVALID", "The source-trust query is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

type upsertAudienceSourceTrustRequest struct {
	TrustLevel      int    `json:"trustLevel"`
	Reason          string `json:"reason"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

func (s *Server) upsertAudienceSourceTrust(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceSourceTrust == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_SOURCE_TRUST_UNAVAILABLE", "Audience source-trust governance is not configured.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "MFA_STEP_UP_REQUIRED", "Recent MFA verification is required.", nil)
		return
	}
	var input upsertAudienceSourceTrustRequest
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	value, err := s.deps.AudienceSourceTrust.Upsert(r.Context(), r.PathValue("id"), r.PathValue("source"), input.TrustLevel, input.Reason, principal.User.ID, input.ExpectedVersion)
	if errors.Is(err, importer.ErrSourceTrustVersion) {
		httpx.WriteError(w, r, http.StatusConflict, "AUDIENCE_SOURCE_TRUST_VERSION", "The source-trust policy changed; reload before retrying.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "AUDIENCE_SOURCE_TRUST_REJECTED", "The source-trust policy was rejected.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (s *Server) getAudienceImport(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceImports == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_IMPORTS_UNAVAILABLE", "Audience-import records are not configured.", nil)
		return
	}
	batch, err := s.deps.AudienceImports.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, importer.ErrImportNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "AUDIENCE_IMPORT_NOT_FOUND", "The audience import was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, batch)
}

type approveAudienceImportRequest struct {
	ExpectedVersion int64 `json:"expectedVersion"`
}

func (s *Server) approveAudienceImport(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceImports == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_IMPORTS_UNAVAILABLE", "Audience-import records are not configured.", nil)
		return
	}
	var input approveAudienceImportRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The import approval request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		s.deps.Identity.RecordStepUpRequired(r.Context(), principal.User.ID, "audience_import.approve", authenticationAttempt(r, principal.User.Email))
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required to approve an audience import.", nil)
		return
	}
	batch, err := s.deps.AudienceImports.Approve(r.Context(), r.PathValue("id"), principal.User.ID, input.ExpectedVersion)
	switch {
	case errors.Is(err, importer.ErrImportNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "AUDIENCE_IMPORT_NOT_FOUND", "The audience import was not found.", nil)
	case errors.Is(err, importer.ErrImportConflict):
		httpx.WriteError(w, r, http.StatusConflict, "AUDIENCE_IMPORT_VERSION_CONFLICT", "The audience import changed; reload before approving.", nil)
	case err != nil:
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "AUDIENCE_IMPORT_APPROVAL_REJECTED", "The audience import approval was rejected.", map[string]any{"detail": err.Error()})
	default:
		httpx.WriteJSON(w, http.StatusOK, batch)
	}
}

func (s *Server) previewAudienceImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_MULTIPART", "The import request must be multipart form data.", map[string]any{"detail": err.Error()})
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "FILE_REQUIRED", "A CSV file is required.", nil)
		return
	}
	defer file.Close()

	mapping := importer.ColumnMapping{
		MSISDN:  formOrDefault(r.MultipartForm, "msisdnColumn", "msisdn"),
		Country: formOrDefault(r.MultipartForm, "countryColumn", "country"),
		State:   formOrDefault(r.MultipartForm, "stateColumn", "state"),
		LGA:     formOrDefault(r.MultipartForm, "lgaColumn", "lga"),
		Age:     formOrDefault(r.MultipartForm, "ageColumn", "age"),
		Gender:  formOrDefault(r.MultipartForm, "genderColumn", "gender"),
	}
	result, err := importer.PreviewCSV(file, importer.PreviewOptions{
		DefaultCountryISO2: strings.ToUpper(formOrDefault(r.MultipartForm, "defaultCountry", "NG")),
		MaxRows:            s.deps.MaxImportPreviewRows,
		Mapping:            mapping,
		Protector:          s.deps.MSISDNProtector,
		GeographyValidator: s.deps.Geography.Validate,
	})
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "IMPORT_PREVIEW_FAILED", "The audience file could not be previewed.", map[string]any{"detail": err.Error()})
		return
	}
	// Never return raw or reversible identifiers in a preview response.
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) resolveCampaignEvidence(ctx context.Context, current campaign.Campaign, input *campaign.TransitionInput) error {
	now := time.Now().UTC()
	verifyConsent := func() error {
		review, err := s.deps.ConsentReviews.Get(ctx, current.ConsentReviewID)
		if err != nil {
			return err
		}
		if review.OrganisationID != current.OrganisationID || review.Status != consent.StatusApproved {
			return errors.New("campaign consent review is not approved for the selected organisation")
		}
		if review.ExpiresAt == nil || !review.ExpiresAt.After(now) {
			return errors.New("campaign consent review is expired")
		}
		if review.Channel != "WHATSAPP" {
			return errors.New("campaign consent channel is not WHATSAPP")
		}
		return nil
	}
	verifySnapshot := func(identifier string) error {
		if s.deps.Snapshots == nil {
			return errors.New("snapshot service is unavailable")
		}
		snapshot, err := s.deps.Snapshots.Get(ctx, identifier)
		if err != nil {
			return err
		}
		if snapshot.CampaignID != current.ID {
			return errors.New("audience snapshot belongs to another campaign")
		}
		if snapshot.EligibleCount <= 0 || snapshot.EligibleCount > current.MaximumUniqueRecipients {
			return errors.New("audience snapshot exceeds campaign entitlement")
		}
		input.AudienceSnapshotID = snapshot.ID
		input.AudienceSnapshotHash = snapshot.SnapshotHash
		input.EligibleAudienceCount = snapshot.EligibleCount
		return nil
	}
	verifyMessage := func(identifier string, approvedRequired bool) error {
		if s.deps.Messages == nil {
			return errors.New("message service is unavailable")
		}
		version, err := s.deps.Messages.Get(ctx, identifier)
		if err != nil {
			return err
		}
		if version.CampaignID != current.ID {
			return errors.New("message version belongs to another campaign")
		}
		if approvedRequired && version.Status != message.StatusApproved {
			return errors.New("message version has not been approved")
		}
		input.MessageVersionID = version.ID
		input.MessageContentHash = version.ContentHash
		return nil
	}
	switch input.Action {
	case campaign.ActionApproveConsent:
		return verifyConsent()
	case campaign.ActionValidateAudience:
		return verifySnapshot(input.AudienceSnapshotID)
	case campaign.ActionSubmitMessage:
		return verifyMessage(input.MessageVersionID, false)
	case campaign.ActionApproveMessage:
		if current.MessageVersionID == "" {
			return errors.New("campaign has no submitted message version")
		}
		return verifyMessage(current.MessageVersionID, true)
	case campaign.ActionComplete:
		if s.deps.DeliveryMetrics == nil {
			return errors.New("delivery metrics service is unavailable")
		}
		metrics, err := s.deps.DeliveryMetrics.Get(ctx, current.ID)
		if err != nil {
			return err
		}
		if metrics.ActiveObligations() > 0 {
			return fmt.Errorf("campaign has %d active delivery obligations", metrics.ActiveObligations())
		}
		if current.EligibleAudienceCount > 0 && metrics.TotalAccounted() != current.EligibleAudienceCount {
			return fmt.Errorf("campaign metrics do not reconcile: expected %d, accounted %d", current.EligibleAudienceCount, metrics.TotalAccounted())
		}
		input.FailedCount = metrics.FailedTotal
		input.UnknownCount = metrics.UnknownTotal
		return nil
	case campaign.ActionApproveFinal, campaign.ActionStartDispatch, campaign.ActionResume:
		if err := verifyConsent(); err != nil {
			return err
		}
		if current.AudienceSnapshotID == "" {
			return errors.New("campaign has no validated audience snapshot")
		}
		if err := verifySnapshot(current.AudienceSnapshotID); err != nil {
			return err
		}
		if current.MessageVersionID == "" {
			return errors.New("campaign has no approved message version")
		}
		return verifyMessage(current.MessageVersionID, true)
	default:
		return nil
	}
}

type createMessageVersionRequest struct {
	Type         message.Type       `json:"type"`
	Body         string             `json:"body"`
	Media        *message.Media     `json:"media"`
	Links        []message.Link     `json:"links"`
	Variables    []message.Variable `json:"variables"`
	AllowedHosts []string           `json:"allowedHosts"`
}

func (s *Server) listMessageVersions(w http.ResponseWriter, r *http.Request) {
	if s.deps.Messages == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "MESSAGE_SERVICE_UNAVAILABLE", "Message versioning is unavailable.", nil)
		return
	}
	items, err := s.deps.Messages.ListByCampaign(r.Context(), r.PathValue("id"))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createMessageVersion(w http.ResponseWriter, r *http.Request) {
	if s.deps.Messages == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "MESSAGE_SERVICE_UNAVAILABLE", "Message versioning is unavailable.", nil)
		return
	}
	campaignEntity, err := s.deps.Campaigns.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, campaign.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if campaignEntity.Status != campaign.StatusAudienceValidated && campaignEntity.Status != campaign.StatusMessageReviewPending {
		httpx.WriteError(w, r, http.StatusConflict, "MESSAGE_VERSION_NOT_ALLOWED", "A message version can be created only after audience validation and before message approval.", nil)
		return
	}
	var input createMessageVersionRequest
	if err := httpx.DecodeJSON(w, r, 2<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The message-version request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if err := message.ValidateIdempotencyKey(idempotencyKey); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "A valid Idempotency-Key header is required for message creation.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	created, err := s.deps.Messages.CreateDraft(r.Context(), message.Input{CampaignID: campaignEntity.ID, Type: input.Type, Body: input.Body, Media: input.Media, Links: input.Links, Variables: input.Variables, CreatedBy: principal.User.ID, IdempotencyKey: idempotencyKey, AllowedHosts: input.AllowedHosts})
	if errors.Is(err, message.ErrIdempotencyConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "The idempotency key was already used with different message content.", nil)
		return
	}
	if errors.Is(err, message.ErrDuplicateContent) {
		httpx.WriteError(w, r, http.StatusConflict, "MESSAGE_CONTENT_DUPLICATE", "Identical message content already exists for this campaign.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "MESSAGE_VERSION_INVALID", "The message version could not be created.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
}

type approveMessageVersionRequest struct {
	ExpectedContentHash string `json:"expectedContentHash"`
}

func (s *Server) approveMessageVersion(w http.ResponseWriter, r *http.Request) {
	if s.deps.Messages == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "MESSAGE_SERVICE_UNAVAILABLE", "Message versioning is unavailable.", nil)
		return
	}
	var input approveMessageVersionRequest
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The approval request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	approved, err := s.deps.Messages.Approve(r.Context(), r.PathValue("id"), principal.User.ID, input.ExpectedContentHash)
	if errors.Is(err, message.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "MESSAGE_VERSION_NOT_FOUND", "The message version was not found.", nil)
		return
	}
	if errors.Is(err, message.ErrApprovalStale) {
		httpx.WriteError(w, r, http.StatusConflict, "MESSAGE_VERSION_STALE", "The message content changed; reload before approval.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "MESSAGE_APPROVAL_REJECTED", "The message version could not be approved.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, approved)
}

func (s *Server) createAudienceSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.deps.Snapshots == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "SNAPSHOT_SERVICE_UNAVAILABLE", "Audience snapshots are unavailable.", nil)
		return
	}
	campaignEntity, err := s.deps.Campaigns.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, campaign.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if campaignEntity.Status != campaign.StatusAudienceBuilding {
		httpx.WriteError(w, r, http.StatusConflict, "SNAPSHOT_NOT_ALLOWED", "A snapshot can be materialised only while the campaign audience is building.", nil)
		return
	}
	var input segment.CreateInput
	if err := httpx.DecodeJSON(w, r, 8<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The snapshot request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	input.CampaignID = campaignEntity.ID
	input.CreatedBy = principal.User.ID
	if err := input.Definition.ValidateForPermissions(s.deps.Registry, principal.User.HasPermission); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "SEGMENT_INVALID", "The snapshot definition is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	created, err := s.deps.Snapshots.Create(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "SNAPSHOT_FAILED", "The audience snapshot could not be materialised.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
}

type materialiseAudienceSnapshotRequest struct {
	Definition           audiencefilter.Group `json:"definition"`
	SegmentID            string               `json:"segmentId,omitempty"`
	DefinitionVersion    int64                `json:"definitionVersion"`
	ConsentPolicyVersion string               `json:"consentPolicyVersion"`
	ConfigurationVersion string               `json:"configurationVersion"`
	AsOf                 *time.Time           `json:"asOf,omitempty"`
	Limit                int                  `json:"limit,omitempty"`
}

func (s *Server) materialiseAudienceSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.deps.Cohorts == nil || s.deps.Snapshots == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "COHORT_EXECUTION_UNAVAILABLE", "Cohort materialisation is unavailable.", nil)
		return
	}
	campaignEntity, err := s.deps.Campaigns.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, campaign.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if campaignEntity.Status != campaign.StatusAudienceBuilding {
		httpx.WriteError(w, r, http.StatusConflict, "SNAPSHOT_NOT_ALLOWED", "A snapshot can be materialised only while the campaign audience is building.", nil)
		return
	}
	var input materialiseAudienceSnapshotRequest
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The materialisation request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	asOf := time.Now().UTC()
	if input.AsOf != nil {
		asOf = input.AsOf.UTC()
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	members, err := s.deps.Cohorts.Materialise(r.Context(), input.Definition, cohort.EligibilityContext{OrganisationID: campaignEntity.OrganisationID, PurposeID: campaignEntity.PurposeID, Channel: "WHATSAPP", AsOf: asOf}, principal.User.HasPermission, input.Limit)
	if errors.Is(err, cohort.ErrCohortTooLarge) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COHORT_TOO_LARGE", "The eligible cohort exceeds the configured materialisation limit.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COHORT_MATERIALISATION_FAILED", "The cohort could not be materialised.", map[string]any{"detail": err.Error()})
		return
	}
	if int64(len(members)) > campaignEntity.MaximumUniqueRecipients {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COHORT_EXCEEDS_ENTITLEMENT", "The eligible cohort exceeds the campaign recipient entitlement.", map[string]any{"eligibleCount": len(members), "maximumUniqueRecipients": campaignEntity.MaximumUniqueRecipients})
		return
	}
	created, err := s.deps.Snapshots.Create(r.Context(), segment.CreateInput{CampaignID: campaignEntity.ID, SegmentID: strings.TrimSpace(input.SegmentID), Definition: input.Definition, DefinitionVersion: input.DefinitionVersion, ConsentPolicyVersion: input.ConsentPolicyVersion, ConfigurationVersion: input.ConfigurationVersion, CreatedBy: principal.User.ID, Members: members})
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "SNAPSHOT_FAILED", "The audience snapshot could not be materialised.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
}

type scheduleAudienceMaterialisationRequest struct {
	Definition           audiencefilter.Group `json:"definition"`
	SegmentID            string               `json:"segmentId,omitempty"`
	DefinitionVersion    int64                `json:"definitionVersion"`
	ConsentPolicyVersion string               `json:"consentPolicyVersion"`
	ConfigurationVersion string               `json:"configurationVersion"`
	AsOf                 *time.Time           `json:"asOf,omitempty"`
}

func (s *Server) scheduleAudienceMaterialisation(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceMaterialisations == nil || s.deps.Cohorts == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_MATERIALISATION_UNAVAILABLE", "Asynchronous audience materialisation is unavailable.", nil)
		return
	}
	entity, err := s.deps.Campaigns.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, campaign.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if entity.Status != campaign.StatusAudienceBuilding {
		httpx.WriteError(w, r, http.StatusConflict, "MATERIALISATION_NOT_ALLOWED", "Audience materialisation can be scheduled only while the campaign audience is building.", nil)
		return
	}
	var input scheduleAudienceMaterialisationRequest
	if err := httpx.DecodeJSON(w, r, 1<<20, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The materialisation request is invalid.", map[string]any{"detail": err.Error()})
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	segmentID := strings.TrimSpace(input.SegmentID)
	definition := input.Definition
	definitionVersion := input.DefinitionVersion
	if segmentID != "" {
		if s.deps.SegmentDefinitions == nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "SEGMENT_SERVICE_UNAVAILABLE", "Saved segments are unavailable.", nil)
			return
		}
		saved, loadErr := s.deps.SegmentDefinitions.Get(r.Context(), segmentID)
		if errors.Is(loadErr, segment.ErrDefinitionNotFound) {
			httpx.WriteError(w, r, http.StatusNotFound, "SEGMENT_NOT_FOUND", "The saved segment was not found.", nil)
			return
		}
		if loadErr != nil {
			s.internalError(w, r, loadErr)
			return
		}
		if saved.OrganisationID != entity.OrganisationID || saved.Status != segment.StatusActive {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, "SEGMENT_NOT_ELIGIBLE", "The saved segment is not active for this campaign organisation.", nil)
			return
		}
		definition = saved.Definition
		definitionVersion = saved.Version
	} else if definitionVersion <= 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "DEFINITION_VERSION_REQUIRED", "An inline definition version is required.", nil)
		return
	}
	asOf := time.Now().UTC()
	if input.AsOf != nil {
		asOf = input.AsOf.UTC()
	}
	eligibility := cohort.EligibilityContext{OrganisationID: entity.OrganisationID, PurposeID: entity.PurposeID, Channel: "WHATSAPP", AsOf: asOf}
	estimate, err := s.deps.Cohorts.Estimate(r.Context(), definition, eligibility, principal.User.HasPermission)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COHORT_ESTIMATE_FAILED", "The eligible audience could not be estimated.", map[string]any{"detail": err.Error()})
		return
	}
	if estimate.EligibleCount <= 0 {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "EMPTY_COHORT", "The cohort contains no eligible recipients.", nil)
		return
	}
	if estimate.EligibleCount > entity.MaximumUniqueRecipients {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "COHORT_EXCEEDS_ENTITLEMENT", "The eligible cohort exceeds the campaign recipient entitlement.", map[string]any{"eligibleCount": estimate.EligibleCount, "maximumUniqueRecipients": entity.MaximumUniqueRecipients})
		return
	}
	job, err := s.deps.AudienceMaterialisations.Schedule(r.Context(), materialisation.ScheduleMaterialisation{CampaignID: entity.ID, SegmentID: segmentID, Definition: definition, DefinitionVersion: definitionVersion, Eligibility: eligibility, ConsentPolicyVersion: input.ConsentPolicyVersion, ConfigurationVersion: input.ConfigurationVersion, RequestedBy: principal.User.ID, ExpectedCount: estimate.EligibleCount})
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "MATERIALISATION_SCHEDULE_FAILED", "The audience materialisation could not be scheduled.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, job)
}
func (s *Server) listAudienceMaterialisations(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceMaterialisations == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_MATERIALISATION_UNAVAILABLE", "Audience materialisation is unavailable.", nil)
		return
	}
	items, err := s.deps.AudienceMaterialisations.List(r.Context(), r.PathValue("id"), 50)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) getAudienceMaterialisation(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceMaterialisations == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_MATERIALISATION_UNAVAILABLE", "Audience materialisation is unavailable.", nil)
		return
	}
	item, err := s.deps.AudienceMaterialisations.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, materialisation.ErrMaterialisationNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "MATERIALISATION_NOT_FOUND", "The audience materialisation was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

type cancelAudienceMaterialisationRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason"`
}

func (s *Server) cancelAudienceMaterialisation(w http.ResponseWriter, r *http.Request) {
	if s.deps.AudienceMaterialisations == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "AUDIENCE_MATERIALISATION_UNAVAILABLE", "Audience materialisation is unavailable.", nil)
		return
	}
	var input cancelAudienceMaterialisationRequest
	if err := httpx.DecodeJSON(w, r, 64<<10, &input); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "The cancellation request is invalid.", nil)
		return
	}
	if strings.TrimSpace(input.Reason) == "" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "REASON_REQUIRED", "A cancellation reason is required.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	item, err := s.deps.AudienceMaterialisations.Cancel(r.Context(), r.PathValue("id"), principal.User.ID, input.Reason, input.ExpectedVersion)
	if errors.Is(err, materialisation.ErrMaterialisationConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "MATERIALISATION_CONFLICT", "The materialisation changed or can no longer be cancelled.", nil)
		return
	}
	if errors.Is(err, materialisation.ErrMaterialisationNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "MATERIALISATION_NOT_FOUND", "The audience materialisation was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (s *Server) getAudienceSnapshotOverlap(w http.ResponseWriter, r *http.Request) {
	if s.deps.Snapshots == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "SNAPSHOT_SERVICE_UNAVAILABLE", "Audience snapshots are unavailable.", nil)
		return
	}
	result, err := s.deps.Snapshots.Overlap(r.Context(), r.URL.Query().Get("leftId"), r.URL.Query().Get("rightId"))
	if errors.Is(err, segment.ErrSnapshotNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "SNAPSHOT_NOT_FOUND", "One or both audience snapshots were not found.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "SNAPSHOT_OVERLAP_INVALID", "The audience snapshot overlap could not be calculated.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) getAudienceSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.deps.Snapshots == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "SNAPSHOT_SERVICE_UNAVAILABLE", "Audience snapshots are unavailable.", nil)
		return
	}
	value, err := s.deps.Snapshots.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, segment.ErrSnapshotNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "SNAPSHOT_NOT_FOUND", "The audience snapshot was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (s *Server) getCampaignMetrics(w http.ResponseWriter, r *http.Request) {
	if s.deps.DeliveryMetrics == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "METRICS_SERVICE_UNAVAILABLE", "Campaign metrics are unavailable.", nil)
		return
	}
	value, err := s.deps.DeliveryMetrics.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, delivery.ErrMetricsNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_METRICS_NOT_FOUND", "Campaign metrics were not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (s *Server) releaseCampaignAudience(w http.ResponseWriter, r *http.Request) {
	if s.deps.Releases == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "RELEASE_SERVICE_UNAVAILABLE", "Campaign audience release is unavailable.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		s.deps.Identity.RecordStepUpRequired(r.Context(), principal.User.ID, "campaign.release", authenticationAttempt(r, principal.User.Email))
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required to release campaign recipients.", nil)
		return
	}
	result, err := s.deps.Releases.Release(r.Context(), r.PathValue("id"))
	if errors.Is(err, campaign.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	if errors.Is(err, orchestration.ErrReleaseConflict) || errors.Is(err, orchestration.ErrEntitlementExceeded) {
		httpx.WriteError(w, r, http.StatusConflict, "CAMPAIGN_RELEASE_CONFLICT", "The campaign release evidence or entitlement no longer matches the approved campaign.", map[string]any{"detail": err.Error()})
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "CAMPAIGN_RELEASE_REJECTED", "The campaign audience could not be released.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) getCampaignExecutionPlan(w http.ResponseWriter, r *http.Request) {
	if s.deps.Execution == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "EXECUTION_SERVICE_UNAVAILABLE", "Campaign execution planning is unavailable.", nil)
		return
	}
	value, err := s.deps.Execution.Plan(r.Context(), r.PathValue("id"))
	if errors.Is(err, campaign.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "EXECUTION_PLAN_REJECTED", "The campaign execution plan could not be produced.", map[string]any{"detail": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

type campaignExecutionRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason"`
}

func (s *Server) executeCampaignAction(w http.ResponseWriter, r *http.Request) {
	if s.deps.Execution == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "EXECUTION_SERVICE_UNAVAILABLE", "Campaign execution control is unavailable.", nil)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !identity.StepUpSatisfied(principal.Session, time.Now().UTC(), 10*time.Minute) {
		httpx.WriteError(w, r, http.StatusForbidden, "STEP_UP_REQUIRED", "Recent multi-factor verification is required for campaign execution controls.", nil)
		return
	}
	var input campaignExecutionRequest
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if input.ExpectedVersion <= 0 {
		httpx.WriteError(w, r, http.StatusBadRequest, "EXPECTED_VERSION_REQUIRED", "Expected campaign version is required.", nil)
		return
	}
	id, action := r.PathValue("id"), strings.ToLower(strings.TrimSpace(r.PathValue("action")))
	actor := principal.User.ID
	switch action {
	case "start":
		entity, plan, err := s.deps.Execution.Start(r.Context(), id, actor, input.Reason, input.ExpectedVersion)
		if err != nil {
			httpx.WriteError(w, r, http.StatusConflict, "CAMPAIGN_START_REJECTED", "Campaign dispatch could not start.", map[string]any{"detail": err.Error(), "plan": plan})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"campaign": entity, "plan": plan})
	case "pause":
		entity, err := s.deps.Execution.Pause(r.Context(), id, actor, input.Reason, input.ExpectedVersion)
		if err != nil {
			httpx.WriteError(w, r, http.StatusConflict, "CAMPAIGN_PAUSE_REJECTED", "Campaign could not be paused.", map[string]any{"detail": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, entity)
	case "resume":
		entity, plan, err := s.deps.Execution.Resume(r.Context(), id, actor, input.Reason, input.ExpectedVersion)
		if err != nil {
			httpx.WriteError(w, r, http.StatusConflict, "CAMPAIGN_RESUME_REJECTED", "Campaign could not resume.", map[string]any{"detail": err.Error(), "plan": plan})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"campaign": entity, "plan": plan})
	case "cancel":
		entity, err := s.deps.Execution.Cancel(r.Context(), id, actor, input.Reason, input.ExpectedVersion)
		if err != nil {
			httpx.WriteError(w, r, http.StatusConflict, "CAMPAIGN_CANCEL_REJECTED", "Campaign could not be cancelled.", map[string]any{"detail": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, entity)
	case "complete":
		entity, assessment, err := s.deps.Execution.AssessAndComplete(r.Context(), id, actor, input.ExpectedVersion)
		if err != nil {
			httpx.WriteError(w, r, http.StatusConflict, "CAMPAIGN_COMPLETION_REJECTED", "Campaign completion could not be recorded.", map[string]any{"detail": err.Error(), "assessment": assessment})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"campaign": entity, "assessment": assessment})
	default:
		httpx.WriteError(w, r, http.StatusNotFound, "EXECUTION_ACTION_NOT_FOUND", "The campaign execution action was not found.", nil)
	}
}

func authorisedForCampaignAction(user identity.User, action campaign.Action) bool {
	switch action {
	case campaign.ActionApproveConsent, campaign.ActionApproveMessage, campaign.ActionApproveCommercial,
		campaign.ActionApproveFinal:
		return user.HasPermission("campaign.approve")
	case campaign.ActionStartDispatch, campaign.ActionPause, campaign.ActionResume,
		campaign.ActionCancel, campaign.ActionComplete:
		return user.HasPermission("campaign.operate")
	default:
		return user.HasPermission("campaign.write")
	}
}

func readiness(ok bool) string {
	if ok {
		return "ok"
	}
	return "unavailable"
}

func formOrDefault(form *multipart.Form, key, fallback string) string {
	values := form.Value[key]
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return fallback
	}
	return strings.TrimSpace(values[0])
}

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" || len(requestID) > 128 {
			generated, err := id.New()
			if err != nil {
				requestID = fmt.Sprintf("fallback-%d", time.Now().UnixNano())
			} else {
				requestID = generated
			}
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(httpx.WithRequestID(r.Context(), requestID)))
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("panic recovered", "requestId", httpx.RequestID(r.Context()), "panic", recovered, "stack", string(debug.Stack()))
				httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred.", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(payload []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(payload)
	r.bytes += n
	return n, err
}

func (s *Server) requestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		s.logger.Info("http request",
			"requestId", httpx.RequestID(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"bytes", recorder.bytes,
			"duration", time.Since(started),
		)
	})
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("request failed", "requestId", httpx.RequestID(r.Context()), "error", err)
	httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "The request could not be completed.", nil)
}

// Ensure compile-time JSON use remains explicit for future streaming endpoints.
var _ = json.Valid

func (s *Server) listInboundReplies(w http.ResponseWriter, r *http.Request) {
	if s.deps.InboundReplies == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "INBOUND_REPLIES_UNAVAILABLE", "Inbound reply operations are not configured.", nil)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.deps.InboundReplies.List(r.Context(), limit)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	principal, _ := identity.PrincipalFromContext(r.Context())
	if !principal.User.HasPermission("inbound.content.read") {
		for index := range items {
			items[index].MessageText = ""
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) getInboundReplyContent(w http.ResponseWriter, r *http.Request) {
	if s.deps.InboundReplies == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "INBOUND_REPLIES_UNAVAILABLE", "Inbound reply operations are not configured.", nil)
		return
	}
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
		return
	}
	item, err := s.deps.InboundReplies.Reveal(r.Context(), r.PathValue("id"), principal.User.ID, httpx.RequestID(r.Context()))
	if errors.Is(err, inbound.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "INBOUND_REPLY_NOT_FOUND", "The inbound reply was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if item.ContentRedactedAt != nil {
		httpx.WriteError(w, r, http.StatusGone, "INBOUND_CONTENT_REDACTED", "The reply content has been redacted under retention policy.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"id": item.ID, "messageText": item.MessageText, "fingerprint": item.MessageFingerprint, "retainUntil": item.ContentRetainUntil, "legalHold": item.LegalHold})
}

func (s *Server) runInboundReplyRetentionSweep(w http.ResponseWriter, r *http.Request) {
	if s.deps.InboundReplies == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "INBOUND_REPLIES_UNAVAILABLE", "Inbound reply operations are not configured.", nil)
		return
	}
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
		return
	}
	count, err := s.deps.InboundReplies.RedactExpiredAudited(r.Context(), principal.User.ID, httpx.RequestID(r.Context()))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"redacted": count})
}

func (s *Server) setInboundReplyLegalHold(w http.ResponseWriter, r *http.Request) {
	if s.deps.InboundReplies == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "INBOUND_REPLIES_UNAVAILABLE", "Inbound reply operations are not configured.", nil)
		return
	}
	var input struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		LegalHold       bool   `json:"legalHold"`
		Reason          string `json:"reason"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
		return
	}
	item, err := s.deps.InboundReplies.SetLegalHold(r.Context(), r.PathValue("id"), input.ExpectedVersion, input.LegalHold, input.Reason, principal.User.ID, httpx.RequestID(r.Context()))
	if errors.Is(err, inbound.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "INBOUND_REPLY_NOT_FOUND", "The inbound reply was not found.", nil)
		return
	}
	if errors.Is(err, inbound.ErrConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "INBOUND_REPLY_CONFLICT", "The inbound reply changed before the legal-hold action was saved.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "INBOUND_LEGAL_HOLD_INVALID", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (s *Server) reviewInboundReply(w http.ResponseWriter, r *http.Request) {
	if s.deps.InboundReplies == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "INBOUND_REPLIES_UNAVAILABLE", "Inbound reply operations are not configured.", nil)
		return
	}
	var input struct {
		ExpectedVersion  int64                  `json:"expectedVersion"`
		Classification   inbound.Classification `json:"classification"`
		Escalated        bool                   `json:"escalated"`
		EscalationReason string                 `json:"escalationReason"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
		return
	}
	item, err := s.deps.InboundReplies.Review(r.Context(), r.PathValue("id"), input.ExpectedVersion, input.Classification, input.Escalated, input.EscalationReason, principal.User.ID)
	if errors.Is(err, inbound.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "INBOUND_REPLY_NOT_FOUND", "The inbound reply was not found.", nil)
		return
	}
	if errors.Is(err, inbound.ErrConflict) {
		httpx.WriteError(w, r, http.StatusConflict, "INBOUND_REPLY_CONFLICT", "The inbound reply changed before this review was saved.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "INBOUND_REPLY_INVALID", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (s *Server) getCampaignInboundMetrics(w http.ResponseWriter, r *http.Request) {
	if s.deps.InboundReplies == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "INBOUND_REPLIES_UNAVAILABLE", "Inbound reply operations are not configured.", nil)
		return
	}
	value, err := s.deps.InboundReplies.Summary(r.Context(), r.PathValue("id"))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}
