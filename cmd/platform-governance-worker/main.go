package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"campaign-platform/internal/observability"
	"campaign-platform/internal/operations"
	"campaign-platform/internal/persistence/database"
	"campaign-platform/internal/retention"
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

	logger := observability.NewLogger(os.Stdout, "platform-governance-worker", os.Getenv("APP_ENV"))
	cfg, err := workerconfig.LoadPlatformGovernance()
	if err != nil {
		logger.Error("platform governance worker configuration is invalid", "error", err)
		os.Exit(1)
	}
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(rootCtx, database.PoolConfig{
		Driver: cfg.DatabaseDriver, DSN: cfg.DatabaseURL,
		MaxOpen: cfg.DBMaxOpen, MaxIdle: cfg.DBMaxIdle,
		ConnMaxLifetime: cfg.DBConnMaxLifetime, ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
		PingTimeout: cfg.DBPingTimeout, Environment: cfg.Environment, ServiceName: "platform-governance-worker",
	})
	if err != nil {
		logger.Error("platform governance database startup failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	objects, err := storage.NewObjectStoreFromEnvironment(cfg.ObjectStoreRoot)
	if err != nil {
		logger.Error("platform governance object-store startup failed", "error", err)
		os.Exit(1)
	}

	retentionStore := &retention.PostgreSQLStore{DB: db}
	retentionWorker := &retention.Worker{
		Store: retentionStore, Executor: &retention.PostgreSQLExecutor{DB: db, Objects: objects},
		WorkerID: cfg.WorkerID, Lease: cfg.RetentionLease, Batch: cfg.RetentionBatch,
		PollInterval: cfg.RetentionPollInterval,
	}
	operationsRepo := &operations.PostgreSQLRepository{DB: db}
	operationsService := &operations.Service{Repo: operationsRepo}
	alertStore := &operations.PostgreSQLAlertStore{DB: db}
	alertEvaluator := &operations.AlertEvaluator{
		Store: alertStore, Dashboard: operationsService, WorkerID: cfg.WorkerID,
		Lease: cfg.AlertEscalationLease,
	}

	var active atomic.Int64
	health := workerruntime.NewHealth("platform-governance-worker", db, active.Load)
	healthServer := &http.Server{
		Addr: cfg.HealthAddr, Handler: health.Handler(), ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 64 << 10,
	}
	healthErrors := make(chan error, 1)
	go func() {
		if serveErr := healthServer.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			healthErrors <- serveErr
		}
	}()

	workerErrors := make(chan error, 3)
	go func() {
		workerErrors <- runPeriodic(rootCtx, cfg.RetentionPollInterval, cfg.FailureBackoff, &active, logger, "retention", func(ctx context.Context) (int, error) {
			return retentionWorker.Process(ctx)
		})
	}()
	go func() {
		workerErrors <- runPeriodic(rootCtx, cfg.AlertEvaluationInterval, cfg.FailureBackoff, &active, logger, "alert-evaluation", func(ctx context.Context) (int, error) {
			return alertEvaluator.Evaluate(ctx)
		})
	}()
	go func() {
		workerErrors <- runPeriodic(rootCtx, cfg.AlertEscalationInterval, cfg.FailureBackoff, &active, logger, "alert-escalation", func(ctx context.Context) (int, error) {
			return alertEvaluator.Escalate(ctx, cfg.AlertEscalationBatch)
		})
	}()

	health.SetReady(true)
	logger.Info("platform governance worker started", "workerId", cfg.WorkerID)
	var runErr error
	select {
	case <-rootCtx.Done():
	case runErr = <-workerErrors:
		stop()
	case runErr = <-healthErrors:
		stop()
	}

	health.SetReady(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	_ = healthServer.Shutdown(shutdownCtx)
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		logger.Error("platform governance worker stopped with error", "error", runErr)
		os.Exit(1)
	}
	logger.Info("platform governance worker stopped")
}

func runPeriodic(ctx context.Context, interval, failureBackoff time.Duration, active *atomic.Int64, logger *slog.Logger, operation string, run func(context.Context) (int, error)) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		active.Add(1)
		count, err := run(ctx)
		active.Add(-1)
		next := interval
		if err != nil && ctx.Err() == nil {
			logger.Error("platform governance operation failed", "operation", operation, "error", err)
			next = failureBackoff
		} else if count > 0 {
			logger.Info("platform governance operation completed", "operation", operation, "processed", count)
		}
		if next <= 0 {
			next = time.Second
		}
		timer.Reset(next)
	}
}
