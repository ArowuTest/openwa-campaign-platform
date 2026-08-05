package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/audience/importer"
	"campaign-platform/internal/audit"
	"campaign-platform/internal/campaign"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/delivery"
	"campaign-platform/internal/execution"
	"campaign-platform/internal/geography"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/inbound"
	"campaign-platform/internal/message"
	"campaign-platform/internal/operations"
	"campaign-platform/internal/orchestration"
	"campaign-platform/internal/organisation"
	"campaign-platform/internal/persistence/database"
	postgresrepo "campaign-platform/internal/persistence/postgres"
	"campaign-platform/internal/platform/httpserver"
	"campaign-platform/internal/security/malware"
	"campaign-platform/internal/segment"
	"campaign-platform/internal/sender"
	"campaign-platform/internal/shared/config"
	sharedcrypto "campaign-platform/internal/shared/crypto"
	"campaign-platform/internal/storage"
)

type inboundAuditSink struct{ recorder *audit.Recorder }

func (s inboundAuditSink) RecordInboundAudit(ctx context.Context, record inbound.AuditRecord) error {
	_, err := s.recorder.Record(ctx, audit.Input{ActorType: "USER", ActorID: record.ActorID, Action: record.Action, ObjectType: record.ObjectType, ObjectID: record.ObjectID, After: record.After, ReasonCode: record.ReasonCode, CorrelationID: record.CorrelationID})
	return err
}

type controlRuntime struct {
	Dependencies httpserver.Dependencies
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
	adminStore := identity.NewMemoryAdministrationRepository("SUPER_ADMIN", "CAMPAIGN_OPERATOR", "COMPLIANCE_REVIEWER", "CAMPAIGN_APPROVER", "ANALYST", "TECHNICAL_ADMIN")
	now := time.Now().UTC()
	_ = adminStore.CreateAccount(context.Background(), identity.Account{ID: admin.ID, Email: admin.Email, DisplayName: admin.DisplayName, Status: identity.StatusActive, MFARequired: true, RoleCodes: []string{"SUPER_ADMIN"}, Version: 1, CreatedAt: now, UpdatedAt: now}, hash, admin.TOTPSecret, admin.ID, "bootstrap administrator")
	memorySessions := identity.NewMemorySessionRepository()
	identityService := identity.NewPersistentServiceWithEvents(adminStore, memorySessions, identity.NewMemoryChallengeRepository(), identity.NewMemoryEventRecorder(), cfg.SessionIdleTimeout, cfg.SessionAbsoluteTimeout)
	identityAdministration := identity.NewAdministrationService(adminStore, memorySessions)
	orgs := organisation.NewService(organisation.NewMemoryRepository())
	reviews := consent.NewService(consent.NewMemoryRepository()).WithOrganisationReader(orgs)
	consentLedger := consent.NewLedgerService(consent.NewMemoryLedgerRepository())
	deliveryEvents := delivery.NewService(delivery.NewMemoryRepository())
	auditRepository := audit.NewMemoryRepository()
	auditRecorder := audit.NewRecorder(auditRepository)
	nowPolicy := time.Now().UTC()
	retentionStore := inbound.NewMemoryRetentionPolicyStore(inbound.RetentionPolicy{ID: "bootstrap-inbound-retention", RetentionDays: cfg.InboundRetentionDays, Status: inbound.RetentionPolicyActive, EffectiveFrom: nowPolicy.Add(-time.Second), Version: 1, CreatedBy: consent.DefaultGatewayServiceActorID, ApprovedBy: consent.DefaultGatewayServiceActorID, Reason: "bootstrap governed inbound retention policy", CreatedAt: nowPolicy, UpdatedAt: nowPolicy})
	retentionPolicies := &inbound.RetentionPolicyAdministration{Store: retentionStore}
	rotationService := &inbound.RotationService{Repository: inbound.NewMemoryRotationRepository(), ActiveKeyVersion: "v1"}
	inboundReplies := &inbound.Service{Repository: inbound.NewMemoryRepository(), Audit: inboundAuditSink{recorder: auditRecorder}, RetentionPolicies: retentionPolicies}
	policyStore := consent.NewMemoryOptOutPolicyStore(consent.GovernedOptOutPolicy{ID: "bootstrap-opt-out-policy", Keywords: cfg.OptOutKeywords, Status: consent.OptOutPolicyActive, EffectiveFrom: nowPolicy.Add(-time.Second), Version: 1, CreatedBy: consent.DefaultGatewayServiceActorID, Reason: "bootstrap governed opt-out policy", CreatedAt: nowPolicy, UpdatedAt: nowPolicy})
	optOutPolicies := &consent.OptOutPolicyAdministration{Store: policyStore}
	optOutProcessor := &consent.OptOutProcessor{Deliveries: deliveryEvents, Ledger: consentLedger, Policies: optOutPolicies, ActorID: consent.DefaultGatewayServiceActorID, Inbox: inboundReplies}
	campaigns := campaign.NewService(campaign.NewMemoryRepository()).WithOrganisationReader(orgs)
	messages := message.NewService(message.NewMemoryRepository())
	snapshots := segment.NewService(segment.NewMemoryRepository())
	metrics := delivery.NewMetricsService(delivery.NewMemoryMetricsRepository())
	senderGovernance := &sender.GovernanceService{Store: sender.NewMemoryGovernanceStore()}
	executionStore := execution.NewMemoryStore()
	executionCoordinator := &execution.Coordinator{Campaigns: campaigns, Store: executionStore, SafetyMarginPercent: 15}
	operationsService := &operations.Service{Repo: operations.NewMemoryRepository(), Audit: auditRecorder, AuditRepository: auditRepository}
	imports := &importer.ImportService{Repository: importer.NewMemoryImportRepository(), Organisations: orgs}
	mediaStore, err := storage.NewFileSystemStore(cfg.ObjectStoreRoot)
	if err != nil {
		return nil, fmt.Errorf("initialise media object store: %w", err)
	}
	intake, err := buildImportIntake(cfg, imports)
	if err != nil {
		return nil, err
	}
	if intake == nil {
		logger.Warn("secure audience-import intake is disabled until CLAMAV_ADDRESS is configured")
	}
	deps := httpserver.Dependencies{Registry: filters.Registry, FilterDefinitions: filters.Administration, Compiler: filters.Compiler, Organisations: orgs, ConsentReviews: reviews, ConsentLedger: consentLedger, OptOutProcessor: optOutProcessor, OptOutPolicies: optOutPolicies, InboundReplies: inboundReplies, InboundRetentionPolicies: retentionPolicies, InboundRotation: rotationService, Campaigns: campaigns, Geography: geography.DefaultCatalogue(), MaxImportPreviewRows: cfg.MaxImportPreviewRows, Identity: identityService, IdentityAdministration: identityAdministration, SecureCookies: cfg.SecureCookies, NetworkPolicy: httpserver.NetworkPolicy{AllowedCIDRs: cfg.AllowedNetworkCIDRs, TrustedProxyCIDRs: cfg.TrustedProxyCIDRs}, MSISDNProtector: protector, Messages: messages, Snapshots: snapshots, DeliveryMetrics: metrics, Execution: executionCoordinator, Operations: operationsService, SenderGovernance: senderGovernance, AudienceImports: imports, AudienceImportIntake: intake, MaxImportFileBytes: cfg.MaxImportFileBytes, DeliveryEvents: deliveryEvents, GatewayCallbackSecret: []byte(cfg.GatewayCallbackSecret), GatewayCallbackMaxSkew: cfg.GatewayCallbackMaxSkew, MediaObjects: mediaStore, MediaDownloadSecret: []byte(cfg.MediaDownloadSecret), ReadinessChecks: []httpserver.ReadinessCheck{filters.Readiness}}
	return &controlRuntime{Dependencies: deps}, nil
}

