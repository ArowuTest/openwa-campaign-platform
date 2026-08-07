package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"campaign-platform/internal/audience/cohort"
	"campaign-platform/internal/audience/contactlife"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/audience/importer"
	"campaign-platform/internal/audience/materialisation"
	"campaign-platform/internal/audit"
	"campaign-platform/internal/campaign"
	"campaign-platform/internal/campaignworkspace"
	"campaign-platform/internal/commercial"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/delivery"
	"campaign-platform/internal/execution"
	"campaign-platform/internal/geography"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/inbound"
	"campaign-platform/internal/jobs"
	"campaign-platform/internal/message"
	"campaign-platform/internal/operations"
	"campaign-platform/internal/orchestration"
	"campaign-platform/internal/organisation"
	"campaign-platform/internal/persistence/database"
	postgresrepo "campaign-platform/internal/persistence/postgres"
	"campaign-platform/internal/platform/httpserver"
	"campaign-platform/internal/platformpolicy"
	"campaign-platform/internal/privacy"
	"campaign-platform/internal/provider"
	"campaign-platform/internal/retention"
	"campaign-platform/internal/security/malware"
	"campaign-platform/internal/segment"
	"campaign-platform/internal/sender"
	"campaign-platform/internal/shared/config"
	sharedcrypto "campaign-platform/internal/shared/crypto"
	"campaign-platform/internal/storage"
	"campaign-platform/internal/testmessage"
)

type inboundAuditSink struct{ recorder *audit.Recorder }

func (s inboundAuditSink) RecordInboundAudit(ctx context.Context, record inbound.AuditRecord) error {
	_, err := s.recorder.Record(ctx, audit.Input{ActorType: "USER", ActorID: record.ActorID, Action: record.Action, ObjectType: record.ObjectType, ObjectID: record.ObjectID, After: record.After, ReasonCode: record.ReasonCode, CorrelationID: record.CorrelationID})
	return err
}

type controlRuntime struct {
	Dependencies httpserver.Dependencies
	DB           *sql.DB
	close        func() error
}

func (r *controlRuntime) Close() error {
	if r == nil || r.close == nil {
		return nil
	}
	return r.close()
}

func buildControlRuntime(ctx context.Context, cfg config.Config, logger *slog.Logger) (*controlRuntime, error) {
	protector, err := buildMSISDNProtector(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("initialise MSISDN protection: %w", err)
	}
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		if cfg.Environment != "development" && cfg.Environment != "test" {
			return nil, errors.New("persistent PostgreSQL storage is required outside development and test")
		}
		return buildMemoryRuntime(cfg, protector, logger)
	}
	return buildPostgreSQLRuntime(ctx, cfg, protector, logger)
}

