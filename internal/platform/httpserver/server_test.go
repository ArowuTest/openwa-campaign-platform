package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/audience/importer"
	"campaign-platform/internal/campaign"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/delivery"
	"campaign-platform/internal/gateway"
	"campaign-platform/internal/geography"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/inbound"
	"campaign-platform/internal/message"
	"campaign-platform/internal/organisation"
	"campaign-platform/internal/security/malware"
	"campaign-platform/internal/segment"
	sharedcrypto "campaign-platform/internal/shared/crypto"
	"campaign-platform/internal/storage"
)

type httpReviewEvidenceResolver struct{}

func (httpReviewEvidenceResolver) ResolveConsentEvidence(_ context.Context, id string) (consent.EvidenceAsset, error) {
	return consent.EvidenceAsset{ID: id, ObjectKey: "evidence/" + id}, nil
}

func testServer(t *testing.T) (http.Handler, string) {
	t.Helper()
	registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := identity.HashPassword("a-very-long-development-password")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{ID: "test-user", Email: "operator@example.test", DisplayName: "Test Operator",
		Status: identity.StatusActive, PasswordHash: hash, MFARequired: false,
		Permissions: map[string]struct{}{"*": {}}}
	identityService := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := identityService.Login(context.Background(), user.Email, "a-very-long-development-password")
	if err != nil {
		t.Fatal(err)
	}
	protector, err := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	filterStore, err := audiencefilter.NewMemoryAdministrationStore(audiencefilter.DefaultDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	nowPolicy := time.Now().UTC()
	optOutPolicies := &consent.OptOutPolicyAdministration{Store: consent.NewMemoryOptOutPolicyStore(consent.GovernedOptOutPolicy{ID: "seed-policy", Keywords: []string{"STOP"}, Status: consent.OptOutPolicyActive, EffectiveFrom: nowPolicy.Add(-time.Hour), Version: 1, CreatedBy: "system", Reason: "initial policy", CreatedAt: nowPolicy.Add(-time.Hour), UpdatedAt: nowPolicy.Add(-time.Hour)})}
	retentionPolicies := &inbound.RetentionPolicyAdministration{Store: inbound.NewMemoryRetentionPolicyStore(inbound.RetentionPolicy{ID: "seed-retention", RetentionDays: 90, Status: inbound.RetentionPolicyActive, EffectiveFrom: nowPolicy.Add(-time.Hour), Version: 1, CreatedBy: "system", ApprovedBy: "system", Reason: "initial retention", CreatedAt: nowPolicy.Add(-time.Hour), UpdatedAt: nowPolicy.Add(-time.Hour)})}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Registry:                 registry,
		FilterDefinitions:        audiencefilter.NewAdministrationService(filterStore, registry),
		Compiler:                 cohort.NewCompiler(registry),
		Organisations:            organisation.NewService(organisation.NewMemoryRepository()),
		ConsentReviews:           consent.NewService(consent.NewMemoryRepository()),
		ConsentLedger:            consent.NewLedgerService(consent.NewMemoryLedgerRepository()),
		OptOutPolicies:           optOutPolicies,
		InboundRetentionPolicies: retentionPolicies,
		InboundReplies:           &inbound.Service{Repository: inbound.NewMemoryRepository(), Audit: &testInboundAudit{}},
		Campaigns:                campaign.NewService(campaign.NewMemoryRepository()),
		Geography:                geography.DefaultCatalogue(),
		MaxImportPreviewRows:     1000,
		Identity:                 identityService,
		MSISDNProtector:          protector,
		Messages:                 message.NewService(message.NewMemoryRepository()),
		Snapshots:                segment.NewService(segment.NewMemoryRepository()),
	}).Handler()
	return handler, login.SessionToken
}

