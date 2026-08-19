package main

import (
	"errors"
	"net/http"
	"strings"

	"campaign-platform/internal/dispatch"
	"campaign-platform/internal/metacloud"
	workerconfig "campaign-platform/internal/worker/config"
)

func buildCampaignTransportRouter(cfg workerconfig.CampaignConfig, client *http.Client) (*dispatch.TransportRouter, error) {
	if client == nil {
		return nil, errors.New("campaign transport HTTP client is required")
	}
	router := &dispatch.TransportRouter{}
	gatewayURL := strings.TrimSpace(cfg.GatewayURL)
	gatewaySecret := strings.TrimSpace(cfg.GatewayCommandSecret)
	if gatewayURL != "" || gatewaySecret != "" {
		if gatewaySecret == "" {
			return nil, errors.New("OpenWA transport signing secret is required")
		}
		router.OpenWA = &dispatch.HTTPGateway{
			BaseURL: gatewayURL, CommandSecret: gatewaySecret, RequireNodeURL: true,
			Client: client, MaximumResponseBytes: cfg.GatewayMaxResponse,
		}
	}
	raw := strings.TrimSpace(cfg.MetaCloudCredentialsJSON)
	if raw == "" {
		if router.OpenWA == nil {
			return nil, errors.New("campaign worker requires at least one messaging transport")
		}
		return router, nil
	}
	credentials, err := metacloud.ParseCredentialSet(raw)
	if err != nil {
		return nil, err
	}
	metaClient := &metacloud.Client{
		Credentials: credentials, HTTPClient: client,
		MaximumResponseBytes: cfg.GatewayMaxResponse,
	}
	router.Meta = &dispatch.MetaGateway{Client: metaClient}
	return router, nil
}