func buildMemoryRuntime(cfg config.Config, protector *sharedcrypto.MSISDNProtector, logger *slog.Logger) (*controlRuntime, error) {
	filterStore, err := audiencefilter.NewMemoryAdministrationStore(audiencefilter.DefaultDefinitions())
	if err != nil {
		return nil, err
	}
	filters, err := buildFilterRuntime(context.Background(), filterStore)
	if err != nil {
		return nil, err
	}
	hash, err := identity.HashPassword(cfg.BootstrapAdminPassword)
	if err != nil {
		return nil, err
	}
	admin := identity.User{ID: "00000000-0000-4000-8000-000000000001", Email: cfg.BootstrapAdminEmail, DisplayName: "Bootstrap Administrator", Status: identity.StatusActive, PasswordHash: hash, TOTPSecret: cfg.BootstrapAdminTOTP, MFARequired: true, Permissions: map[string]struct{}{"*": {}}}
	adminStore := identity.NewMemoryAdministrationRepository("SUPER_ADMIN", "CAMPAIGN_OPERATOR", "COMPLIANCE_REVIEWER", "CAMPAIGN_APPROVER", "ANALYST", "TECHNICAL_ADMIN", "FINANCE_USER", "FINANCE_APPROVER")
	now := time.Now().UTC()
	if err := adminStore.CreateAccount(context.Background(), identity.Account{ID: admin.ID, Email: admin.Email, DisplayName: admin.DisplayName, Status: identity.StatusActive, MFARequired: true, RoleCodes: []string{"SUPER_ADMIN"}, Version: 1, CreatedAt: now, UpdatedAt: now}, hash, admin.TOTPSecret, admin.ID, "bootstrap administrator"); err != nil {
		return nil, fmt.Errorf("bootstrap administrator: %w", err)
	}
	memorySessions := identity.NewMemorySessionRepository()
	identityService := identity.NewPersistentServiceWithEvents(adminStore, memorySessions, identity.NewMemoryChallengeRepository(), identity.NewMemoryEventRecorder(), cfg.SessionIdleTimeout, cfg.SessionAbsoluteTimeout)
	identityAdministration := identity.NewAdministrationService(adminStore, memorySessions)
	orgs := organisation.NewService(organisation.NewMemoryRepository())
	organisationPolicies := &organisation.PolicyAdministration{Store: organisation.NewMemoryPolicyStore(), Organisations: orgs}
	commercialService := &commercial.Service{Store: commercial.NewMemoryStore()}
	reviews := consent.NewService(consent.NewMemoryRepository()).WithOrganisationReader(orgs)
	consentLedger := consent.NewLedgerService(consent.NewMemoryLedgerRepository())
	deliveryEvents := delivery.NewService(delivery.NewMemoryRepository())
	auditRepository := audit.NewMemoryRepository()
	auditRecorder := audit.NewRecorder(auditRepository)
	privacyKeyring, err := buildPrivacyEvidenceKeyring(cfg, false)
	if err != nil {
		return nil, fmt.Errorf("initialise privacy evidence protection: %w", err)
	}
	privacyCases := &privacy.Service{Repository: privacy.NewMemoryRepository(), Protector: protector, Evidence: privacyKeyring, Audit: auditRecorder}
	nowPolicy := time.Now().UTC()
	retentionStore := inbound.NewMemoryRetentionPolicyStore(inbound.RetentionPolicy{ID: "bootstrap-inbound-retention", RetentionDays: cfg.InboundRetentionDays, Status: inbound.RetentionPolicyActive, EffectiveFrom: nowPolicy.Add(-time.Second), Version: 1, CreatedBy: consent.DefaultGatewayServiceActorID, ApprovedBy: consent.DefaultGatewayServiceActorID, Reason: "bootstrap governed inbound retention policy", CreatedAt: nowPolicy, UpdatedAt: nowPolicy})
	retentionPolicies := &inbound.RetentionPolicyAdministration{Store: retentionStore}
	rotationService := &inbound.RotationService{Repository: inbound.NewMemoryRotationRepository(), ActiveKeyVersion: "v1"}
	inboundReplies := &inbound.Service{Repository: inbound.NewMemoryRepository(), Audit: inboundAuditSink{recorder: auditRecorder}, RetentionPolicies: retentionPolicies}
	policyStore := consent.NewMemoryOptOutPolicyStore(consent.GovernedOptOutPolicy{ID: "bootstrap-opt-out-policy", Keywords: cfg.OptOutKeywords, Status: consent.OptOutPolicyActive, EffectiveFrom: nowPolicy.Add(-time.Second), Version: 1, CreatedBy: consent.DefaultGatewayServiceActorID, Reason: "bootstrap governed opt-out policy", CreatedAt: nowPolicy, UpdatedAt: nowPolicy})
	optOutPolicies := &consent.OptOutPolicyAdministration{Store: policyStore}
	optOutProcessor := &consent.OptOutProcessor{Deliveries: deliveryEvents, Ledger: consentLedger, Policies: optOutPolicies, ActorID: consent.DefaultGatewayServiceActorID, Inbox: inboundReplies, Protector: protector}
	providerStore := provider.NewMemoryStore()
	providerCapabilities := &provider.Service{Store: providerStore}
	if err := bootstrapProviderCapabilities(context.Background(), providerStore, nowPolicy); err != nil {
		return nil, fmt.Errorf("bootstrap provider capabilities: %w", err)
	}
	senderStore := sender.NewMemoryGovernanceStore()
	proxyKeys, err := buildSenderProxyKeyring(cfg, false)
	if err != nil {
		return nil, fmt.Errorf("initialise sender proxy protection: %w", err)
	}
	senderProxies := &sender.SessionProxyAdministration{Store: senderStore, Keys: proxyKeys}
	gatewayPools := &sender.GatewayPoolService{Store: senderStore}
	if err := bootstrapGatewayPools(context.Background(), gatewayPools); err != nil {
		return nil, fmt.Errorf("bootstrap gateway pools: %w", err)
	}
	gatewayPoolAdministration := &sender.GatewayPoolAdministration{Store: senderStore}
	gatewayRuntime := &sender.RuntimeRegistrationService{Store: senderStore, GatewayPools: senderStore, Secret: []byte(cfg.GatewayRuntimeSecret), PreviousSecrets: nonEmptySecrets(cfg.GatewayRuntimePreviousSecret), MaximumSkew: cfg.GatewayCallbackMaxSkew}
	platformPolicyStore := platformpolicy.NewMemoryStore()
	configurations := &platformpolicy.ConfigurationAdministration{Store: platformPolicyStore}
	maintenance := &platformpolicy.MaintenanceAdministration{Store: platformPolicyStore}
	campaigns := campaign.NewService(campaign.NewMemoryRepository()).WithOrganisationReader(orgs).WithOrganisationPolicies(organisationPolicies).WithCommercialApprovals(commercialService).WithConsentReviews(reviews).WithProviderCapabilities(providerCapabilities).WithGatewayPools(gatewayPools)
	messages := message.NewService(message.NewMemoryRepository())
	testMessages := &testmessage.Service{Repository: testmessage.NewMemoryRepository(), Protector: protector, Messages: messages, Routes: testmessage.RouteValidatorFunc(func(context.Context, testmessage.RouteRequirements) (testmessage.RouteEvidence, error) {
		return testmessage.RouteEvidence{GatewayPoolVersion: 1, AdapterVersion: "0.13.0", ProviderDefinitionID: "00000000-0000-4000-8000-000000000101", ProviderDefinitionVersion: 1, GatewayNodeID: "00000000-0000-4000-8000-000000000201", GatewayNodeVersion: 1, SessionLeaseVersion: 1, SessionConfigurationVersion: 1, AuthorityExpiresAt: time.Now().UTC().Add(10 * time.Minute)}, nil
	})}
	snapshots := segment.NewService(segment.NewMemoryRepository())
	segmentDefinitions := &segment.DefinitionService{Repository: segment.NewMemoryDefinitionRepository(), Registry: filters.Registry}
	cohortExecution := cohort.NewExecutionService(filters.Compiler, &cohort.MemoryQueryRepository{})
	materialisationService := &materialisation.MaterialisationService{Repository: materialisation.NewMemoryMaterialisationRepository()}
	metrics := delivery.NewMetricsService(delivery.NewMemoryMetricsRepository())
	senderGovernance := &sender.GovernanceService{Store: senderStore}
	senderLifecycle := &sender.SessionLifecycleService{Governance: senderGovernance, Gateway: &sender.HTTPSessionGateway{CommandSecret: cfg.GatewayCommandSecret}, Proxies: senderProxies, ActiveWork: sender.StaticActiveSessionWorkChecker(false), Maintenance: maintenance}
	pacingPolicies := &sender.PacingAdministration{Store: sender.NewMemoryPacingStore()}
	executionStore := execution.NewMemoryStore()
	executionCoordinator := &execution.Coordinator{Campaigns: campaigns, Store: executionStore, SafetyMarginPercent: 15, Maintenance: maintenance}
	campaignWorkspace := &campaignworkspace.Service{Repository: campaignworkspace.NewMemoryRepository(), Campaigns: campaigns, Metrics: campaignworkspace.MetricsReaderFunc(func(ctx context.Context, id string) (campaignworkspace.DeliveryMetrics, error) {
		m, err := executionStore.Metrics(ctx, id)
		return campaignworkspace.DeliveryMetrics{Authorised: m.Authorised, Queued: m.Queued, Pending: m.Pending, Submitted: m.Submitted, Sent: m.Sent, Delivered: m.Delivered, Read: m.Read, Unknown: m.Unknown}, err
	})}
	routingPlans := &execution.RoutingAdministration{Store: execution.NewMemoryRoutingPlanStore(), Campaigns: campaigns, ProviderCapabilities: providerCapabilities, GatewayPools: gatewayPools}
	shardReallocations := &execution.ReallocationAdministration{Store: execution.NewMemoryShardRepository()}
	executionCoordinator.RoutingPlans = routingPlans
	reportingPrivacy := &operations.ReportingPrivacyAdministration{Store: operations.NewMemoryReportingPrivacyStore(), Audit: auditRecorder}
	operationsRepo := operations.NewMemoryRepository()
	alertStore := operations.NewMemoryAlertStore()
	alertStore.IncidentRepo = operationsRepo
	alertPolicies := &operations.AlertAdministration{Store: alertStore}
	operationsService := &operations.Service{Repo: operationsRepo, Audit: auditRecorder, AuditRepository: auditRepository, Deliveries: deliveryEvents, ReportingPrivacy: reportingPrivacy, Alerting: alertStore}
	alertEvaluator := &operations.AlertEvaluator{Store: alertStore, Dashboard: operationsService}
	platformRetentionStore := retention.NewMemoryStore()
	platformRetention := &retention.Administration{Store: platformRetentionStore}
	importRepository := importer.NewMemoryImportRepository()
	imports := &importer.ImportService{Repository: importRepository, Organisations: orgs}
	importMappings := &importer.MappingAdministration{Store: importer.NewMemoryMappingStore(), Audit: auditRecorder}
	importRollback := &importer.RollbackService{Repository: &importer.MemoryRollbackRepository{Imports: importRepository}, Audit: auditRecorder}
	importIssues := &importer.IssueExportService{Repository: &importer.MemoryIssueRepository{Staging: importer.NewMemoryStagingRepository()}, Audit: auditRecorder}
	contactLifecycle := &contactlife.Service{Repository: contactlife.NewMemoryRepository(), Audit: auditRecorder}
	conflictRepository := importer.NewMemoryConflictRepository()
	audienceConflicts := &importer.ConflictService{Repository: conflictRepository}
	audienceReconciliation := &importer.ReconciliationService{Imports: imports, Conflicts: audienceConflicts, Repository: importer.NewMemoryReconciliationRepository()}
	audienceSourceTrust := &importer.SourceTrustService{Repository: importer.NewMemorySourceTrustRepository()}
	mediaStore, err := storage.NewObjectStoreFromEnvironment(cfg.ObjectStoreRoot)
	if err != nil {
		return nil, fmt.Errorf("initialise media object store: %w", err)
	}
	trustedAssets, err := buildTrustedAssets(cfg, mediaStore, storage.NewMemoryAssetRepository())
	if err != nil {
		return nil, err
	}
	if trustedAssets != nil {
		messages.Assets = trustedMessageAssetResolver{service: trustedAssets, maximum: 64 << 20}
		reviews.Evidence = trustedConsentEvidenceResolver{service: trustedAssets}
	}
	messages.LinkPolicies = message.StaticLinkPolicy{Hosts: cfg.MessageAllowedHosts, Version: "environment-v1"}
	jobOperations := &jobs.AdministrationService{Repository: jobs.NewMemoryRepository()}
	intake, err := buildImportIntake(cfg, imports, mediaStore)
	if err != nil {
		return nil, err
	}
	if intake == nil {
		logger.Warn("secure audience-import intake is disabled until CLAMAV_ADDRESS is configured")
	}
	deps := httpserver.Dependencies{Registry: filters.Registry, FilterDefinitions: filters.Administration, Compiler: filters.Compiler, Cohorts: cohortExecution, Organisations: orgs, OrganisationPolicies: organisationPolicies, ConsentReviews: reviews, ConsentLedger: consentLedger, OptOutProcessor: optOutProcessor, OptOutPolicies: optOutPolicies, InboundReplies: inboundReplies, InboundRetentionPolicies: retentionPolicies, InboundRotation: rotationService, Campaigns: campaigns, CampaignWorkspace: campaignWorkspace, Commercial: commercialService, Geography: geography.DefaultCatalogue(), MaxImportPreviewRows: cfg.MaxImportPreviewRows, Identity: identityService, IdentityAdministration: identityAdministration, SecureCookies: cfg.SecureCookies, NetworkPolicy: httpserver.NetworkPolicy{AllowedCIDRs: cfg.AllowedNetworkCIDRs, TrustedProxyCIDRs: cfg.TrustedProxyCIDRs}, MSISDNProtector: protector, Messages: messages, TestMessages: testMessages, Snapshots: snapshots, SegmentDefinitions: segmentDefinitions, AudienceMaterialisations: materialisationService, DeliveryMetrics: metrics, Execution: executionCoordinator, RoutingPlans: routingPlans, ShardReallocations: shardReallocations, JobOperations: jobOperations, Operations: operationsService, PrivacyCases: privacyCases, ContactLifecycle: contactLifecycle, SenderGovernance: senderGovernance, SenderSessionProxies: senderProxies, GatewayPools: gatewayPoolAdministration, GatewayRuntime: gatewayRuntime, SenderSessionLifecycle: senderLifecycle, PacingPolicies: pacingPolicies, ProviderCapabilities: providerCapabilities, Configurations: configurations, Maintenance: maintenance, Retention: platformRetention, AlertPolicies: alertPolicies, AlertEvaluator: alertEvaluator, AudienceImports: imports, AudienceImportMappings: importMappings, AudienceImportRollback: importRollback, AudienceImportIssues: importIssues, AudienceConflicts: audienceConflicts, AudienceReconciliation: audienceReconciliation, AudienceSourceTrust: audienceSourceTrust, AudienceImportIntake: intake, MaxImportFileBytes: cfg.MaxImportFileBytes, DeliveryEvents: deliveryEvents, GatewayCallbackSecret: []byte(cfg.GatewayCallbackSecret), GatewayCallbackPreviousSecrets: nonEmptySecrets(cfg.GatewayCallbackPreviousSecret), GatewayCallbackMaxSkew: cfg.GatewayCallbackMaxSkew, MediaObjects: mediaStore, TrustedAssets: trustedAssets, MediaDownloadSecret: []byte(cfg.MediaDownloadSecret), ReadinessChecks: []httpserver.ReadinessCheck{filters.Readiness}}
	return &controlRuntime{Dependencies: deps}, nil
}