func TestOrganisationAndConsentReviewWorkflow(t *testing.T) {
	handler, token := testServer(t)
	orgBody := `{"legalName":"ABC Events Limited","tradingName":"ABC Events","countryISO2":"NG"}`
	orgRequest := httptest.NewRequest(http.MethodPost, "/api/v1/organisations", strings.NewReader(orgBody))
	orgRequest.Header.Set("Content-Type", "application/json")
	orgRequest.Header.Set("Authorization", "Bearer "+token)
	orgResponse := httptest.NewRecorder()
	handler.ServeHTTP(orgResponse, orgRequest)
	if orgResponse.Code != http.StatusCreated {
		t.Fatalf("create organisation: status=%d body=%s", orgResponse.Code, orgResponse.Body.String())
	}
	var org map[string]any
	_ = json.Unmarshal(orgResponse.Body.Bytes(), &org)
	orgID, _ := org["id"].(string)
	if orgID == "" {
		t.Fatal("organisation ID missing")
	}

	reviewPayload := map[string]any{
		"organisationId":           orgID,
		"name":                     "Event registration consent",
		"purposeDescription":       "Event promotions",
		"channel":                  "WHATSAPP",
		"consentSource":            "Registration form",
		"wordingVersion":           "v1",
		"privacyNoticeReviewed":    true,
		"optOutProcessReviewed":    true,
		"sampleRecordsReviewed":    true,
		"sampleReviewNotes":        "Reviewed 20 randomly selected records; identifiers were not retained.",
		"permittedCountries":       []string{"NG"},
		"permittedMessageCategory": "Entertainment",
		"restrictions":             "One campaign only",
	}
	encoded, _ := json.Marshal(reviewPayload)
	reviewRequest := httptest.NewRequest(http.MethodPost, "/api/v1/consent-reviews", bytes.NewReader(encoded))
	reviewRequest.Header.Set("Content-Type", "application/json")
	reviewRequest.Header.Set("Authorization", "Bearer "+token)
	reviewResponse := httptest.NewRecorder()
	handler.ServeHTTP(reviewResponse, reviewRequest)
	if reviewResponse.Code != http.StatusCreated {
		t.Fatalf("create review: status=%d body=%s", reviewResponse.Code, reviewResponse.Body.String())
	}
}

func TestImportPreviewDoesNotReturnRawNumbers(t *testing.T) {
	handler, token := testServer(t)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "audience.csv")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(file, "msisdn,country,state,lga,age,gender\n08012345678,NG,Lagos,Ikeja,27,Female\n")
	_ = writer.WriteField("defaultCountry", "NG")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/audience-imports/preview", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("preview: status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "08012345678") || strings.Contains(response.Body.String(), "+2348012345678") {
		t.Fatalf("preview leaked raw MSISDN: %s", response.Body.String())
	}
}

func TestGeographyReturnsLagosLGAs(t *testing.T) {
	handler, token := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/geography/areas?country=NG&parent=LAGOS&level=2", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "Ikeja") || !strings.Contains(response.Body.String(), "Surulere") {
		t.Fatalf("Lagos LGA catalogue incomplete: %s", response.Body.String())
	}
}

func TestSecurityHeadersAndRequestID(t *testing.T) {
	handler, _ := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("request ID header missing")
	}
	if response.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("security headers missing")
	}
}

