package main

import (
	"context"
	"crypto/rand"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"campaign-platform/internal/observability"
	"campaign-platform/internal/platform/httpserver"
	"campaign-platform/internal/shared/config"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "healthcheck" {
		response, err := http.Get(os.Args[2])
		if err != nil || response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		_ = response.Body.Close()
		return
	}
	cfg, err := config.Load()
	if err != nil {
		log.New(os.Stderr, "configuration: ", 0).Println(err)
		os.Exit(1)
	}
	logger := observability.NewLogger(os.Stdout, "control-api", os.Getenv("APP_ENV"))
	startupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	runtime, err := buildControlRuntime(startupCtx, cfg, logger)
	cancel()
	if err != nil {
		logger.Error("control API runtime initialisation failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := runtime.Close(); err != nil {
			logger.Error("control API resource close failed", "error", err)
		}
	}()
	registry := observability.NewRegistry("control-api")
	application := observability.HTTPMiddleware(registry, logger, httpserver.New(logger, runtime.Dependencies).Handler())
	router := http.NewServeMux()
	router.Handle("GET /metrics", registry.Handler(runtime.DB))
	observability.RegisterProfiling(router, cfg.ProfilingToken)
	router.Handle("/", application)
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
	errorsCh := make(chan error, 1)
	go func() {
		logger.Info("control API starting", "address", cfg.HTTPAddr, "environment", cfg.Environment)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errorsCh <- err
		}
		close(errorsCh)
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-signals:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-errorsCh:
		if err != nil {
			logger.Error("control API failed", "error", err)
		}
	}
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}

func buildMSISDNProtector(cfg config.Config, logger *slog.Logger) (*sharedcrypto.MSISDNProtector, error) {
	if cfg.MSISDNEncryptionKeyBase64 != "" || cfg.MSISDNLookupKeyBase64 != "" {
		return sharedcrypto.NewMSISDNProtectorFromBase64(cfg.MSISDNEncryptionKeyBase64, cfg.MSISDNLookupKeyBase64)
	}
	encryptionKey := make([]byte, 32)
	lookupKey := make([]byte, 32)
	if _, err := rand.Read(encryptionKey); err != nil {
		return nil, err
	}
	if _, err := rand.Read(lookupKey); err != nil {
		return nil, err
	}
	logger.Warn("using ephemeral development MSISDN keys; configure stable keys before persistence")
	return sharedcrypto.NewMSISDNProtector(encryptionKey, lookupKey)
}
