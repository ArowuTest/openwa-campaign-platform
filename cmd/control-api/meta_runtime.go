package main

import (
	"database/sql"
	"strings"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/execution"

	"campaign-platform/internal/message"
	"campaign-platform/internal/metacloud"
	"campaign-platform/internal/platform/httpserver"
	"campaign-platform/internal/shared/config"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

type metaControlRuntime struct {
	Credentials         metacloud.CredentialResolver
	Senders             *metacloud.Service
	Templates           *metacloud.TemplateService
	Verifier            httpserver.MetaSenderVerifier
	WebhookSenders      httpserver.MetaWebhookSenderResolver
	WebhookDeliveries   httpserver.MetaWebhookDeliveryResolver
	ConversationWindows httpserver.MetaConversationWindowStore
	ConversationWindow  time.Duration
}

func applyMetaRoutingRuntime(routing *execution.RoutingAdministration, runtime metaControlRuntime, healthStaleAfter time.Duration) {
	if routing == nil {
		return
	}
	if runtime.Senders != nil {
		routing.MetaSenders = runtime.Senders.Store
	}
	if runtime.Templates != nil {
		routing.MetaTemplates = runtime.Templates.Store
	}
	if healthStaleAfter > 0 {
		routing.MetaHealthStaleAfter = healthStaleAfter
	}
}
func applyMetaControlRuntime(deps *httpserver.Dependencies, runtime metaControlRuntime) {
	if deps == nil {
		return
	}
	deps.MetaCredentials = runtime.Credentials
	deps.MetaSenders = runtime.Senders
	deps.MetaTemplates = runtime.Templates
	deps.MetaVerifier = runtime.Verifier
	deps.MetaWebhookSenders = runtime.WebhookSenders
	deps.MetaWebhookDeliveries = runtime.WebhookDeliveries
	deps.MetaConversationWindows = runtime.ConversationWindows
	deps.MetaConversationWindow = runtime.ConversationWindow
}

func buildMemoryMetaControlRuntime(cfg config.Config, messages *message.Service) (metaControlRuntime, error) {
	senderStore := metacloud.NewMemoryStore()
	templateStore := metacloud.NewMemoryTemplateStore()
	out := metaControlRuntime{
		Senders:            &metacloud.Service{Store: senderStore},
		Templates:          &metacloud.TemplateService{Store: templateStore, Messages: messages},
		WebhookSenders:     senderStore,
		ConversationWindow: cfg.MetaConversationWindow,
	}
	return attachMetaClient(cfg.MetaCloudCredentialsJSON, out)
}

func buildPostgreSQLMetaControlRuntime(cfg config.Config, db *sql.DB, messages *message.Service, deliveries *delivery.Service, protector *sharedcrypto.MSISDNProtector) (metaControlRuntime, error) {
	senderStore := &metacloud.PostgreSQLStore{DB: db}
	templateStore := &metacloud.PostgreSQLTemplateStore{DB: db}
	out := metaControlRuntime{
		Senders:             &metacloud.Service{Store: senderStore},
		Templates:           &metacloud.TemplateService{Store: templateStore, Messages: messages},
		WebhookSenders:      senderStore,
		WebhookDeliveries:   &metacloud.PostgreSQLWebhookDeliveryResolver{DB: db, Deliveries: deliveries, Protector: protector},
		ConversationWindows: &metacloud.PostgreSQLConversationWindowStore{DB: db},
		ConversationWindow:  cfg.MetaConversationWindow,
	}
	return attachMetaClient(cfg.MetaCloudCredentialsJSON, out)
}

func attachMetaClient(raw string, out metaControlRuntime) (metaControlRuntime, error) {
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}
	credentials, err := metacloud.ParseCredentialSet(raw)
	if err != nil {
		return metaControlRuntime{}, err
	}
	client := &metacloud.Client{Credentials: credentials}
	out.Credentials = credentials
	out.Verifier = client
	out.Templates.Client = client
	return out, nil
}