func TestCampaignTransitionsResolveSnapshotAndMessageEvidenceServerSide(t *testing.T) {
	registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := identity.HashPassword("a-very-long-development-password")
	user := identity.User{ID: "00000000-0000-4000-8000-000000000111", Email: "checker@example.test", DisplayName: "Checker", Status: identity.StatusActive, PasswordHash: hash, MFARequired: false, Permissions: map[string]struct{}{"*": {}}}
	identityService := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := identityService.Login(context.Background(), user.Email, "a-very-long-development-password")
	if err != nil {
		t.Fatal(err)
	}
	orgs := organisation.NewService(organisation.NewMemoryRepository())
	org, err := orgs.Create(context.Background(), organisation.CreateInput{LegalName: "Evidence Org", CountryISO2: "NG"})
	if err != nil {
		t.Fatal(err)
	}
	consents := consent.NewService(consent.NewMemoryRepository())
	consents.Evidence = httpReviewEvidenceResolver{}
	from := time.Now().UTC().Add(-30 * 24 * time.Hour)
	to := time.Now().UTC().Add(-24 * time.Hour)
	review, err := consents.Create(context.Background(), consent.CreateInput{
		OrganisationID: org.ID, Scope: consent.ReviewScopeSource, SourceSystem: "registration", Name: "Reviewed source",
		PurposeDescription: "Event notifications", PurposeCode: "EVENT_NOTIFICATION", Channel: "WHATSAPP",
		ConsentSource: "form", CollectionMethod: "WEB_FORM", CollectionPeriodFrom: &from, CollectionPeriodTo: &to,
		ControllerRole: "CONTROLLER", WordingVersion: "v1", ExactConsentWording: "I agree to event notifications.",
		PrivacyNoticeVersion: "privacy-v1", EvidenceAssetIDs: []string{"asset-1"}, PrivacyNoticeReviewed: true,
		OptOutProcessReviewed: true, SampleRecordsReviewed: true, SampleReviewNotes: "Reviewed 20 randomly selected records; identifiers were not retained.", PermittedCountries: []string{"NG"},
		PermittedMessageCategory: "EVENT_NOTIFICATION", CreatedBy: "review-maker",
	})
	if err != nil {
		t.Fatal(err)
	}
	review, err = consents.Submit(context.Background(), review.ID, consent.SubmitInput{ActorID: "review-submitter", ExpectedVersion: review.Version, Reason: "evidence complete"})
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().UTC().Add(24 * time.Hour)
	review, err = consents.Decide(context.Background(), review.ID, consent.DecisionInput{ReviewerID: "privacy-reviewer", Decision: consent.StatusApproved, ExpiresAt: &expires, Reason: "consent approved", ExpectedVersion: review.Version})
	if err != nil {
		t.Fatal(err)
	}
	campaigns := campaign.NewService(campaign.NewMemoryRepository())
	entity, err := campaigns.Create(context.Background(), campaign.CreateInput{Transport: campaign.TransportSelection{Channel: "WHATSAPP", Provider: campaign.ProviderOpenWA, Engine: campaign.EngineBaileys, RoutingMode: campaign.RoutingSenderPool, GatewayPoolID: "gw-baileys", SenderPoolID: "11111111-1111-1111-1111-111111111111", AdapterVersion: "0.13.0", FallbackMode: campaign.FallbackNone, RoutingPolicyVersion: "route-v1", CapacityEvidenceVersion: "cap-v1"}, OrganisationID: org.ID, Name: "Authoritative evidence", PurposeID: "00000000-0000-4000-8000-000000000222", ConsentReviewID: review.ID, MaximumUniqueRecipients: 10, MaximumMessagesPerRecipient: 1, CreatedBy: "maker"})
	if err != nil {
		t.Fatal(err)
	}
	messages := message.NewService(message.NewMemoryRepository())
	snapshots := segment.NewService(segment.NewMemoryRepository())
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{Registry: registry, Compiler: cohort.NewCompiler(registry), Organisations: orgs, ConsentReviews: consents, Campaigns: campaigns, Geography: geography.DefaultCatalogue(), Identity: identityService, Messages: messages, Snapshots: snapshots}).Handler()
	transition := func(action campaign.Action, version int64, extra map[string]any) map[string]any {
		payload := map[string]any{"action": action, "expectedVersion": version}
		for k, v := range extra {
			payload[k] = v
		}
		raw, _ := json.Marshal(payload)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/"+entity.ID+"/transition", bytes.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+login.SessionToken)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("transition %s status=%d body=%s", action, response.Code, response.Body.String())
		}
		var out map[string]any
		_ = json.Unmarshal(response.Body.Bytes(), &out)
		return out
	}
	out := transition(campaign.ActionSubmitConsentReview, 1, nil)
	out = transition(campaign.ActionApproveConsent, int64(out["version"].(float64)), nil)
	out = transition(campaign.ActionStartAudienceBuild, int64(out["version"].(float64)), nil)
	definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorEquals, Values: []any{"NG"}}}}
	snapshot, err := snapshots.Create(context.Background(), segment.CreateInput{CampaignID: entity.ID, Definition: definition, DefinitionVersion: 1, ConsentPolicyVersion: "consent-v1", ConfigurationVersion: "config-v1", CreatedBy: "snapshot-worker", Members: []segment.Member{{ContactID: "00000000-0000-4000-8000-000000000333", EligibilityEvidenceHash: "evidence-hash"}}})
	if err != nil {
		t.Fatal(err)
	}
	out = transition(campaign.ActionValidateAudience, int64(out["version"].(float64)), map[string]any{"audienceSnapshotId": snapshot.ID})
	if out["audienceSnapshotHash"] != snapshot.SnapshotHash || int64(out["eligibleAudienceCount"].(float64)) != snapshot.EligibleCount {
		t.Fatalf("server did not resolve snapshot evidence: %#v", out)
	}
	draft, err := messages.CreateDraft(context.Background(), message.Input{CampaignID: entity.ID, Type: message.TypeText, Body: "approved canonical content", CreatedBy: "message-maker", IdempotencyKey: "message-request-server-test"})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := messages.Approve(context.Background(), draft.ID, user.ID, draft.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	out = transition(campaign.ActionSubmitMessage, int64(out["version"].(float64)), map[string]any{"messageVersionId": approved.ID})
	out = transition(campaign.ActionApproveMessage, int64(out["version"].(float64)), nil)
	if out["messageContentHash"] != approved.ContentHash {
		t.Fatalf("server did not resolve message hash: %#v", out)
	}
}