func buildPostgreSQLRuntime(ctx context.Context, cfg config.Config, protector *sharedcrypto.MSISDNProtector, logger *slog.Logger) (*controlRuntime, error) {
	db, err := database.Open(ctx, database.PoolConfig{Driver: cfg.DatabaseDriver, DSN: cfg.DatabaseURL, MaxOpen: cfg.DatabaseMaxOpen, MaxIdle: cfg.DatabaseMaxIdle, ConnMaxLifetime: cfg.DatabaseConnMaxLifetime, ConnMaxIdleTime: cfg.DatabaseConnMaxIdleTime, PingTimeout: cfg.DatabasePingTimeout})
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
	optOutProcessor := &consent.OptOutProcessor{Deliveries: deliveryEvents, Ledger: consentLedger, Policies: optOutPolicies, ActorID: consent.DefaultGatewayServiceActorID, Inbox: inboundReplies}
	campaigns := campaign.NewService(&postgresrepo.CampaignRepository{DB: db}).WithOrganisationReader(orgs)
	messages := message.NewService(&message.PostgreSQLRepository{DB: db})
	snapshotStore := &segment.PostgreSQLStore{DB: db}
	snapshots := segment.NewService(snapshotStore)
	metrics := delivery.NewMetricsService(&delivery.PostgreSQLMetricsRepository{DB: db})
	senderGovernance := &sender.GovernanceService{Store: &sender.PostgreSQLGovernanceStore{DB: db}}
	executionCoordinator := &execution.Coordinator{Campaigns: campaigns, Store: &execution.PostgreSQLStore{DB: db}, SafetyMarginPercent: 15}
	operationsService := &operations.Service{Repo: &operations.PostgreSQLRepository{DB: db}, Audit: auditRecorder, AuditRepository: auditRepository}
	imports := &importer.ImportService{Repository: &importer.PostgreSQLImportRepository{DB: db}, Organisations: orgs}
	mediaStore, err := storage.NewFileSystemStore(cfg.ObjectStoreRoot)
	if err != nil {
		return fail(fmt.Errorf("initialise media object store: %w", err))
	}
	intake, err := buildImportIntake(cfg, imports)
	if err != nil {
		return fail(err)
	}
	if intake == nil {
		return fail(errors.New("secure audience-import intake is required for persistent runtime"))
	}
	releases := &orchestration.ReleaseService{Campaigns: campaigns, Organisations: orgs, Snapshots: snapshots, Store: &orchestration.PostgreSQLStore{DB: db}, Eligibility: orchestration.SQLFinalEligibilityChecker{}, BatchSize: 1000, ShardCount: 256}
	deps := httpserver.Dependencies{Registry: filters.Registry, FilterDefinitions: filters.Administration, Compiler: filters.Compiler, Organisations: orgs, ConsentReviews: reviews, ConsentLedger: consentLedger, OptOutProcessor: optOutProcessor, OptOutPolicies: optOutPolicies, InboundReplies: inboundReplies, InboundRetentionPolicies: retentionPolicies, InboundRotation: rotationService, Campaigns: campaigns, Geography: geography.DefaultCatalogue(), MaxImportPreviewRows: cfg.MaxImportPreviewRows, Identity: identityService, IdentityAdministration: identityAdministration, SecureCookies: cfg.SecureCookies, NetworkPolicy: httpserver.NetworkPolicy{AllowedCIDRs: cfg.AllowedNetworkCIDRs, TrustedProxyCIDRs: cfg.TrustedProxyCIDRs}, MSISDNProtector: protector, Messages: messages, Snapshots: snapshots, Releases: releases, DeliveryMetrics: metrics, Execution: executionCoordinator, Operations: operationsService, SenderGovernance: senderGovernance, AudienceImports: imports, AudienceImportIntake: intake, MaxImportFileBytes: cfg.MaxImportFileBytes, DeliveryEvents: deliveryEvents, GatewayCallbackSecret: []byte(cfg.GatewayCallbackSecret), GatewayCallbackMaxSkew: cfg.GatewayCallbackMaxSkew, MediaObjects: mediaStore, MediaDownloadSecret: []byte(cfg.MediaDownloadSecret), ReadinessChecks: []httpserver.ReadinessCheck{{Name: "postgres", Check: db.PingContext}, {Name: "schema", Check: func(c context.Context) error { return verifyControlSchema(c, db) }}, filters.Readiness}}
	return &controlRuntime{Dependencies: deps, close: db.Close}, nil
}