func buildPostgreSQLRuntime(ctx context.Context, cfg config.Config, protector *sharedcrypto.MSISDNProtector, logger *slog.Logger) (*controlRuntime, error) {
	db, err := database.Open(ctx, database.PoolConfig{Driver: cfg.DatabaseDriver, DSN: cfg.DatabaseURL, MaxOpen: cfg.DatabaseMaxOpen, MaxIdle: cfg.DatabaseMaxIdle, ConnMaxLifetime: cfg.DatabaseConnMaxLifetime, ConnMaxIdleTime: cfg.DatabaseConnMaxIdleTime, PingTimeout: cfg.DatabasePingTimeout, Environment: cfg.Environment, ServiceName: "control-api"})
	if err != nil {
		return nil, err
	}
	fail := func(e error) (*controlRuntime, error) { _ = db.Close(); return nil, e }
	secretBox, err := sharedcrypto.NewSecretBoxFromBase64(cfg.IdentitySecretKeyBase64)
	if err != nil {
		return fail(fmt.Errorf("initialise identity secret protection: %w", err))
	}
	identityRepo := &postgresrepo.IdentityRepository{DB: db, Secrets: secretBox}
	hash, err := identity.HashPassword(cfg.BootstrapAdminPassword)
	if err != nil {
		return fail(err)
	}
	bootstrap := identity.User{ID: "00000000-0000-4000-8000-000000000001", Email: cfg.BootstrapAdminEmail, DisplayName: "Bootstrap Administrator", Status: identity.StatusActive, PasswordHash: hash, TOTPSecret: cfg.BootstrapAdminTOTP, MFARequired: true}
	_, created, err := identityRepo.EnsureBootstrapAdministrator(ctx, bootstrap)
	if err != nil {
		return fail(fmt.Errorf("ensure bootstrap administrator: %w", err))
	}
	if created {
		logger.Warn("bootstrap super-administrator created; rotate bootstrap deployment secrets immediately", "email", cfg.BootstrapAdminEmail)
	}
	sessionRepo := &postgresrepo.IdentitySessionRepository{DB: db}
	identityService := identity.NewPersistentServiceWithEvents(identityRepo, sessionRepo, &postgresrepo.IdentityChallengeRepository{DB: db}, &postgresrepo.AuthenticationEventRecorder{DB: db}, cfg.SessionIdleTimeout, cfg.SessionAbsoluteTimeout)
	identityAdministration := identity.NewAdministrationService(&postgresrepo.IdentityAdministrationRepository{DB: db, Secrets: secretBox}, sessionRepo)
	filterStore := &postgresrepo.FilterDefinitionStore{DB: db}
	filters, err := buildFilterRuntime(ctx, filterStore)
	if err != nil {
		return fail(err)
	}
	orgs := organisation.NewService(&postgresrepo.OrganisationRepository{DB: db})
	organisationPolicies := &organisation.PolicyAdministration{Store: &postgresrepo.OrganisationPolicyRepository{DB: db}, Organisations: orgs}
	commercialService := &commercial.Service{Store: &postgresrepo.CommercialRepository{DB: db}}
	reviews := consent.NewService(&postgresrepo.ConsentRepository{DB: db}).WithOrganisationReader(orgs)
	consentLedger := consent.NewLedgerService(&postgresrepo.ConsentLedgerRepository{DB: db})
	deliveryEvents := delivery.NewService(&delivery.PostgreSQLRepository{DB: db})
	var inboundKeyring *sharedcrypto.SecretKeyring
	if cfg.InboundContentKeysJSON != "" {
		inboundKeyring, err = sharedcrypto.NewSecretKeyringFromJSON(cfg.InboundContentActiveKey, cfg.InboundContentKeysJSON)
	} else {
		inboundKeyring, err = sharedcrypto.NewSecretKeyringFromJSON("v1", fmt.Sprintf(`{"v1":%q}`, cfg.InboundContentKeyBase64))
	}
	if err != nil {
		return fail(fmt.Errorf("initialise inbound content keyring: %w", err))
	}
	auditRepository := &audit.PostgreSQLRepository{DB: db}
	auditRecorder := audit.NewRecorder(auditRepository)
	privacyKeyring, err := buildPrivacyEvidenceKeyring(cfg, true)
	if err != nil {
		return fail(fmt.Errorf("initialise privacy evidence protection: %w", err))
	}
	privacyCases := &privacy.Service{Repository: &privacy.PostgreSQLRepository{DB: db}, Protector: protector, Evidence: privacyKeyring, Audit: auditRecorder}
	retentionStore := &inbound.PostgreSQLRetentionPolicyStore{DB: db}
	retentionPolicies := &inbound.RetentionPolicyAdministration{Store: retentionStore}
	if _, retentionErr := retentionStore.Active(ctx, time.Now().UTC()); errors.Is(retentionErr, inbound.ErrRetentionPolicyNotFound) {
		nowRetention := time.Now().UTC()
		_, retentionErr = retentionStore.Create(ctx, inbound.RetentionPolicy{ID: "bootstrap-inbound-retention", RetentionDays: cfg.InboundRetentionDays, Status: inbound.RetentionPolicyActive, EffectiveFrom: nowRetention.Add(-time.Second), Version: 1, CreatedBy: "00000000-0000-4000-8000-000000000001", ApprovedBy: "00000000-0000-4000-8000-000000000001", Reason: "bootstrap governed inbound retention policy", CreatedAt: nowRetention, UpdatedAt: nowRetention})
	} else if retentionErr != nil {
		return fail(fmt.Errorf("initialise inbound retention policy: %w", retentionErr))
	}
	inboundReplies := &inbound.Service{Repository: &inbound.PostgreSQLRepository{DB: db, Keyring: inboundKeyring}, Audit: inboundAuditSink{recorder: auditRecorder}, RetentionPolicies: retentionPolicies}
	rotationService := &inbound.RotationService{Repository: &inbound.PostgreSQLRotationRepository{DB: db, Keyring: inboundKeyring}, ActiveKeyVersion: inboundKeyring.ActiveVersion()}
	policyStore := &postgresrepo.OptOutPolicyStore{DB: db}
	optOutPolicies := &consent.OptOutPolicyAdministration{Store: policyStore}
	_, activeErr := policyStore.Active(ctx, time.Now().UTC())
	if errors.Is(activeErr, consent.ErrOptOutPolicyNotFound) {
		nowPolicy := time.Now().UTC()
		_, activeErr = policyStore.Create(ctx, consent.GovernedOptOutPolicy{ID: "bootstrap-opt-out-policy", Keywords: cfg.OptOutKeywords, Status: consent.OptOutPolicyActive, EffectiveFrom: nowPolicy.Add(-time.Second), Version: 1, CreatedBy: consent.DefaultGatewayServiceActorID, Reason: "bootstrap governed opt-out policy", CreatedAt: nowPolicy, UpdatedAt: nowPolicy})
	}
	if activeErr != nil {
		return fail(fmt.Errorf("initialise governed opt-out policy: %w", activeErr))
	}
	optOutProcessor := &consent.OptOutProcessor{Deliveries: deliveryEvents, Ledger: consentLedger, Policies: optOutPolicies, ActorID: consent.DefaultGatewayServiceActorID, Inbox: inboundReplies, Protector: protector, Senders: &postgresrepo.InboundSenderResolver{DB: db}}
	providerCapabilities := &provider.Service{Store: &provider.PostgreSQLStore{DB: db}}
	senderStore := &sender.PostgreSQLGovernanceStore{DB: db}
	proxyKeys, err := buildSenderProxyKeyring(cfg, true)
	if err != nil {
		return fail(fmt.Errorf("initialise sender proxy protection: %w", err))
	}
	senderProxies := &sender.SessionProxyAdministration{Store: senderStore, Keys: proxyKeys}
	gatewayPools := &sender.GatewayPoolService{Store: senderStore}
	gatewayPoolAdministration := &sender.GatewayPoolAdministration{Store: senderStore}
	gatewayRuntime := &sender.RuntimeRegistrationService{Store: senderStore, GatewayPools: senderStore, Secret: []byte(cfg.GatewayRuntimeSecret), PreviousSecrets: nonEmptySecrets(cfg.GatewayRuntimePreviousSecret), MaximumSkew: cfg.GatewayCallbackMaxSkew}
	platformPolicyStore := &platformpolicy.PostgreSQLStore{DB: db}
	configurations := &platformpolicy.ConfigurationAdministration{Store: platformPolicyStore}
	maintenance := &platformpolicy.MaintenanceAdministration{Store: platformPolicyStore}
	campaigns := campaign.NewService(&postgresrepo.CampaignRepository{DB: db}).WithOrganisationReader(orgs).WithOrganisationPolicies(organisationPolicies).WithCommercialApprovals(commercialService).WithConsentReviews(reviews).WithProviderCapabilities(providerCapabilities).WithGatewayPools(gatewayPools)
	messages := message.NewService(&message.PostgreSQLRepository{DB: db})
	testMessageRepository := &testmessage.PostgreSQLRepository{DB: db}
	testMessages := &testmessage.Service{Repository: testMessageRepository, Protector: protector, Messages: messages, Routes: testMessageRepository}
	snapshotStore := &segment.PostgreSQLStore{DB: db}
	snapshots := segment.NewService(snapshotStore)
	segmentDefinitions := &segment.DefinitionService{Repository: &segment.PostgreSQLDefinitionRepository{DB: db}, Registry: filters.Registry}
	cohortExecution := cohort.NewExecutionService(filters.Compiler, &cohort.PostgreSQLQueryRepository{DB: db})
	materialisationService := &materialisation.MaterialisationService{Repository: &materialisation.PostgreSQLRepository{DB: db}}
	metrics := delivery.NewMetricsService(&delivery.PostgreSQLMetricsRepository{DB: db})
	senderGovernance := &sender.GovernanceService{Store: senderStore}
	senderLifecycle := &sender.SessionLifecycleService{Governance: senderGovernance, Gateway: &sender.HTTPSessionGateway{CommandSecret: cfg.GatewayCommandSecret}, Proxies: senderProxies, ActiveWork: sender.PostgreSQLActiveSessionWorkChecker{DB: db}, Maintenance: maintenance}
	pacingPolicies := &sender.PacingAdministration{Store: &postgresrepo.PacingPolicyRepository{DB: db}}
	executionStore := &execution.PostgreSQLStore{DB: db}
	executionCoordinator := &execution.Coordinator{Campaigns: campaigns, Store: executionStore, SafetyMarginPercent: 15, Maintenance: maintenance}
	campaignWorkspace := &campaignworkspace.Service{Repository: &postgresrepo.CampaignWorkspaceRepository{DB: db}, Campaigns: campaigns, Metrics: campaignworkspace.MetricsReaderFunc(func(ctx context.Context, id string) (campaignworkspace.DeliveryMetrics, error) {
		m, err := executionStore.Metrics(ctx, id)
		return campaignworkspace.DeliveryMetrics{Authorised: m.Authorised, Queued: m.Queued, Pending: m.Pending, Submitted: m.Submitted, Sent: m.Sent, Delivered: m.Delivered, Read: m.Read, Unknown: m.Unknown}, err
	})}
	routingPlans := &execution.RoutingAdministration{Store: &execution.PostgreSQLRoutingPlanStore{DB: db}, Campaigns: campaigns, ProviderCapabilities: providerCapabilities, GatewayPools: gatewayPools}
	shardReallocations := &execution.ReallocationAdministration{Store: &execution.PostgreSQLShardRepository{DB: db}}
	executionCoordinator.RoutingPlans = routingPlans
	reportingPrivacy := &operations.ReportingPrivacyAdministration{Store: &operations.PostgreSQLReportingPrivacyStore{DB: db}, Audit: auditRecorder}
	alertStore := &operations.PostgreSQLAlertStore{DB: db}
	alertPolicies := &operations.AlertAdministration{Store: alertStore}
	operationsService := &operations.Service{Repo: &operations.PostgreSQLRepository{DB: db}, Audit: auditRecorder, AuditRepository: auditRepository, Deliveries: deliveryEvents, ReportingPrivacy: reportingPrivacy, Alerting: alertStore}
	alertEvaluator := &operations.AlertEvaluator{Store: alertStore, Dashboard: operationsService}
	platformRetentionStore := &retention.PostgreSQLStore{DB: db}
	platformRetention := &retention.Administration{Store: platformRetentionStore}
	jobOperations := &jobs.AdministrationService{Repository: &jobs.PostgreSQLRepository{DB: db}}
	imports := &importer.ImportService{Repository: &importer.PostgreSQLImportRepository{DB: db}, Organisations: orgs}
	importMappings := &importer.MappingAdministration{Store: &importer.PostgreSQLMappingStore{DB: db}, Audit: auditRecorder}
	importRollback := &importer.RollbackService{Repository: &importer.PostgreSQLRollbackRepository{DB: db}, Audit: auditRecorder}
	importIssues := &importer.IssueExportService{Repository: &importer.PostgreSQLIssueRepository{DB: db}, Audit: auditRecorder}
	contactLifecycle := &contactlife.Service{Repository: &contactlife.PostgreSQLRepository{DB: db}, Audit: auditRecorder}
	audienceConflicts := &importer.ConflictService{Repository: &importer.PostgreSQLConflictRepository{DB: db}}
	audienceReconciliation := &importer.ReconciliationService{Imports: imports, Conflicts: audienceConflicts, Repository: &importer.PostgreSQLReconciliationRepository{DB: db}}
	audienceSourceTrust := &importer.SourceTrustService{Repository: &importer.PostgreSQLSourceTrustRepository{DB: db}}
	mediaStore, err := storage.NewObjectStoreFromEnvironment(cfg.ObjectStoreRoot)
	if err != nil {
		return fail(fmt.Errorf("initialise media object store: %w", err))
	}
	trustedAssets, err := buildTrustedAssets(cfg, mediaStore, &storage.PostgreSQLAssetRepository{DB: db})
	if err != nil {
		return fail(err)
	}
	if trustedAssets == nil {
		return fail(errors.New("trusted asset intake is required for persistent runtime"))
	}
	messages.Assets = trustedMessageAssetResolver{service: trustedAssets, maximum: 64 << 20}
	reviews.Evidence = trustedConsentEvidenceResolver{service: trustedAssets}
	messages.LinkPolicies = message.StaticLinkPolicy{Hosts: cfg.MessageAllowedHosts, Version: "environment-v1"}
	intake, err := buildImportIntake(cfg, imports, mediaStore)
	if err != nil {
		return fail(err)
	}
	if intake == nil {
		return fail(errors.New("secure audience-import intake is required for persistent runtime"))
	}
	releases := &orchestration.ReleaseService{Campaigns: campaigns, Organisations: orgs, Snapshots: snapshots, Store: &orchestration.PostgreSQLStore{DB: db}, Eligibility: orchestration.SQLFinalEligibilityChecker{}, BatchSize: 1000, ShardCount: 256}
	deps := httpserver.Dependencies{Registry: filters.Registry, FilterDefinitions: filters.Administration, Compiler: filters.Compiler, Cohorts: cohortExecution, Organisations: orgs, OrganisationPolicies: organisationPolicies, ConsentReviews: reviews, ConsentLedger: consentLedger, OptOutProcessor: optOutProcessor, OptOutPolicies: optOutPolicies, InboundReplies: inboundReplies, InboundRetentionPolicies: retentionPolicies, InboundRotation: rotationService, Campaigns: campaigns, CampaignWorkspace: campaignWorkspace, Commercial: commercialService, Geography: geography.DefaultCatalogue(), MaxImportPreviewRows: cfg.MaxImportPreviewRows, Identity: identityService, IdentityAdministration: identityAdministration, SecureCookies: cfg.SecureCookies, NetworkPolicy: httpserver.NetworkPolicy{AllowedCIDRs: cfg.AllowedNetworkCIDRs, TrustedProxyCIDRs: cfg.TrustedProxyCIDRs}, MSISDNProtector: protector, Messages: messages, TestMessages: testMessages, Snapshots: snapshots, SegmentDefinitions: segmentDefinitions, AudienceMaterialisations: materialisationService, Releases: releases, DeliveryMetrics: metrics, Execution: executionCoordinator, RoutingPlans: routingPlans, ShardReallocations: shardReallocations, JobOperations: jobOperations, Operations: operationsService, PrivacyCases: privacyCases, ContactLifecycle: contactLifecycle, SenderGovernance: senderGovernance, SenderSessionProxies: senderProxies, GatewayPools: gatewayPoolAdministration, GatewayRuntime: gatewayRuntime, SenderSessionLifecycle: senderLifecycle, PacingPolicies: pacingPolicies, ProviderCapabilities: providerCapabilities, Configurations: configurations, Maintenance: maintenance, Retention: platformRetention, AlertPolicies: alertPolicies, AlertEvaluator: alertEvaluator, AudienceImports: imports, AudienceImportMappings: importMappings, AudienceImportRollback: importRollback, AudienceImportIssues: importIssues, AudienceConflicts: audienceConflicts, AudienceReconciliation: audienceReconciliation, AudienceSourceTrust: audienceSourceTrust, AudienceImportIntake: intake, MaxImportFileBytes: cfg.MaxImportFileBytes, DeliveryEvents: deliveryEvents, GatewayCallbackSecret: []byte(cfg.GatewayCallbackSecret), GatewayCallbackPreviousSecrets: nonEmptySecrets(cfg.GatewayCallbackPreviousSecret), GatewayCallbackMaxSkew: cfg.GatewayCallbackMaxSkew, MediaObjects: mediaStore, TrustedAssets: trustedAssets, MediaDownloadSecret: []byte(cfg.MediaDownloadSecret), ReadinessChecks: []httpserver.ReadinessCheck{{Name: "postgres", Check: db.PingContext}, {Name: "schema", Check: func(c context.Context) error { return verifyControlSchema(c, db) }}, filters.Readiness}}
	return &controlRuntime{Dependencies: deps, DB: db, close: db.Close}, nil
}

