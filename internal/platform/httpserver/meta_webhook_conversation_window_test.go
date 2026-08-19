package httpserver

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/consent"
	"campaign-platform/internal/delivery"
	"campaign-platform/internal/metacloud"
	_ "campaign-platform/internal/persistence/database"
)

type recordingMetaConversationWindows struct {
	observations []metacloud.ConversationWindowObservation
	windows      []time.Duration
	err          error
}

func (s *recordingMetaConversationWindows) ObserveInbound(_ context.Context, observation metacloud.ConversationWindowObservation, window time.Duration) (metacloud.ConversationWindowEvidence, bool, error) {
	if s.err != nil {
		return metacloud.ConversationWindowEvidence{}, false, s.err
	}
	s.observations = append(s.observations, observation)
	s.windows = append(s.windows, window)
	return metacloud.ConversationWindowEvidence{MetaSenderID: observation.MetaSenderID, ContactID: observation.ContactID, ProviderMessageID: observation.ProviderMessageID, OccurredAt: observation.OccurredAt, EligibleUntil: observation.OccurredAt.Add(window)}, true, nil
}

type conversationWindowDeliveryResolver struct {
	expectedSenderID string
	recipient        delivery.Recipient
}

func (r conversationWindowDeliveryResolver) ResolveMetaWebhookRecipient(_ context.Context, sender metacloud.Sender, _ string) (delivery.Recipient, error) {
	if sender.ID != r.expectedSenderID {
		return delivery.Recipient{}, delivery.ErrRecipientNotFound
	}
	return r.recipient, nil
}
func (r conversationWindowDeliveryResolver) ResolveMetaWebhookInboundRecipient(_ context.Context, sender metacloud.Sender, _ string) (delivery.Recipient, error) {
	if sender.ID != r.expectedSenderID {
		return delivery.Recipient{}, delivery.ErrRecipientNotFound
	}
	return r.recipient, nil
}

type missingMetaInboundResolver struct{}

func (missingMetaInboundResolver) ResolveMetaWebhookRecipient(context.Context, metacloud.Sender, string) (delivery.Recipient, error) {
	return delivery.Recipient{}, delivery.ErrRecipientNotFound
}
func (missingMetaInboundResolver) ResolveMetaWebhookInboundRecipient(context.Context, metacloud.Sender, string) (delivery.Recipient, error) {
	return delivery.Recipient{}, delivery.ErrRecipientNotFound
}
func newConversationWindowWebhookHarness(t *testing.T, resolver MetaWebhookDeliveryResolver) (http.Handler, *recordingMetaConversationWindows, string, metacloud.Sender) {
	t.Helper()
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 13, 0, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "recipient-window", ContactID: "contact-window", CampaignID: "campaign-window", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.out.window", UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	if resolver == nil {
		resolver = conversationWindowDeliveryResolver{expectedSenderID: "meta-sender-window", recipient: recipient}
	}
	ledger := consent.NewLedgerService(consent.NewMemoryLedgerRepository())
	processor := &consent.OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Clock: func() time.Time { return now }}
	sender := metacloud.Sender{ID: "meta-sender-window", WABAID: "waba-window", PhoneNumberID: "phone-window", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	windows := &recordingMetaConversationWindows{}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: sender}, MetaWebhookMaxBody: 1 << 20,
		MetaWebhookDeliveries: resolver, DeliveryEvents: deliveries, OptOutProcessor: processor,
		MetaConversationWindows: windows, MetaConversationWindow: 24 * time.Hour,
	}).Handler()
	return handler, windows, "meta-app-secret-0123456789", sender
}

