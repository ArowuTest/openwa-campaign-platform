package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"campaign-platform/internal/audit"
	"campaign-platform/internal/observability"
	"campaign-platform/internal/operations"
	"campaign-platform/internal/persistence/database"
	"campaign-platform/internal/privacy"
	sharedcrypto "campaign-platform/internal/shared/crypto"
	"campaign-platform/internal/shared/envfile"
	"campaign-platform/internal/storage"
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

	environment := strings.ToLower(strings.TrimSpace(envOrDefault("APP_ENV", "development")))
	if err := envfile.Resolve(environment,
		"DATABASE_URL", "PRIVACY_EVIDENCE_KEY_BASE64", "PRIVACY_EVIDENCE_KEYS_JSON",
		"S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "S3_SESSION_TOKEN", "PROFILING_TOKEN"); err != nil {
		fmt.Fprintln(os.Stderr, "export worker secret-file configuration is invalid:", err)
		os.Exit(2)
	}
	logger := observability.NewLogger(os.Stdout, "export-worker", environment)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(2)
	}
	driver := strings.TrimSpace(envOrDefault("POSTGRES_DRIVER", "postgres"))
	root := strings.TrimSpace(os.Getenv("OBJECT_STORE_ROOT"))
	if root == "" && environment == "development" {
		root = "./var/objects"
	}
	workerID := strings.TrimSpace(envOrDefault("EXPORT_WORKER_ID", "export-worker-1"))
	healthAddr := strings.TrimSpace(envOrDefault("WORKER_HEALTH_ADDR", ":8093"))
	poll, err := strictEnvDuration("EXPORT_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		logger.Error("invalid export polling configuration", "error", err)
		os.Exit(2)
	}
	expireEvery, err := strictEnvDuration("EXPORT_EXPIRY_INTERVAL", time.Minute)
	if err != nil {
		logger.Error("invalid export expiry configuration", "error", err)
		os.Exit(2)
	}
	readyTTL, err := strictEnvDuration("EXPORT_READY_TTL", 24*time.Hour)
	if err != nil {
		logger.Error("invalid export retention configuration", "error", err)
		os.Exit(2)
	}
	shutdownTimeout, err := strictEnvDuration("WORKER_SHUTDOWN_TIMEOUT", 30*time.Second)
	if err != nil {
		logger.Error("invalid shutdown configuration", "error", err)
		os.Exit(2)
	}

	db, err := database.Open(ctx, database.PoolConfig{
		Driver: driver, DSN: dsn, MaxOpen: 4, MaxIdle: 2, PingTimeout: 10 * time.Second,
		Environment: environment, ServiceName: "export-worker",
	})
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	objects, err := storage.NewObjectStoreFromEnvironment(root)
	if err != nil {
		logger.Error("open object store", "error", err)
		os.Exit(1)
	}
	repo := &operations.PostgreSQLRepository{DB: db}
	privacyKeyring, err := privacyEvidenceKeyringFromEnvironment()
	if err != nil {
		logger.Error("initialise privacy evidence keyring", "error", err)
		os.Exit(2)
	}
	privacyResolver := &privacy.Service{Evidence: privacyKeyring}
	worker := &operations.ExportWorker{
		Repository: repo, AuditRepository: &audit.PostgreSQLRepository{DB: db},
		PrivacyPackages: privacyResolver, Objects: objects, WorkerID: workerID,
		LeaseDuration: 2 * time.Minute, ReadyTTL: readyTTL,
	}

	var active atomic.Int64
	health := workerruntime.NewHealth("export-worker", db, active.Load)
	healthServer := &http.Server{
		Addr: healthAddr, Handler: health.Handler(),
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 64 << 10,
	}
	healthErrors := make(chan error, 1)
	go func() {
		logger.Info("export worker health server starting", "address", healthAddr)
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			healthErrors <- err
		}
	}()
	health.SetReady(true)

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	expiry := time.NewTicker(expireEvery)
	defer expiry.Stop()
	logger.Info("export worker started", "workerId", workerID)
	for {
		select {
		case <-ctx.Done():
			health.SetReady(false)
			shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			_ = healthServer.Shutdown(shutdownCtx)
			cancel()
			logger.Info("export worker stopped")
			return
		case err := <-healthErrors:
			logger.Error("export worker health server failed", "error", err)
			stop()
		case <-ticker.C:
			for i := 0; i < 10; i++ {
				active.Add(1)
				processed, processErr := worker.ProcessOne(ctx)
				active.Add(-1)
				if processErr != nil {
					logger.Error("process export", "error", processErr)
				}
				if !processed {
					break
				}
			}
		case <-expiry.C:
			active.Add(1)
			n, expireErr := worker.Expire(ctx, 100)
			active.Add(-1)
			if expireErr != nil {
				logger.Error("expire exports", "error", expireErr)
			} else if n > 0 {
				logger.Info("expired exports", "count", n)
			}
		}
	}
}

func privacyEvidenceKeyringFromEnvironment() (*sharedcrypto.SecretKeyring, error) {
	if value := strings.TrimSpace(os.Getenv("PRIVACY_EVIDENCE_KEYS_JSON")); value != "" {
		active := strings.TrimSpace(os.Getenv("PRIVACY_EVIDENCE_ACTIVE_KEY_VERSION"))
		if active == "" {
			active = "v1"
		}
		return sharedcrypto.NewSecretKeyringFromJSON(active, value)
	}
	if value := strings.TrimSpace(os.Getenv("PRIVACY_EVIDENCE_KEY_BASE64")); value != "" {
		return sharedcrypto.NewSecretKeyringFromJSON("v1", fmt.Sprintf(`{"v1":%q}`, value))
	}
	return nil, errors.New("PRIVACY_EVIDENCE_KEY_BASE64 or PRIVACY_EVIDENCE_KEYS_JSON is required")
}

func strictEnvDuration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	if duration, err := time.ParseDuration(raw); err == nil && duration > 0 {
		return duration, nil
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second, nil
	}
	return 0, fmt.Errorf("%s must be a positive duration or number of seconds", name)
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
