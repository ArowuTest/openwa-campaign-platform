package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/audience/importer"
	"campaign-platform/internal/audience/materialisation"
	"campaign-platform/internal/geography"
	"campaign-platform/internal/observability"
	"campaign-platform/internal/persistence/database"
	postgresrepo "campaign-platform/internal/persistence/postgres"
	"campaign-platform/internal/segment"
	sharedcrypto "campaign-platform/internal/shared/crypto"
	"campaign-platform/internal/storage"
	workerconfig "campaign-platform/internal/worker/config"
	workerruntime "campaign-platform/internal/worker/runtime"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "healthcheck" {
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get(os.Args[2])
		if err != nil || response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		_ = response.Body.Close()
		return
	}
	logger := observability.NewLogger(os.Stdout, "audience-worker", os.Getenv("APP_ENV"))
	cfg, err := workerconfig.LoadAudience()
	if err != nil {
		logger.Error("audience worker configuration is invalid", "error", err)
		os.Exit(1)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	db, err := database.Open(rootCtx, database.PoolConfig{
		Driver: cfg.DatabaseDriver, DSN: cfg.DatabaseURL,
		MaxOpen: cfg.DBMaxOpen, MaxIdle: cfg.DBMaxIdle,
		ConnMaxLifetime: cfg.DBConnMaxLifetime, ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
		PingTimeout: cfg.DBPingTimeout, Environment: cfg.Environment, ServiceName: "audience-worker",
	})
	if err != nil {
		logger.Error("audience worker database startup failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	protector, err := sharedcrypto.NewMSISDNProtectorFromBase64(cfg.MSISDNEncryptionKey, cfg.MSISDNLookupKey)
	if err != nil {
		logger.Error("audience worker MSISDN protection startup failed", "error", err)
		os.Exit(1)
	}
	objectStore, err := storage.NewObjectStoreFromEnvironment(cfg.ObjectStoreRoot)
	if err != nil {
		logger.Error("audience worker object storage startup failed", "error", err)
		os.Exit(1)
	}

	staging := &importer.PostgreSQLStagingRepository{
		DB: db, WorkerID: cfg.WorkerID, LeaseDuration: cfg.ValidationLeaseDuration,
	}
	catalogue := geography.DefaultCatalogue()
	worker := &importer.ValidationWorker{
		Claimer: staging,
		Staging: staging,
		Store:   objectStore,
		Ingest:  &importer.IngestService{Repository: staging, BatchSize: cfg.StageBatchSize},
		Options: importer.PreviewOptions{
			DefaultCountryISO2: cfg.DefaultCountryISO2,
			MaxRows:            cfg.MaxRows,
			MaxIssues:          cfg.MaxIssues,
			MaxCandidateSample: 1,
			Protector:          protector,
			GeographyValidator: catalogue.Validate,
		},
		Concurrency:           cfg.Concurrency,
		ClaimBatch:            cfg.ClaimBatch,
		PollInterval:          cfg.PollInterval,
		ClaimFailureBackoff:   cfg.ClaimFailureBackoff,
		MaximumFailureBackoff: cfg.MaximumFailureBackoff,
		OnError: func(work importer.ValidationWork, err error) {
			logger.Error("audience validation work failed", "importId", work.ImportID, "error", err)
		},
	}

	filterDefinitions, err := (&postgresrepo.FilterDefinitionStore{DB: db}).List(rootCtx)
	if err != nil {
		logger.Error("audience worker filter registry startup failed", "error", err)
		os.Exit(1)
	}
	registry, err := audiencefilter.NewRegistry(filterDefinitions...)
	if err != nil {
		logger.Error("audience worker filter registry is invalid", "error", err)
		os.Exit(1)
	}
	cohortExecution := cohort.NewExecutionService(cohort.NewCompiler(registry), &cohort.PostgreSQLQueryRepository{DB: db})

	mergeRepository := &importer.PostgreSQLMergeRepository{DB: db}
	mergeWorker := &importer.MergeWorker{
		Repository:     mergeRepository,
		Merger:         &importer.MergeService{Repository: mergeRepository},
		WorkerID:       cfg.WorkerID + "-merge",
		Concurrency:    cfg.MergeConcurrency,
		ClaimBatch:     cfg.MergeClaimBatch,
		LeaseDuration:  cfg.MergeLeaseDuration,
		PollInterval:   cfg.MergePollInterval,
		FailureBackoff: cfg.MergeFailureBackoff,
		OnError: func(work importer.MergeWork, err error) {
			logger.Error("audience merge work failed", "importId", work.ImportID, "error", err)
		},
	}

	sourceRetentionWorker := &importer.SourceRetentionWorker{
		Repository:    &importer.PostgreSQLSourceRetentionRepository{DB: db},
		Objects:       objectStore,
		WorkerID:      cfg.WorkerID + "-source-retention",
		LeaseDuration: cfg.SourceRetentionLeaseDuration,
		PollInterval:  cfg.SourceRetentionPollInterval,
		BatchSize:     cfg.SourceRetentionBatchSize,
	}
	materialisationWorker := &materialisation.MaterialisationWorker{
		Repository:    &materialisation.PostgreSQLRepository{DB: db},
		Cohorts:       cohortExecution,
		Snapshots:     &segment.PostgreSQLStore{DB: db},
		WorkerID:      cfg.WorkerID + "-materialisation",
		BatchSize:     cfg.MaterialisationBatchSize,
		ClaimBatch:    cfg.MaterialisationClaimBatch,
		LeaseDuration: cfg.MaterialisationLeaseDuration,
		PollInterval:  cfg.MaterialisationPollInterval,
		OnError: func(job materialisation.MaterialisationJob, err error) {
			logger.Error("audience materialisation work failed", "jobId", job.ID, "campaignId", job.CampaignID, "error", err)
		},
	}

	health := workerruntime.NewHealth("audience-worker", db, func() int64 {
		return worker.Active() + materialisationWorker.Active() + mergeWorker.Active() + sourceRetentionWorker.Active()
	})
	healthServer := &http.Server{
		Addr: cfg.HealthAddr, Handler: health.Handler(),
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 64 << 10,
	}
	healthErrors := make(chan error, 1)
	go func() {
		logger.Info("audience worker health server starting", "address", cfg.HealthAddr)
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			healthErrors <- err
		}
	}()
	health.SetReady(true)

	type workerResult struct {
		name string
		err  error
	}
	workerErrors := make(chan workerResult, 4)
	go func() { workerErrors <- workerResult{name: "validation", err: worker.Run(rootCtx)} }()
	go func() { workerErrors <- workerResult{name: "materialisation", err: materialisationWorker.Run(rootCtx)} }()
	go func() { workerErrors <- workerResult{name: "merge", err: mergeWorker.Run(rootCtx)} }()
	go func() {
		workerErrors <- workerResult{name: "source-retention", err: sourceRetentionWorker.Run(rootCtx)}
	}()
	logger.Info("audience workers started",
		"workerId", cfg.WorkerID, "validationConcurrency", cfg.Concurrency,
		"validationClaimBatch", cfg.ClaimBatch, "validationLeaseDuration", cfg.ValidationLeaseDuration.String(),
		"materialisationBatchSize", cfg.MaterialisationBatchSize, "materialisationClaimBatch", cfg.MaterialisationClaimBatch,
		"mergeConcurrency", cfg.MergeConcurrency, "mergeClaimBatch", cfg.MergeClaimBatch,
		"sourceRetentionBatchSize", cfg.SourceRetentionBatchSize, "sourceRetentionPollInterval", cfg.SourceRetentionPollInterval.String())

	var runErr error
	stoppedWorkers := 0
	select {
	case <-rootCtx.Done():
	case result := <-workerErrors:
		stoppedWorkers++
		runErr = result.err
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			logger.Error("audience worker component stopped", "component", result.name, "error", runErr)
		}
		stop()
	case runErr = <-healthErrors:
		stop()
	}
	health.SetReady(false)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("audience worker health shutdown failed", "error", err)
	}
	for stoppedWorkers < 4 {
		select {
		case result := <-workerErrors:
			stoppedWorkers++
			if runErr == nil && result.err != nil && !errors.Is(result.err, context.Canceled) {
				runErr = result.err
			}
		case <-shutdownCtx.Done():
			logger.Error("audience worker shutdown timed out", "validationActive", worker.Active(), "materialisationActive", materialisationWorker.Active(), "mergeActive", mergeWorker.Active(), "sourceRetentionActive", sourceRetentionWorker.Active())
			os.Exit(1)
		}
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		logger.Error("audience worker stopped with error", "error", runErr)
		os.Exit(1)
	}
	logger.Info("audience workers stopped", "validationActive", worker.Active(), "materialisationActive", materialisationWorker.Active(), "mergeActive", mergeWorker.Active(), "sourceRetentionActive", sourceRetentionWorker.Active())
}
