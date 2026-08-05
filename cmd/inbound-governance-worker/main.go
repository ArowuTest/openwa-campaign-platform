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

	"campaign-platform/internal/inbound"
	"campaign-platform/internal/persistence/database"
	sharedcrypto "campaign-platform/internal/shared/crypto"
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
	cfg, err := workerconfig.LoadGovernance()
	if err != nil {
		logger.Error("inbound governance worker configuration is invalid", "error", err)
		os.Exit(1)
	}
	keyring, err := sharedcrypto.NewSecretKeyringFromJSON(cfg.InboundContentActiveKey, cfg.InboundContentKeysJSON)
	if err != nil {
		logger.Error("inbound keyring configuration is invalid", "error", err)
		os.Exit(1)
	}
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	db, err := database.Open(rootCtx, database.PoolConfig{Driver: cfg.DatabaseDriver, DSN: cfg.DatabaseURL, MaxOpen: cfg.DBMaxOpen, MaxIdle: cfg.DBMaxIdle, ConnMaxLifetime: cfg.DBConnMaxLifetime, ConnMaxIdleTime: cfg.DBConnMaxIdleTime, PingTimeout: cfg.DBPingTimeout})
	if err != nil {
		logger.Error("inbound governance database startup failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	replyRepo := &inbound.PostgreSQLRepository{DB: db, Keyring: keyring}
	rotationRepo := &inbound.PostgreSQLRotationRepository{DB: db, Keyring: keyring}
	rotationWorker := &inbound.RotationWorker{Repository: rotationRepo, Owner: cfg.WorkerID, BatchSize: cfg.RotationBatchSize, Lease: cfg.RotationLease, PollInterval: cfg.RotationPollInterval, OnError: func(run inbound.RotationRun, err error) {
		logger.Error("inbound content rotation failed", "runId", run.ID, "error", err)
	}}
	operations := &inbound.PostgreSQLGovernanceOperations{DB: db, Replies: replyRepo}
	health := workerruntime.NewHealth("inbound-governance-worker", db, rotationWorker.Active)
	healthServer := &http.Server{Addr: cfg.HealthAddr, Handler: health.Handler(), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 64 << 10}
	healthErrors := make(chan error, 1)
	go func() {
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			healthErrors <- err
		}
	}()
	workerErrors := make(chan error, 1)
	go func() { workerErrors <- rotationWorker.Run(rootCtx) }()
	retentionErrors := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(cfg.RetentionSweepInterval)
		defer ticker.Stop()
		for {
			count, err := operations.RetentionSweep(rootCtx, cfg.WorkerID, time.Now().UTC())
			if err != nil {
				logger.Error("inbound retention sweep failed", "error", err)
			} else {
				logger.Info("inbound retention sweep completed", "redactedCount", count)
			}
			select {
			case <-rootCtx.Done():
				retentionErrors <- nil
				return
			case <-ticker.C:
			}
		}
	}()
	health.SetReady(true)
	logger.Info("inbound governance worker started", "workerId", cfg.WorkerID, "activeKeyVersion", keyring.ActiveVersion())
	var runErr error
	select {
	case <-rootCtx.Done():
	case runErr = <-workerErrors:
		stop()
	case runErr = <-retentionErrors:
		stop()
	case runErr = <-healthErrors:
		stop()
	}
	health.SetReady(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	_ = healthServer.Shutdown(shutdownCtx)
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		logger.Error("inbound governance worker stopped with error", "error", runErr)
		os.Exit(1)
	}
	logger.Info("inbound governance worker stopped")
}