func buildPrivacyEvidenceKeyring(cfg config.Config, persistent bool) (*sharedcrypto.SecretKeyring, error) {
	if strings.TrimSpace(cfg.PrivacyEvidenceKeysJSON) != "" {
		return sharedcrypto.NewSecretKeyringFromJSON(cfg.PrivacyEvidenceActiveKey, cfg.PrivacyEvidenceKeysJSON)
	}
	if strings.TrimSpace(cfg.PrivacyEvidenceKeyBase64) != "" {
		return sharedcrypto.NewSecretKeyringFromJSON("v1", fmt.Sprintf(`{"v1":%q}`, cfg.PrivacyEvidenceKeyBase64))
	}
	if persistent {
		return nil, errors.New("persistent runtime requires PRIVACY_EVIDENCE_KEY_BASE64 or PRIVACY_EVIDENCE_KEYS_JSON")
	}
	return sharedcrypto.NewSecretKeyring("development-v1", map[string][]byte{"development-v1": []byte("development-privacy-key-32-byte!")})
}

func buildSenderProxyKeyring(cfg config.Config, persistent bool) (*sharedcrypto.SecretKeyring, error) {
	if strings.TrimSpace(cfg.SenderProxyKeysJSON) != "" {
		return sharedcrypto.NewSecretKeyringFromJSON(cfg.SenderProxyActiveKey, cfg.SenderProxyKeysJSON)
	}
	if persistent {
		return nil, errors.New("persistent runtime requires SENDER_PROXY_KEYS_JSON")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate ephemeral sender proxy key: %w", err)
	}
	return sharedcrypto.NewSecretKeyring("ephemeral-v1", map[string][]byte{"ephemeral-v1": key})
}