func TestCampaignTransitionRejectsClientSuppliedEvidenceHash(t *testing.T) {
	handler, token := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/not-used/transition", strings.NewReader(`{"action":"VALIDATE_AUDIENCE","expectedVersion":1,"audienceSnapshotId":"x","audienceSnapshotHash":"forged"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected unknown-field rejection, got %d %s", response.Code, response.Body.String())
	}
}

func TestCompletionUsesAuthoritativeMetricsAndRequiresReconciliation(t *testing.T) {
	metricsRepo := delivery.NewMemoryMetricsRepository()
	metricsRepo.Set("campaign-complete", delivery.Metrics{DeliveredTotal: 8, FailedTotal: 1, UnknownTotal: 1})
	server := &Server{deps: Dependencies{DeliveryMetrics: delivery.NewMetricsService(metricsRepo)}}
	input := campaign.TransitionInput{Action: campaign.ActionComplete, FailedCount: 0, UnknownCount: 0}
	current := campaign.Campaign{ID: "campaign-complete", EligibleAudienceCount: 10}
	if err := server.resolveCampaignEvidence(context.Background(), current, &input); err != nil {
		t.Fatal(err)
	}
	if input.FailedCount != 1 || input.UnknownCount != 1 {
		t.Fatalf("completion trusted caller instead of metrics: %+v", input)
	}
	metricsRepo.Set("campaign-complete", delivery.Metrics{AuthorisedTotal: 1, DeliveredTotal: 8, FailedTotal: 1})
	if err := server.resolveCampaignEvidence(context.Background(), current, &input); err == nil {
		t.Fatal("expected active obligation to block completion")
	}
}

func TestSignedGatewayCallbackIsAuthenticatedAndReplaySafe(t *testing.T) {
	secret := bytes.Repeat([]byte{9}, 32)
	now := time.Now().UTC()
	repository := delivery.NewMemoryRepository(delivery.Recipient{
		ID: "recipient-1", CampaignID: "campaign-1", ContactID: "contact-1", MessageVersionID: "message-1",
		IdempotencyKey: "delivery-key-1", Status: delivery.StatusSubmitting, UpdatedAt: now.Add(-time.Minute),
	})
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		DeliveryEvents: delivery.NewService(repository), GatewayCallbackSecret: secret,
		GatewayCallbackMaxSkew: 5 * time.Minute,
	}).Handler()
	body := []byte(`{"schemaVersion":"1.0","eventId":"evt-1","eventType":"message.delivered","sessionId":"session-1","clientReference":"recipient-1","providerMessageId":"provider-1","occurredAt":"` + now.Format(time.RFC3339Nano) + `"}`)
	timestamp, signature, err := gateway.SignCallback(secret, now, body)
	if err != nil {
		t.Fatal(err)
	}
	call := func(payload []byte, sig string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/internal/gateway/events", bytes.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(gateway.TimestampHeader, timestamp)
		request.Header.Set(gateway.SignatureHeader, sig)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	first := call(body, signature)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"replayed":false`) {
		t.Fatalf("first callback status=%d body=%s", first.Code, first.Body.String())
	}
	second := call(body, signature)
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"replayed":true`) {
		t.Fatalf("replay status=%d body=%s", second.Code, second.Body.String())
	}
	forged := call(append(append([]byte(nil), body...), ' '), signature)
	if forged.Code != http.StatusUnauthorized {
		t.Fatalf("forged callback accepted: %d %s", forged.Code, forged.Body.String())
	}
}