func buildImportIntake(cfg config.Config, service *importer.ImportService) (*importer.IntakeService, error) {
	if strings.TrimSpace(cfg.ClamAVAddress) == "" {
		return nil, nil
	}
	store, err := storage.NewFileSystemStore(cfg.ObjectStoreRoot)
	if err != nil {
		return nil, err
	}
	return &importer.IntakeService{Store: store, Imports: service, MaxFileSize: cfg.MaxImportFileBytes, Scanner: malware.ClamAVScanner{Address: cfg.ClamAVAddress, DialTimeout: cfg.ClamAVDialTimeout, ScanTimeout: cfg.ClamAVScanTimeout}}, nil
}
func verifyControlSchema(ctx context.Context, db *sql.DB) error {
	var ready bool
	err := db.QueryRowContext(ctx, `SELECT to_regclass('public.internal_mfa_challenges') IS NOT NULL AND to_regclass('public.attribute_definitions') IS NOT NULL AND to_regclass('public.audience_import_staging') IS NOT NULL AND to_regclass('public.delivery_provider_events') IS NOT NULL AND to_regclass('public.campaign_metric_reconciliation') IS NOT NULL AND to_regclass('public.consent_grants') IS NOT NULL AND to_regclass('public.suppressions') IS NOT NULL AND to_regclass('public.consent_events') IS NOT NULL AND to_regclass('public.inbound_replies') IS NOT NULL AND to_regclass('public.opt_out_policies') IS NOT NULL AND to_regclass('public.inbound_retention_policies') IS NOT NULL AND to_regclass('public.sender_pools') IS NOT NULL AND to_regclass('public.sender_governance_events') IS NOT NULL AND to_regclass('public.campaign_capacity_assessments') IS NOT NULL AND to_regclass('public.campaign_execution_leases') IS NOT NULL AND to_regclass('public.operations_incidents') IS NOT NULL AND to_regclass('public.export_requests') IS NOT NULL`).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return errors.New("required database migrations are not applied")
	}
	return nil
}