func buildImportIntake(cfg config.Config, service *importer.ImportService, store storage.ObjectStore) (*importer.IntakeService, error) {
	if strings.TrimSpace(cfg.ClamAVAddress) == "" {
		return nil, nil
	}
	if store == nil {
		return nil, errors.New("object storage is required for audience intake")
	}
	return &importer.IntakeService{Store: store, Imports: service, MaxFileSize: cfg.MaxImportFileBytes, DefaultSourceRetention: time.Duration(cfg.AudienceImportSourceRetentionDays) * 24 * time.Hour, Scanner: malware.ClamAVScanner{Address: cfg.ClamAVAddress, DialTimeout: cfg.ClamAVDialTimeout, ScanTimeout: cfg.ClamAVScanTimeout}}, nil
}

const controlSchemaReadinessQuery = `SELECT
  to_regclass('public.internal_mfa_challenges') IS NOT NULL
  AND to_regclass('public.attribute_definitions') IS NOT NULL
  AND to_regclass('public.audience_import_staging') IS NOT NULL
  AND to_regclass('public.delivery_events') IS NOT NULL
  AND to_regclass('public.campaign_metric_reconciliations') IS NOT NULL
  AND to_regclass('public.consent_grants') IS NOT NULL
  AND to_regclass('public.suppressions') IS NOT NULL
  AND to_regclass('public.consent_events') IS NOT NULL
  AND to_regclass('public.inbound_replies') IS NOT NULL
  AND to_regclass('public.opt_out_policies') IS NOT NULL
  AND to_regclass('public.inbound_retention_policies') IS NOT NULL
  AND to_regclass('public.sender_pools') IS NOT NULL
  AND to_regclass('public.sender_governance_events') IS NOT NULL
  AND to_regclass('public.campaign_capacity_assessments') IS NOT NULL
  AND to_regclass('public.campaign_execution_leases') IS NOT NULL
  AND to_regclass('public.operations_incidents') IS NOT NULL
  AND to_regclass('public.export_requests') IS NOT NULL
  AND to_regclass('public.export_download_grants') IS NOT NULL
  AND to_regclass('public.privacy_cases') IS NOT NULL
  AND to_regclass('public.privacy_legal_holds') IS NOT NULL
  AND to_regclass('public.privacy_legal_hold_events') IS NOT NULL
  AND to_regclass('public.reporting_privacy_policies') IS NOT NULL
  AND to_regclass('public.audience_import_mapping_definitions') IS NOT NULL
  AND to_regclass('public.audience_import_contact_mutations') IS NOT NULL
  AND to_regclass('public.audience_import_rollback_events') IS NOT NULL
  AND to_regclass('public.audience_import_source_deletion_events') IS NOT NULL
  AND to_regclass('public.contact_lifecycle_events') IS NOT NULL
  AND to_regclass('public.audience_profile_conflicts') IS NOT NULL
  AND to_regclass('public.organisation_policy_versions') IS NOT NULL
  AND to_regclass('public.campaign_commercial_approvals') IS NOT NULL
  AND to_regclass('public.audience_materialisation_jobs') IS NOT NULL
  AND to_regclass('public.sender_pacing_policies') IS NOT NULL
  AND to_regclass('public.sender_pacing_runtime') IS NOT NULL
  AND to_regclass('public.campaign_shard_reallocations') IS NOT NULL
  AND to_regclass('public.approved_test_recipients') IS NOT NULL
  AND to_regclass('public.test_message_sends') IS NOT NULL
  AND to_regclass('public.provider_capability_definitions') IS NOT NULL
  AND to_regclass('public.gateway_session_authorities') IS NOT NULL
  AND to_regclass('public.trusted_assets') IS NOT NULL
  AND to_regclass('public.consent_review_events') IS NOT NULL
  AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='campaigns' AND column_name='provider_capability_definition_id')
  AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='campaigns' AND column_name='provider_capability_definition_version')
  AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='campaigns' AND column_name='gateway_pool_version')
  AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='campaign_routing_plan_pools' AND column_name='provider_capability_definition_id')
  AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='campaign_routing_plan_pools' AND column_name='gateway_pool_version')
  AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='test_message_sends' AND column_name='gateway_pool_version')
  AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='test_message_sends' AND column_name='provider_adapter_version')
  AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='test_message_sends' AND column_name='provider_capability_definition_id')
  AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='contacts' AND column_name='lifecycle_version')
  AND EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='platform_configurations')
  AND EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='maintenance_windows')
  AND EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='gateway_runtime_events')
  AND EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='retention_policies')
  AND EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='operational_alert_policies')
  AND EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='operational_incident_events')
  AND EXISTS (SELECT 1 FROM pg_constraint WHERE conname='provider_capability_active_period_exclusion')`

