package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/delivery"
	"campaign-platform/internal/dispatch"
	"campaign-platform/internal/execution"
	"campaign-platform/internal/jobs"
	"campaign-platform/internal/message"
	"campaign-platform/internal/outbox"
	"campaign-platform/internal/persistence/database"
	postgresrepo "campaign-platform/internal/persistence/postgres"
	"campaign-platform/internal/sender"
	sharedcrypto "campaign-platform/internal/shared/crypto"
	"campaign-platform/internal/storage"
	"campaign-platform/internal/testmessage"
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
	cfg, err := workerconfig.LoadCampaign()
	if err != nil {
		logger.Error("campaign worker configuration is invalid", "error", err)
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
		logger.Error("campaign worker database startup failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	protector, err := sharedcrypto.NewMSISDNProtectorFromBase64(cfg.MSISDNEncryptionKey, cfg.MSISDNLookupKey)
	if err != nil {
		logger.Error("campaign worker MSISDN protection startup failed", "error", err)
		os.Exit(1)
	}

	jobRepository := &jobs.PostgreSQLRepository{DB: db}
	jobService := jobs.NewService(jobRepository)
	outboxRepository := &outbox.PostgreSQLRepository{DB: db}
	outboxRunner := &outbox.Runner{
		Repository: outboxRepository,
		Publisher:  &outbox.Publisher{Outbox: outboxRepository, Jobs: jobService, QueueBackpressureLimit: cfg.DispatchQueueBackpressureLimit, BackpressureRetryAfter: cfg.DispatchQueueBackpressureRetry},
		Owner:      cfg.WorkerID + ":outbox", Lease: cfg.OutboxLease,
		Batch: cfg.OutboxClaimBatch, Concurrency: cfg.OutboxConcurrency,
		PollInterval: cfg.OutboxPollInterval, OperationTimeout: cfg.OperationTimeout,
		ShutdownGrace: cfg.ShutdownTimeout,
	}

	ledger := delivery.NewService(&delivery.PostgreSQLRepository{DB: db})
	pacingPolicies := &sender.PacingAdministration{Store: &postgresrepo.PacingPolicyRepository{DB: db}}
	allocator := &sender.PostgreSQLAllocator{DB: db, HeartbeatTTL: cfg.SenderHeartbeatTTL}
	materials := &dispatch.PostgreSQLMaterialLoader{
		DB: db, Protector: protector, Allocator: allocator,
		Objects: dispatch.SignedObjectResolver{Signer: storage.URLSigner{
			BaseURL: cfg.MediaDownloadBaseURL, Secret: []byte(cfg.MediaDownloadSecret),
		}},
	}
	gatewayClient := &http.Client{
		Timeout: cfg.GatewayHTTPTimeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          cfg.JobConcurrency * 2,
			MaxIdleConnsPerHost:   cfg.JobConcurrency,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: cfg.GatewayHTTPTimeout,
			ExpectContinueTimeout: time.Second,
		},
	}
	testMessageRepository := &testmessage.PostgreSQLRepository{DB: db}
	testMessageService := message.NewService(&message.PostgreSQLRepository{DB: db})
	dispatchHandler := &dispatch.Handler{
		Ledger:      ledger,
		Materials:   materials,
		Eligibility: &dispatch.PostgreSQLFinalEligibility{DB: db},
		Pacing:      &dispatch.PostgreSQLPacingController{DB: db, Policies: pacingPolicies},
		Gateway: &dispatch.HTTPGateway{
			BaseURL: cfg.GatewayURL, CommandSecret: cfg.GatewayCommandSecret,
			Client: gatewayClient, MaximumResponseBytes: cfg.GatewayMaxResponse,
		},
	}
	testMessageRunner := &testmessage.Runner{
		Repository: testMessageRepository,
		Processor: &testmessage.Processor{
			Repository: testMessageRepository,
			Protector:  protector,
			Messages:   testMessageService,
			Routes:     testMessageRepository,
			Pacing:     &dispatch.PostgreSQLPacingController{DB: db, Policies: pacingPolicies},
			Gateway:    &dispatch.HTTPGateway{BaseURL: cfg.GatewayURL, CommandSecret: cfg.GatewayCommandSecret, Client: gatewayClient, MaximumResponseBytes: cfg.GatewayMaxResponse},
			Media:      dispatch.SignedObjectResolver{Signer: storage.URLSigner{BaseURL: cfg.MediaDownloadBaseURL, Secret: []byte(cfg.MediaDownloadSecret)}},
		},
		Owner: cfg.WorkerID + ":test-message", Lease: cfg.JobLease, PollInterval: cfg.JobPollInterval, Batch: 20,
	}

	campaignService := campaign.NewService(&postgresrepo.CampaignRepository{DB: db})
	executionStore := &execution.PostgreSQLStore{DB: db}
	routingPlans := &execution.RoutingAdministration{Store: &execution.PostgreSQLRoutingPlanStore{DB: db}, Campaigns: campaignService}
	executionCoordinator := &execution.Coordinator{Campaigns: campaignService, Store: executionStore, SafetyMarginPercent: 15, RoutingPlans: routingPlans}
	executionRunner := &execution.Runner{Repository: executionStore, Coordinator: executionCoordinator, Owner: cfg.WorkerID + ":execution", Lease: cfg.JobLease, PollInterval: cfg.JobPollInterval, Batch: 20}
	shardRunner := &execution.ShardRunner{Repository: &execution.PostgreSQLShardRepository{DB: db}, Owner: cfg.WorkerID + ":shards", TargetSize: cfg.DispatchShardTargetSize, DiscoveryBatch: 10, ClaimBatch: cfg.DispatchShardClaimBatch, Lease: cfg.JobLease, PollInterval: cfg.JobPollInterval}

	queueRepairRunner := &dispatch.QueueRepairRunner{Repository: &dispatch.PostgreSQLQueueRepairRepository{DB: db}, Batch: cfg.DispatchQueueRepairBatch, PollInterval: cfg.DispatchQueueRepairInterval}

	jobRunner := &jobs.Runner{
		Repository: jobRepository, Owner: cfg.WorkerID + ":dispatch",
		Types:       []string{dispatch.JobType},
		Concurrency: cfg.JobConcurrency, ClaimBatch: cfg.JobClaimBatch,
		Lease: cfg.JobLease, PollInterval: cfg.JobPollInterval,
		OperationTimeout: cfg.OperationTimeout, ShutdownGrace: cfg.ShutdownTimeout,
		Handler: dispatchHandler.Handle,
	}

	health := workerruntime.NewHealth("campaign-worker", db, func() int64 {
		return jobRunner.Active() + outboxRunner.Active() + executionRunner.Active() + shardRunner.Active() + queueRepairRunner.Active() + testMessageRunner.Active()
	})
	healthServer := &http.Server{
		Addr: cfg.HealthAddr, Handler: health.Handler(),
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 64 << 10,
	}
	healthErrors := make(chan error, 1)
	go func() {
		logger.Info("campaign worker health server starting", "address", cfg.HealthAddr)
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			healthErrors <- err
		}
	}()

	runnerErrors := make(chan error, 6)
	go func() { runnerErrors <- outboxRunner.Run(rootCtx) }()
	go func() { runnerErrors <- jobRunner.Run(rootCtx) }()
	go func() { runnerErrors <- executionRunner.Run(rootCtx) }()
	go func() { runnerErrors <- shardRunner.Run(rootCtx) }()
	go func() { runnerErrors <- queueRepairRunner.Run(rootCtx) }()
	go func() { runnerErrors <- testMessageRunner.Run(rootCtx) }()
	health.SetReady(true)
	logger.Info("campaign worker started",
		"workerId", cfg.WorkerID,
		"dispatchConcurrency", cfg.JobConcurrency,
		"outboxConcurrency", cfg.OutboxConcurrency,
		"executionScheduler", true,
		"dispatchSharding", true,
		"queueRepair", true,
		"controlledTestMessages", true,
		"mediaDelivery", "signed-short-lived-url")

	var runErr error
	runnersStopped := 0
	select {
	case <-rootCtx.Done():
	case runErr = <-runnerErrors:
		runnersStopped = 1
		stop()
	case runErr = <-healthErrors:
		stop()
	}
	health.SetReady(false)
	if transport, ok := gatewayClient.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("campaign worker health shutdown failed", "error", err)
	}
	for runnersStopped < 6 {
		select {
		case err := <-runnerErrors:
			runnersStopped++
			if runErr == nil {
				runErr = err
			}
		case <-shutdownCtx.Done():
			logger.Error("campaign worker shutdown timed out", "activeDispatch", jobRunner.Active(), "activeOutbox", outboxRunner.Active())
			os.Exit(1)
		}
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		logger.Error("campaign worker stopped with error", "error", runErr)
		os.Exit(1)
	}
	logger.Info("campaign worker stopped", "activeDispatch", jobRunner.Active(), "activeOutbox", outboxRunner.Active())
}
