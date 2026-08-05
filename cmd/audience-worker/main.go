package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"campaign-platform/internal/audience/importer"
	"campaign-platform/internal/geography"
	"campaign-platform/internal/persistence/database"
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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
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
		PingTimeout: cfg.DBPingTimeout,
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
	objectStore, err := storage.NewFileSystemStore(cfg.ObjectStoreRoot)
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

	health := workerruntime.NewHealth("audience-worker", db, worker.Active)
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

	workerErrors := make(chan error, 1)
	go func() { workerErrors <- worker.Run(rootCtx) }()
	logger.Info("audience validation worker started",
		"workerId", cfg.WorkerID, "concurrency", cfg.Concurrency,
		"claimBatch", cfg.ClaimBatch, "leaseDuration", cfg.ValidationLeaseDuration.String())

	var runErr error
	workerStopped := false
	select {
	case <-rootCtx.Done():
	case runErr = <-workerErrors:
		workerStopped = true
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
	if !workerStopped {
		select {
		case err := <-workerErrors:
			if runErr == nil {
				runErr = err
			}
		case <-shutdownCtx.Done():
			logger.Error("audience worker shutdown timed out", "active", worker.Active())
			os.Exit(1)
		}
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		logger.Error("audience worker stopped with error", "error", runErr)
		os.Exit(1)
	}
	logger.Info("audience validation worker stopped", "active", worker.Active())
}