func verifyControlSchema(ctx context.Context, db *sql.DB) error {
	var ready bool
	err := db.QueryRowContext(ctx, controlSchemaReadinessQuery).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return errors.New("required database migrations are not applied")
	}
	return nil
}

func bootstrapGatewayPools(ctx context.Context, service *sender.GatewayPoolService) error {
	definitions := []sender.GatewayPool{
		{ID: "00000000-0000-4000-8000-000000000201", Name: "OpenWA whatsapp-web.js", Provider: sender.GatewayProviderOpenWA, Engine: sender.GatewayEngineWhatsAppWebJS, AdapterVersion: "0.13.0", Status: sender.GatewayPoolActive, Capabilities: []sender.Capability{sender.CapabilitySendText, sender.CapabilitySendImage, sender.CapabilitySendVideo, sender.CapabilitySendDocument, sender.CapabilityDeliveryEvents, sender.CapabilityReadEvents, sender.CapabilityInboundMessages}, MinimumHealthyNodes: 1},
		{ID: "00000000-0000-4000-8000-000000000202", Name: "OpenWA Baileys", Provider: sender.GatewayProviderOpenWA, Engine: sender.GatewayEngineBaileys, AdapterVersion: "0.13.0", Status: sender.GatewayPoolActive, Capabilities: []sender.Capability{sender.CapabilitySendText, sender.CapabilitySendImage, sender.CapabilitySendVideo, sender.CapabilitySendDocument, sender.CapabilityDeliveryEvents, sender.CapabilityReadEvents, sender.CapabilityInboundMessages}, MinimumHealthyNodes: 1},
	}
	for _, definition := range definitions {
		if _, err := service.Create(ctx, definition, "00000000-0000-4000-8000-000000000001", "bootstrap governed OpenWA gateway pool"); err != nil {
			return err
		}
	}
	return nil
}