func TestGatewayCallbackCanResolveRecipientByProviderMessageID(t *testing.T) {
	secret := bytes.Repeat([]byte{7}, 32)
	now := time.Now().UTC()
	repository := delivery.NewMemoryRepository(delivery.Recipient{
		ID: "recipient-provider-lookup", CampaignID: "campaign-1", ContactID: "contact-1", MessageVersionID: "message-1",
		IdempotencyKey: "delivery-key-provider", Status: delivery.StatusGatewayAccepted, ProviderMessageID: "provider-lookup-1", UpdatedAt: now.Add(-time.Minute),
	})
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		DeliveryEvents: delivery.NewService(repository), GatewayCallbackSecret: secret,
		GatewayCallbackMaxSkew: 5 * time.Minute,
	}).Handler()
	body := []byte(`{"schemaVersion":"1.0","eventId":"evt-provider-lookup","eventType":"message.delivered","sessionId":"session-1","providerMessageId":"provider-lookup-1","occurredAt":"` + now.Format(time.RFC3339Nano) + `"}`)
	timestamp, signature, err := gateway.SignCallback(secret, now, body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/internal/gateway/events", bytes.NewReader(body))
	request.Header.Set(gateway.TimestampHeader, timestamp)
	request.Header.Set(gateway.SignatureHeader, signature)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"recipientId":"recipient-provider-lookup"`) {
		t.Fatalf("provider lookup callback status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGatewayCallbackQueuesAuthenticatedUnmatchedEventForReconciliation(t *testing.T) {
	secret := bytes.Repeat([]byte{6}, 32)
	now := time.Now().UTC()
	repository := delivery.NewMemoryRepository()
	queue := delivery.NewMemoryUnmatchedEventStore()
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		DeliveryEvents: delivery.NewService(repository), UnmatchedDeliveryEvents: queue, GatewayCallbackSecret: secret,
		GatewayCallbackMaxSkew: 5 * time.Minute,
	}).Handler()
	body := []byte(`{"schemaVersion":"1.0","eventId":"evt-unmatched","eventType":"message.delivered","sessionId":"session-1","providerMessageId":"provider-missing","occurredAt":"` + now.Format(time.RFC3339Nano) + `"}`)
	timestamp, signature, err := gateway.SignCallback(secret, now, body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/internal/gateway/events", bytes.NewReader(body))
	request.Header.Set(gateway.TimestampHeader, timestamp)
	request.Header.Set(gateway.SignatureHeader, signature)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"GATEWAY_EVENT_DEFERRED"`) {
		t.Fatalf("unmatched callback status=%d body=%s", response.Code, response.Body.String())
	}
	items, err := queue.List(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ProviderEventID != "evt-unmatched" || items[0].ProviderMessageID != "provider-missing" {
		t.Fatalf("unmatched queue=%+v", items)
	}
	conflictBody := []byte(`{"schemaVersion":"1.0","eventId":"evt-unmatched","eventType":"message.read","sessionId":"session-1","providerMessageId":"provider-missing","occurredAt":"` + now.Format(time.RFC3339Nano) + `"}`)
	conflictTimestamp, conflictSignature, err := gateway.SignCallback(secret, now, conflictBody)
	if err != nil {
		t.Fatal(err)
	}
	conflictRequest := httptest.NewRequest(http.MethodPost, "/api/v1/internal/gateway/events", bytes.NewReader(conflictBody))
	conflictRequest.Header.Set(gateway.TimestampHeader, conflictTimestamp)
	conflictRequest.Header.Set(gateway.SignatureHeader, conflictSignature)
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflictRequest)
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("unmatched replay conflict status=%d body=%s", conflictResponse.Code, conflictResponse.Body.String())
	}

	if err := repository.Create(context.Background(), delivery.Recipient{
		ID: "recipient-late-correlation", Status: delivery.StatusSubmitting,
		ProviderMessageID: "provider-missing", UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	lateConflictRequest := httptest.NewRequest(http.MethodPost, "/api/v1/internal/gateway/events", bytes.NewReader(conflictBody))
	lateConflictRequest.Header.Set(gateway.TimestampHeader, conflictTimestamp)
	lateConflictRequest.Header.Set(gateway.SignatureHeader, conflictSignature)
	lateConflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(lateConflictResponse, lateConflictRequest)
	if lateConflictResponse.Code != http.StatusConflict {
		t.Fatalf("late correlated replay conflict status=%d body=%s", lateConflictResponse.Code, lateConflictResponse.Body.String())
	}
	items, err = queue.List(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ProviderEventID != "evt-unmatched" {
		t.Fatalf("conflicting late correlation cleared unmatched evidence: %+v", items)
	}
	unmodified, err := repository.Get(context.Background(), "recipient-late-correlation")
	if err != nil {
		t.Fatal(err)
	}
	if unmodified.Status != delivery.StatusSubmitting {
		t.Fatalf("conflicting late correlation mutated recipient before replay validation: %+v", unmodified)
	}

	retryRequest := httptest.NewRequest(http.MethodPost, "/api/v1/internal/gateway/events", bytes.NewReader(body))
	retryRequest.Header.Set(gateway.TimestampHeader, timestamp)
	retryRequest.Header.Set(gateway.SignatureHeader, signature)
	retryResponse := httptest.NewRecorder()
	handler.ServeHTTP(retryResponse, retryRequest)
	if retryResponse.Code != http.StatusOK || !strings.Contains(retryResponse.Body.String(), `"recipientId":"recipient-late-correlation"`) {
		t.Fatalf("correlated retry status=%d body=%s", retryResponse.Code, retryResponse.Body.String())
	}
	items, err = queue.List(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("resolved unmatched event remained pending: %+v", items)
	}
}

func TestGatewayCallbackWithNoQueuedUnmatchedRecordRemainsValid(t *testing.T) {
	secret := bytes.Repeat([]byte{10}, 32)
	now := time.Now().UTC()
	repository := delivery.NewMemoryRepository(delivery.Recipient{ID: "recipient-no-queue", Status: delivery.StatusSubmitting, ProviderMessageID: "provider-no-queue", UpdatedAt: now})
	queue := delivery.NewMemoryUnmatchedEventStore()
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{DeliveryEvents: delivery.NewService(repository), UnmatchedDeliveryEvents: queue, GatewayCallbackSecret: secret}).Handler()
	body := []byte(`{"schemaVersion":"1.0","eventId":"evt-no-queue","eventType":"message.delivered","sessionId":"session-1","providerMessageId":"provider-no-queue","occurredAt":"` + now.Format(time.RFC3339Nano) + `"}`)
	ts, sig, err := gateway.SignCallback(secret, now, body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/gateway/events", bytes.NewReader(body))
	req.Header.Set(gateway.TimestampHeader, ts)
	req.Header.Set(gateway.SignatureHeader, sig)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("matched callback without queued evidence status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestGatewayCallbackRejectsEventIDReuseWithDifferentEvidence(t *testing.T) {
	secret := bytes.Repeat([]byte{8}, 32)
	now := time.Now().UTC()
	repository := delivery.NewMemoryRepository(delivery.Recipient{ID: "recipient-1", CampaignID: "campaign-1", ContactID: "contact-1", MessageVersionID: "message-1", IdempotencyKey: "delivery-key-1", Status: delivery.StatusSubmitting, UpdatedAt: now})
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{DeliveryEvents: delivery.NewService(repository), GatewayCallbackSecret: secret}).Handler()
	post := func(body []byte) *httptest.ResponseRecorder {
		ts, sig, err := gateway.SignCallback(secret, now, body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/gateway/events", bytes.NewReader(body))
		req.Header.Set(gateway.TimestampHeader, ts)
		req.Header.Set(gateway.SignatureHeader, sig)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	accepted := []byte(`{"schemaVersion":"1.0","eventId":"evt-conflict","eventType":"message.sent","sessionId":"session-1","clientReference":"recipient-1","providerMessageId":"provider-1","occurredAt":"` + now.Format(time.RFC3339Nano) + `"}`)
	if response := post(accepted); response.Code != http.StatusOK {
		t.Fatalf("initial event: %d %s", response.Code, response.Body.String())
	}
	conflict := []byte(`{"schemaVersion":"1.0","eventId":"evt-conflict","eventType":"message.delivered","sessionId":"session-1","clientReference":"recipient-1","providerMessageId":"provider-1","occurredAt":"` + now.Format(time.RFC3339Nano) + `"}`)
	if response := post(conflict); response.Code != http.StatusConflict {
		t.Fatalf("conflicting replay: %d %s", response.Code, response.Body.String())
	}
}

type cleanImportScanner struct{}

func (cleanImportScanner) Scan(_ context.Context, source io.Reader) (malware.Result, error) {
	_, _ = io.Copy(io.Discard, source)
	return malware.Result{Clean: true, Response: "clean"}, nil
}

func importRequestBody(t *testing.T, fileContents string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadata, err := writer.CreateFormField("metadata")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(metadata, `{"organisationId":"org-1","consentReviewId":"review-1","purposeId":"purpose-1","channel":"WHATSAPP","wordingVersion":"v1","sourceName":"Approved source","defaultCountryIso2":"NG","templateVersion":"audience-v1","mapping":{"MSISDN":"msisdn","Country":"country","State":"state","LGA":"lga","Age":"age","Gender":"gender"},"updatePolicy":"NEWEST_SOURCE"}`)
	file, err := writer.CreateFormFile("file", "audience.csv")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(file, fileContents)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, writer.FormDataContentType()
}

func TestSecureAudienceImportIntakeIsStreamingAndReplaySafe(t *testing.T) {
	hash, err := identity.HashPassword("a-very-long-development-password")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{ID: "uploader-1", Email: "uploader@example.test", DisplayName: "Uploader", Status: identity.StatusActive, PasswordHash: hash, Permissions: map[string]struct{}{"*": {}}}
	identityService := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := identityService.Login(context.Background(), user.Email, "a-very-long-development-password")
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repository := importer.NewMemoryImportRepository()
	imports := &importer.ImportService{Repository: repository}
	intake := &importer.IntakeService{Store: store, Scanner: cleanImportScanner{}, Imports: imports, MaxFileSize: 1 << 20}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{Identity: identityService, AudienceImports: imports, AudienceImportIntake: intake, MaxImportFileBytes: 1 << 20}).Handler()
	post := func(contents string) *httptest.ResponseRecorder {
		body, contentType := importRequestBody(t, contents)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/audience-imports", body)
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Authorization", "Bearer "+login.SessionToken)
		request.Header.Set("Idempotency-Key", "audience-import-request-0001")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	csv := "msisdn,country,state,lga,age,gender\n08012345678,NG,Lagos,Ikeja,27,Female\n"
	first := post(csv)
	if first.Code != http.StatusCreated || !strings.Contains(first.Body.String(), `"status":"VALIDATING"`) {
		t.Fatalf("initial intake: %d %s", first.Code, first.Body.String())
	}
	if strings.Contains(first.Body.String(), "08012345678") || strings.Contains(first.Body.String(), "+2348012345678") {
		t.Fatalf("intake response leaked raw MSISDN: %s", first.Body.String())
	}
	replay := post(csv)
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), `"created":false`) {
		t.Fatalf("exact replay: %d %s", replay.Code, replay.Body.String())
	}
	conflict := post(strings.Replace(csv, "08012345678", "08099999999", 1))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("altered replay accepted: %d %s", conflict.Code, conflict.Body.String())
	}
}

func TestFilterDefinitionAdministrationAndCohortUse(t *testing.T) {
	handler, token := testServer(t)
	body := `{"code":"OCCUPATION","displayName":"Occupation","description":"Self-declared occupation","dataType":"single_select","allowedValues":["Engineer","Teacher"],"reportable":true,"displayOrder":80,"reason":"approved new audience dimension"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/filter-definitions", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create filter: status=%d body=%s", response.Code, response.Body.String())
	}

	cohortBody := `{"join":"AND","rules":[{"definitionCode":"OCCUPATION","operator":"in","values":["Engineer"]}]}`
	cohortRequest := httptest.NewRequest(http.MethodPost, "/api/v1/cohorts/validate", strings.NewReader(cohortBody))
	cohortRequest.Header.Set("Content-Type", "application/json")
	cohortRequest.Header.Set("Authorization", "Bearer "+token)
	cohortResponse := httptest.NewRecorder()
	handler.ServeHTTP(cohortResponse, cohortRequest)
	if cohortResponse.Code != http.StatusOK {
		t.Fatalf("validate dynamic filter: status=%d body=%s", cohortResponse.Code, cohortResponse.Body.String())
	}
}

func TestFilterDefinitionRejectsIdempotentSchemaAbuseAndMandatoryDisable(t *testing.T) {
	handler, token := testServer(t)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/filter-definitions/COUNTRY", strings.NewReader(`{"displayName":"Country","operators":["in"],"filterable":false,"reportable":true,"active":false,"displayOrder":10,"expectedVersion":1,"reason":"disable core geography"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mandatory filter disable: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestReadinessReflectsDependencyAndExternalChecks(t *testing.T) {
	handler, _ := testServer(t)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected ready server, status=%d body=%s", response.Code, response.Body.String())
	}

	unready := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		ReadinessChecks: []ReadinessCheck{{Name: "schema", Check: func(context.Context) error {
			return errors.New("migration missing")
		}}},
	}).Handler()
	response = httptest.NewRecorder()
	unready.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected unready server, status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"schema":"failed"`) || !strings.Contains(response.Body.String(), `"status":"not_ready"`) {
		t.Fatalf("unexpected readiness body: %s", response.Body.String())
	}
}

type testInboundAudit struct{}

func (*testInboundAudit) RecordInboundAudit(context.Context, inbound.AuditRecord) error { return nil }

func TestOrganisationLifecycleAPI(t *testing.T) {
	handler, token := testServer(t)
	create := httptest.NewRequest(http.MethodPost, "/api/v1/organisations", strings.NewReader(`{"legalName":"Lifecycle Org","countryISO2":"NG","primaryContactEmail":"ops@example.test"}`))
	create.Header.Set("Content-Type", "application/json")
	create.Header.Set("Authorization", "Bearer "+token)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var org organisation.Organisation
	if err := json.Unmarshal(created.Body.Bytes(), &org); err != nil {
		t.Fatal(err)
	}
	updateBody := `{"expectedVersion":1,"legalName":"Lifecycle Organisation Limited","countryISO2":"NG","primaryContactEmail":"ops@example.test","reason":"registered legal name"}`
	update := httptest.NewRequest(http.MethodPut, "/api/v1/organisations/"+org.ID, strings.NewReader(updateBody))
	update.Header.Set("Content-Type", "application/json")
	update.Header.Set("Authorization", "Bearer "+token)
	updated := httptest.NewRecorder()
	handler.ServeHTTP(updated, update)
	if updated.Code != http.StatusOK {
		t.Fatalf("update: %d %s", updated.Code, updated.Body.String())
	}
	status := httptest.NewRequest(http.MethodPost, "/api/v1/organisations/"+org.ID+"/status", strings.NewReader(`{"expectedVersion":2,"status":"SUSPENDED","reason":"consent basis under review"}`))
	status.Header.Set("Content-Type", "application/json")
	status.Header.Set("Authorization", "Bearer "+token)
	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, status)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("status: %d %s", statusResponse.Code, statusResponse.Body.String())
	}
	events := httptest.NewRequest(http.MethodGet, "/api/v1/organisations/"+org.ID+"/events", nil)
	events.Header.Set("Authorization", "Bearer "+token)
	eventsResponse := httptest.NewRecorder()
	handler.ServeHTTP(eventsResponse, events)
	if eventsResponse.Code != http.StatusOK {
		t.Fatalf("events: %d %s", eventsResponse.Code, eventsResponse.Body.String())
	}
	if !strings.Contains(eventsResponse.Body.String(), "STATUS_CHANGED") {
		t.Fatalf("missing status event: %s", eventsResponse.Body.String())
	}
	stale := httptest.NewRequest(http.MethodPost, "/api/v1/organisations/"+org.ID+"/status", strings.NewReader(`{"expectedVersion":2,"status":"ACTIVE","reason":"stale request"}`))
	stale.Header.Set("Content-Type", "application/json")
	stale.Header.Set("Authorization", "Bearer "+token)
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, stale)
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf("stale expected conflict: %d %s", staleResponse.Code, staleResponse.Body.String())
	}
}
