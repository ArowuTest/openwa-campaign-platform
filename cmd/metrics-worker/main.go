package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"campaign-platform/internal/delivery/reconciliation"
	"campaign-platform/internal/observability"
	"campaign-platform/internal/persistence/database"
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

	logger := observability.NewLogger(os.Stdout, "metrics-worker", os.Getenv("APP_ENV"))
	cfg, err := workerconfig.LoadMetrics()
	if err != nil {
		logger.Error("metrics worker configuration is invalid", "error", err)
		os.Exit(1)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	db, err := database.Open(rootCtx, database.PoolConfig{
		Driver: cfg.DatabaseDriver, DSN: cfg.DatabaseURL,
		MaxOpen: cfg.DBMaxOpen, MaxIdle: cfg.DBMaxIdle,
		ConnMaxLifetime: cfg.DBConnMaxLifetime, ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
		PingTimeout: cfg.DBPingTimeout, Environment: cfg.Environment, ServiceName: "metrics-worker",
	})
	if err != nil {
		logger.Error("metrics worker database startup failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	worker := &reconciliation.Worker{
		Repository:      &reconciliation.PostgreSQLRepository{DB: db},
		Calculator:      &reconciliation.PostgreSQLCalculator{DB: db},
		Owner:           cfg.WorkerID,
		Concurrency:     cfg.Concurrency,
		ClaimBatch:      cfg.ClaimBatch,
		Lease:           cfg.Lease,
		PollInterval:    cfg.PollInterval,
		MatchInterval:   cfg.MatchInterval,
		DriftInterval:   cfg.DriftInterval,
		FailureInterval: cfg.FailureInterval,
		ShutdownGrace:   cfg.ShutdownTimeout,
		OnDrift: func(observation reconciliation.Observation) {
			logger.Warn("campaign metric drift detected",
				"campaignId", observation.CampaignID,
				"canonicalTotal", observation.CanonicalTotal,
				"storedTotal", observation.StoredTotal,
				"observedAt", observation.ObservedAt)
		},
		OnError: func(work reconciliation.Work, err error) {
			logger.Error("campaign metric reconciliation failed", "campaignId", work.CampaignID, "error", err)
		},
	}

	health := workerruntime.NewHealth("metrics-worker", db, worker.Active)
	healthServer := &http.Server{
		Addr: cfg.HealthAddr, Handler: health.Handler(),
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 64 << 10,
	}
	healthErrors := make(chan error, 1)
	go func() {
		logger.Info("metrics worker health server starting", "address", cfg.HealthAddr)
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			healthErrors <- err
		}
	}()

	workerErrors := make(chan error, 1)
	go func() { workerErrors <- worker.Run(rootCtx) }()
	outcomeWorker := &reconciliation.OutcomeWorker{
		Repository:           &reconciliation.PostgreSQLOutcomeRepository{DB: db},
		ReconciliationWindow: cfg.OutcomeReconcileWindow, FinalUnknownWindow: cfg.FinalUnknownWindow,
		BatchSize: cfg.OutcomeReconcileBatch,
	}
	outcomeErrors := make(chan error, 1)
	if n, err := drainOutcomeWorker(rootCtx, outcomeWorker, cfg.OutcomeReconcileBatch); err != nil {
		logger.Error("initial delivery outcome reconciliation failed", "error", err)
		outcomeErrors <- err
	} else {
		if n > 0 {
			logger.Warn("delivery outcomes reconciled before readiness", "count", n)
		}
		go func() {
			ticker := time.NewTicker(cfg.OutcomeReconcileInterval)
			defer ticker.Stop()
			for {
				select {
				case <-rootCtx.Done():
					return
				case <-ticker.C:
					n, err := drainOutcomeWorker(rootCtx, outcomeWorker, cfg.OutcomeReconcileBatch)
					if err != nil {
						logger.Error("delivery outcome reconciliation failed", "error", err)
						outcomeErrors <- err
						return
					}
					if n > 0 {
						logger.Warn("delivery outcomes reconciled", "count", n)
					}
				}
			}
		}()
		health.SetReady(true)
		logger.Info("metrics reconciliation worker started",
			"workerId", cfg.WorkerID,
			"concurrency", cfg.Concurrency,
			"claimBatch", cfg.ClaimBatch,
			"outcomeBatch", cfg.OutcomeReconcileBatch,
			"lease", cfg.Lease.String())
	}

	var runErr error
	workerStopped := false
	select {
	case <-rootCtx.Done():
	case runErr = <-workerErrors:
		workerStopped = true
		stop()
	case runErr = <-outcomeErrors:
		stop()
	case runErr = <-healthErrors:
		stop()
	}
	health.SetReady(false)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("metrics worker health shutdown failed", "error", err)
	}
	if !workerStopped {
		select {
		case err := <-workerErrors:
			if runErr == nil {
				runErr = err
			}
		case <-shutdownCtx.Done():
			logger.Error("metrics worker shutdown timed out", "active", worker.Active())
			os.Exit(1)
		}
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		logger.Error("metrics worker stopped with error", "error", runErr)
		os.Exit(1)
	}
	logger.Info("metrics reconciliation worker stopped", "active", worker.Active())
}