func bootstrapProviderCapabilities(ctx context.Context, store provider.Store, now time.Time) error {
	definitions := []provider.Definition{
		{ID: "00000000-0000-4000-8000-000000000101", Provider: "OPENWA", Channel: provider.ChannelWhatsApp, Engine: "WHATSAPP_WEB_JS", AdapterVersion: "0.13.0", Capabilities: []provider.Capability{provider.CapabilitySendText, provider.CapabilitySendImage, provider.CapabilitySendVideo, provider.CapabilitySendDocument, provider.CapabilityDeliveryEvents, provider.CapabilityReadEvents, provider.CapabilityInbound, provider.CapabilityPairingQR}, MaximumAttachmentBytes: 64 << 20, Status: provider.StatusActive, EffectiveFrom: now.Add(-time.Second), Version: 1, CreatedBy: "00000000-0000-4000-8000-000000000001", ApprovedBy: "00000000-0000-4000-8000-000000000001", Reason: "bootstrap OpenWA whatsapp-web.js capability definition", CreatedAt: now, UpdatedAt: now},
		{ID: "00000000-0000-4000-8000-000000000102", Provider: "OPENWA", Channel: provider.ChannelWhatsApp, Engine: "BAILEYS", AdapterVersion: "0.13.0", Capabilities: []provider.Capability{provider.CapabilitySendText, provider.CapabilitySendImage, provider.CapabilitySendVideo, provider.CapabilitySendDocument, provider.CapabilityDeliveryEvents, provider.CapabilityReadEvents, provider.CapabilityInbound, provider.CapabilityPairingQR, provider.CapabilityPairingCode}, MaximumAttachmentBytes: 64 << 20, Status: provider.StatusActive, EffectiveFrom: now.Add(-time.Second), Version: 1, CreatedBy: "00000000-0000-4000-8000-000000000001", ApprovedBy: "00000000-0000-4000-8000-000000000001", Reason: "bootstrap OpenWA Baileys capability definition", CreatedAt: now, UpdatedAt: now},
	}
	for _, definition := range definitions {
		if _, err := store.Create(ctx, definition); err != nil {
			return err
		}
	}
	return nil
}

