package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"campaign-platform/internal/audit"
	"campaign-platform/internal/operations"
	"campaign-platform/internal/persistence/database"
	"campaign-platform/internal/storage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(2)
	}
	root := os.Getenv("OBJECT_STORE_ROOT")
	if root == "" {
		root = "./var/objects"
	}
	workerID := os.Getenv("EXPORT_WORKER_ID")
	if workerID == "" {
		workerID = "export-worker-1"
	}
	db, err := database.Open(ctx, database.PoolConfig{Driver: "postgres", DSN: dsn, MaxOpen: 4, MaxIdle: 2, PingTimeout: 10 * time.Second})
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	objects, err := storage.NewFileSystemStore(root)
	if err != nil {
		logger.Error("open object store", "error", err)
		os.Exit(1)
	}
	repo := &operations.PostgreSQLRepository{DB: db}
	worker := &operations.ExportWorker{Repository: repo, AuditRepository: &audit.PostgreSQLRepository{DB: db}, Objects: objects, WorkerID: workerID, LeaseDuration: 2 * time.Minute, ReadyTTL: envDuration("EXPORT_READY_TTL", 24*time.Hour)}
	poll := envDuration("EXPORT_POLL_INTERVAL", 2*time.Second)
	expireEvery := envDuration("EXPORT_EXPIRY_INTERVAL", time.Minute)
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	expiry := time.NewTicker(expireEvery)
	defer expiry.Stop()
	logger.Info("export worker started", "workerId", workerID)
	for {
		select {
		case <-ctx.Done():
			logger.Info("export worker stopped")
			return
		case <-ticker.C:
			for i := 0; i < 10; i++ {
				processed, e := worker.ProcessOne(ctx)
				if e != nil {
					logger.Error("process export", "error", e)
				}
				if !processed {
					break
				}
			}
		case <-expiry.C:
			n, e := worker.Expire(ctx, 100)
			if e != nil {
				logger.Error("expire exports", "error", e)
			} else if n > 0 {
				logger.Info("expired exports", "count", n)
			}
		}
	}
}
func envDuration(name string, fallback time.Duration) time.Duration {
	if raw := os.Getenv(name); raw != "" {
		if d, e := time.ParseDuration(raw); e == nil {
			return d
		}
		if n, e := strconv.Atoi(raw); e == nil {
			return time.Duration(n) * time.Second
		}
	}
	return fallback
}