func conversationWindowInboundBody(phoneID string) []byte {
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-window","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"` + phoneID + `"},"messages":[{"from":"2348012345678","id":"wamid.in.window","timestamp":"1786798800","type":"text","text":{"body":"hello"}}]}}]}]}`)
}
func postSignedMetaWebhook(handler http.Handler, secret string, body []byte, signature string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/meta/meta-ng", bytes.NewReader(body))
	if signature == "" {
		signature = signMetaWebhook(secret, body)
	}
	req.Header.Set(metacloud.WebhookSignatureHeader, signature)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func TestMetaWebhookConversationWindowWritesOnlyAfterAuthenticatedSenderBoundContactResolution(t *testing.T) {
	handler, windows, secret, sender := newConversationWindowWebhookHarness(t, nil)
	body := conversationWindowInboundBody(sender.PhoneNumberID)
	res := postSignedMetaWebhook(handler, secret, body, "")
	if res.Code != http.StatusOK {
		t.Fatalf("valid inbound response=%d body=%s", res.Code, res.Body.String())
	}
	if len(windows.observations) != 1 || len(windows.windows) != 1 {
		t.Fatalf("conversation window observations=%#v windows=%#v", windows.observations, windows.windows)
	}
	observation := windows.observations[0]
	if observation.MetaSenderID != sender.ID || observation.ContactID != "contact-window" || observation.ProviderMessageID != "wamid.in.window" || windows.windows[0] != 24*time.Hour {
		t.Fatalf("unexpected conversation-window observation=%#v window=%s", observation, windows.windows[0])
	}
}
func TestMetaWebhookConversationWindowRejectsUnauthenticatedOrUnresolvedEvidence(t *testing.T) {
	t.Run("bad signature", func(t *testing.T) {
		handler, windows, secret, sender := newConversationWindowWebhookHarness(t, nil)
		body := conversationWindowInboundBody(sender.PhoneNumberID)
		res := postSignedMetaWebhook(handler, secret, body, signMetaWebhook(secret, append(body, ' ')))
		if res.Code != http.StatusUnauthorized || len(windows.observations) != 0 {
			t.Fatalf("bad signature response=%d observations=%d", res.Code, len(windows.observations))
		}
	})
	t.Run("unbound sender", func(t *testing.T) {
		handler, windows, secret, _ := newConversationWindowWebhookHarness(t, nil)
		body := conversationWindowInboundBody("phone-unbound")
		res := postSignedMetaWebhook(handler, secret, body, "")
		if res.Code != http.StatusForbidden || len(windows.observations) != 0 {
			t.Fatalf("unbound sender response=%d observations=%d", res.Code, len(windows.observations))
		}
	})
	t.Run("unresolved contact", func(t *testing.T) {
		handler, windows, secret, sender := newConversationWindowWebhookHarness(t, missingMetaInboundResolver{})
		body := conversationWindowInboundBody(sender.PhoneNumberID)
		res := postSignedMetaWebhook(handler, secret, body, "")
		if res.Code != http.StatusOK || len(windows.observations) != 0 {
			t.Fatalf("unresolved contact response=%d observations=%d body=%s", res.Code, len(windows.observations), res.Body.String())
		}
	})
}

func TestPostgreSQLMetaWebhookPersistsAuthenticatedConversationWindowEvidence(t *testing.T) {
	dsn := os.Getenv("POSTGRES_META_CLOUD_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_META_CLOUD_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var actorID, orgID, poolID, senderID, contactID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&actorID, &orgID, &poolID, &senderID, &contactID); err != nil {
		t.Fatal(err)
	}
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Webhook window actor','ACTIVE',false)`, actorID, "webhook-window-"+actorID[:8]+"@example.test")
	mustExec(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Webhook Window "+orgID[:8])
	mustExec(`INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',10,100)`, poolID, "webhook-window-"+poolID[:8], orgID)
	wabaID := "waba-" + senderID[:8]
	phoneID := "phone-" + senderID[:8]
	mustExec(`INSERT INTO meta_cloud_senders(id,organisation_id,sender_pool_id,waba_id,phone_number_id,display_name,business_phone_display,credential_key,graph_api_version,status,health_status,health_observed_at,effective_from,version,created_by,approved_by,reason) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,'Webhook Window','+234 ***','meta-ng','v23.0','ACTIVE','HEALTHY',now(),now()-interval '1 hour',1,$6::uuid,$6::uuid,'approved sender')`, senderID, orgID, poolID, wabaID, phoneID, actorID)
	mustExec(`INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,'x'::bytea,$2::bytea,'+234******5678','ACTIVE',now())`, contactID, []byte("webhook-window-lookup-"+contactID))
	defer func() {
		cleanup := context.Background()
		_, _ = db.ExecContext(cleanup, `DELETE FROM meta_cloud_conversation_windows WHERE meta_sender_id=$1::uuid`, senderID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM contacts WHERE id=$1::uuid`, contactID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM meta_cloud_senders WHERE id=$1::uuid`, senderID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_pools WHERE id=$1::uuid`, poolID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 13, 0, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "recipient-webhook-window", ContactID: contactID, CampaignID: "campaign-webhook-window", Status: delivery.StatusGatewayAccepted, UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	processor := &consent.OptOutProcessor{Deliveries: deliveries, Ledger: consent.NewLedgerService(consent.NewMemoryLedgerRepository()), Clock: func() time.Time { return now }}
	sender := metacloud.Sender{ID: senderID, WABAID: wabaID, PhoneNumberID: phoneID, CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	store := &metacloud.PostgreSQLConversationWindowStore{DB: db}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: sender}, MetaWebhookMaxBody: 1 << 20,
		MetaWebhookDeliveries: conversationWindowDeliveryResolver{expectedSenderID: senderID, recipient: recipient},
		DeliveryEvents:        deliveries, OptOutProcessor: processor,
		MetaConversationWindows: store, MetaConversationWindow: 24 * time.Hour,
	}).Handler()
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"` + wabaID + `","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"` + phoneID + `"},"messages":[{"from":"2348012345678","id":"wamid.pg.window","timestamp":"1786798800","type":"text","text":{"body":"hello"}}]}}]}]}`)
	res := postSignedMetaWebhook(handler, "meta-app-secret-0123456789", body, "")
	if res.Code != http.StatusOK {
		t.Fatalf("signed PostgreSQL inbound response=%d body=%s", res.Code, res.Body.String())
	}
	evidence, ok, err := store.Current(ctx, senderID, contactID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || evidence.ProviderMessageID != "wamid.pg.window" || evidence.MetaSenderID != senderID || evidence.ContactID != contactID {
		t.Fatalf("signed inbound did not persist exact sender/contact evidence: %#v ok=%v", evidence, ok)
	}
	if !evidence.EligibleUntil.Equal(evidence.OccurredAt.Add(24 * time.Hour)) {
		t.Fatalf("persisted window=%s occurred=%s", evidence.EligibleUntil, evidence.OccurredAt)
	}
}

type quotedMissMetaInboundResolver struct {
	expectedSenderID string
	recipient        delivery.Recipient
}

func (r quotedMissMetaInboundResolver) ResolveMetaWebhookRecipient(_ context.Context, sender metacloud.Sender, _ string) (delivery.Recipient, error) {
	if sender.ID != r.expectedSenderID {
		return delivery.Recipient{}, delivery.ErrRecipientNotFound
	}
	return delivery.Recipient{}, delivery.ErrRecipientNotFound
}

func (r quotedMissMetaInboundResolver) ResolveMetaWebhookInboundRecipient(_ context.Context, sender metacloud.Sender, _ string) (delivery.Recipient, error) {
	if sender.ID != r.expectedSenderID {
		return delivery.Recipient{}, delivery.ErrRecipientNotFound
	}
	return r.recipient, nil
}

func TestMetaWebhookUnknownQuotedMessageFallsBackToAuthenticatedSenderAndFrom(t *testing.T) {
	recipient := delivery.Recipient{ID: "recipient-window", ContactID: "contact-window", CampaignID: "campaign-window", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.out.window"}
	resolver := quotedMissMetaInboundResolver{expectedSenderID: "meta-sender-window", recipient: recipient}
	handler, windows, secret, sender := newConversationWindowWebhookHarness(t, resolver)
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-window","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"` + sender.PhoneNumberID + `"},"messages":[{"from":"2348012345678","id":"wamid.in.window.unknown-quote","timestamp":"1786798800","type":"text","context":{"id":"wamid.unknown"},"text":{"body":"hello"}}]}}]}]}`)
	res := postSignedMetaWebhook(handler, secret, body, "")
	if res.Code != http.StatusOK || len(windows.observations) != 1 {
		t.Fatalf("unknown quoted inbound response=%d observations=%d body=%s", res.Code, len(windows.observations), res.Body.String())
	}
}

func TestMetaWebhookInboundFailsClosedWhenConversationWindowStoreMissing(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 13, 0, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "recipient-window", ContactID: "contact-window", CampaignID: "campaign-window", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.out.window", UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	processor := &consent.OptOutProcessor{
		Deliveries: deliveries,
		Ledger:     consent.NewLedgerService(consent.NewMemoryLedgerRepository()),
		Clock:      func() time.Time { return now },
	}
	sender := metacloud.Sender{ID: "meta-sender-window", WABAID: "waba-window", PhoneNumberID: "phone-window", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: sender}, MetaWebhookMaxBody: 1 << 20,
		MetaWebhookDeliveries: conversationWindowDeliveryResolver{expectedSenderID: sender.ID, recipient: recipient},
		DeliveryEvents:        deliveries, OptOutProcessor: processor,
		MetaConversationWindow: 24 * time.Hour,
	}).Handler()
	res := postSignedMetaWebhook(handler, "meta-app-secret-0123456789", conversationWindowInboundBody(sender.PhoneNumberID), "")
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("missing conversation-window store response=%d body=%s", res.Code, res.Body.String())
	}
}

type failingMetaOptOutPolicyProvider struct{}

func (failingMetaOptOutPolicyProvider) Active(context.Context) (consent.OptOutPolicy, error) {
	return consent.OptOutPolicy{}, errors.New("opt-out policy store unavailable")
}

func TestMetaWebhookAuthenticatedInboundOpensConversationWindowBeforeOptOutProcessing(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 13, 0, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "recipient-window-stop", ContactID: "contact-window-stop", CampaignID: "campaign-window-stop", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "wamid.out.window.stop", UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	processor := &consent.OptOutProcessor{
		Deliveries: deliveries,
		Ledger:     consent.NewLedgerService(consent.NewMemoryLedgerRepository()),
		Policies:   failingMetaOptOutPolicyProvider{},
		Clock:      func() time.Time { return now },
	}
	sender := metacloud.Sender{ID: "meta-sender-window-stop", WABAID: "waba-window-stop", PhoneNumberID: "phone-window-stop", CredentialKey: "meta-ng", Status: metacloud.StatusActive}
	windows := &recordingMetaConversationWindows{}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		MetaCredentials: credentials, MetaWebhookSenders: metaWebhookSenderStub{sender: sender}, MetaWebhookMaxBody: 1 << 20,
		MetaWebhookDeliveries: conversationWindowDeliveryResolver{expectedSenderID: sender.ID, recipient: recipient},
		DeliveryEvents:        deliveries, OptOutProcessor: processor,
		MetaConversationWindows: windows, MetaConversationWindow: 24 * time.Hour,
	}).Handler()
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-window-stop","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"phone-window-stop"},"messages":[{"from":"2348012345678","id":"wamid.in.window.stop","timestamp":"1786798800","type":"text","text":{"body":"STOP"}}]}}]}]}`)
	res := postSignedMetaWebhook(handler, "meta-app-secret-0123456789", body, "")
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("failed opt-out processing response=%d body=%s", res.Code, res.Body.String())
	}
	if len(windows.observations) != 1 {
		t.Fatalf("authenticated inbound did not create conversation-window evidence before downstream opt-out failure: %#v", windows.observations)
	}
}