type trustedConsentEvidenceResolver struct {
	service *storage.TrustedAssetService
}

func (r trustedConsentEvidenceResolver) ResolveConsentEvidence(ctx context.Context, identifier string) (consent.EvidenceAsset, error) {
	asset, err := r.service.ResolveClean(ctx, identifier, storage.AssetPurposeConsentEvidence, 128<<20)
	if err != nil {
		return consent.EvidenceAsset{}, err
	}
	return consent.EvidenceAsset{ID: asset.ID, ObjectKey: asset.ObjectKey}, nil
}

type trustedMessageAssetResolver struct {
	service *storage.TrustedAssetService
	maximum int64
}

func (r trustedMessageAssetResolver) ResolveMessageAsset(ctx context.Context, identifier string) (message.TrustedAsset, error) {
	asset, err := r.service.ResolveClean(ctx, identifier, storage.AssetPurposeMessageMedia, r.maximum)
	if err != nil {
		return message.TrustedAsset{}, err
	}
	return message.TrustedAsset{ID: asset.ID, ObjectKey: asset.ObjectKey, SHA256: asset.SHA256, MediaType: asset.MediaType, Size: asset.Size}, nil
}

func buildTrustedAssets(cfg config.Config, objects storage.ObjectStore, repository storage.AssetRepository) (*storage.TrustedAssetService, error) {
	if objects == nil || repository == nil {
		return nil, nil
	}
	if strings.TrimSpace(cfg.ClamAVAddress) == "" {
		return nil, nil
	}
	return &storage.TrustedAssetService{Objects: objects, Repository: repository, MaximumBytes: 128 << 20, Scanner: malware.ClamAVScanner{Address: cfg.ClamAVAddress, DialTimeout: cfg.ClamAVDialTimeout, ScanTimeout: cfg.ClamAVScanTimeout}}, nil
}

func nonEmptySecrets(values ...string) [][]byte {
	out := make([][]byte, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, []byte(value))
		}
	}
	return out
}
