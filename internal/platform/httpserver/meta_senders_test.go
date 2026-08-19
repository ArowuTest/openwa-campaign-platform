package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/message"
	"campaign-platform/internal/metacloud"
)

type metaVerifierStub struct {
	result metacloud.SenderVerification
	err    error
	calls  int
}

func (v *metaVerifierStub) VerifySender(_ context.Context, _ metacloud.Sender) (metacloud.SenderVerification, error) {
	v.calls++
	return v.result, v.err
}

type metaAdminHarness struct {
	handler   http.Handler
	tokens    map[string]string
	sessions  *identity.MemorySessionRepository
	verifier  *metaVerifierStub
	senders   *metacloud.Service
	templates *metacloud.TemplateService
	messages  *message.Service
}

func newMetaAdminHarness(t *testing.T) metaAdminHarness {
	t.Helper()
	password := "a-very-long-meta-admin-password"
	hash, err := identity.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	permissions := map[string]struct{}{"sender.read": {}, "sender.admin": {}, "configuration.approve": {}, "campaign.read": {}, "campaign.approve": {}}
	users := []identity.User{
		{ID: "00000000-0000-4000-8000-000000000311", Email: "meta-maker@example.test", DisplayName: "Meta Maker", Status: identity.StatusActive, PasswordHash: hash, Permissions: permissions},
		{ID: "00000000-0000-4000-8000-000000000312", Email: "meta-submitter@example.test", DisplayName: "Meta Submitter", Status: identity.StatusActive, PasswordHash: hash, Permissions: permissions},
		{ID: "00000000-0000-4000-8000-000000000313", Email: "meta-checker@example.test", DisplayName: "Meta Checker", Status: identity.StatusActive, PasswordHash: hash, Permissions: permissions},
	}
	sessions := identity.NewMemorySessionRepository()
	identityService := identity.NewPersistentService(identity.NewMemoryRepository(users...), sessions, identity.NewMemoryChallengeRepository(), 30*time.Minute, 12*time.Hour)
	tokens := map[string]string{}
	for _, user := range users {
		login, err := identityService.Login(context.Background(), user.Email, password)
		if err != nil {
			t.Fatal(err)
		}
		tokens[user.ID] = login.SessionToken
	}
	for _, userID := range []string{users[0].ID, users[2].ID} {
		digest := sha256.Sum256([]byte(tokens[userID]))
		if _, err := sessions.MarkMFAVerified(context.Background(), digest, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	verifier := &metaVerifierStub{result: metacloud.SenderVerification{PhoneNumberID: "phone-1", DisplayPhoneNumber: "+234 801 234 5678", VerifiedName: "Groove", QualityRating: "GREEN"}}
	senders := &metacloud.Service{Store: metacloud.NewMemoryStore()}
	messages := message.NewService(message.NewMemoryRepository())
	templateClient := &metaTemplateListerStub{values: []metacloud.Template{{MetaTemplateID: "tpl-1", Name: "hello_name", Language: "en_US", Category: "MARKETING", Status: "APPROVED", Components: json.RawMessage(`[{"type":"BODY","text":"Hello {{1}}"}]`)}}}
	templates := &metacloud.TemplateService{Store: metacloud.NewMemoryTemplateStore(), Client: templateClient, Messages: messages}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Identity: identityService, Messages: messages, MetaSenders: senders, MetaTemplates: templates, MetaCredentials: credentials, MetaVerifier: verifier,
	}).Handler()
	return metaAdminHarness{handler: handler, tokens: tokens, sessions: sessions, verifier: verifier, senders: senders, templates: templates, messages: messages}
}

func metaAdminCall(t *testing.T, handler http.Handler, token, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	request := httptest.NewRequest(method, path, body)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func TestMetaSenderAdminLifecycleEnforcesMakerCheckerAndNeverReturnsSecrets(t *testing.T) {
	h := newMetaAdminHarness(t)
	maker := "00000000-0000-4000-8000-000000000311"
	submitter := "00000000-0000-4000-8000-000000000312"
	checker := "00000000-0000-4000-8000-000000000313"
	create := metaAdminCall(t, h.handler, h.tokens[maker], http.MethodPost, "/api/v1/admin/meta-senders", map[string]any{
		"organisationId": "00000000-0000-4000-8000-000000000401", "senderPoolId": "00000000-0000-4000-8000-000000000402",
		"wabaId": "waba-1", "phoneNumberId": "phone-1", "displayName": "Groove Meta", "businessPhoneDisplay": "+234 801 234 5678",
		"credentialKey": "meta-ng", "graphApiVersion": "v23.0", "reason": "create governed Meta sender",
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", create.Code, create.Body.String())
	}
	assertNoMetaSecrets(t, create.Body.String())
	var sender metacloud.Sender
	if err := json.Unmarshal(create.Body.Bytes(), &sender); err != nil || sender.ID == "" || sender.Version != 1 {
		t.Fatalf("sender=%#v err=%v", sender, err)
	}
	submit := metaAdminCall(t, h.handler, h.tokens[submitter], http.MethodPost, "/api/v1/admin/meta-senders/"+sender.ID+"/submit", map[string]any{"expectedVersion": 1, "reason": "submit Meta sender for approval"})
	if submit.Code != http.StatusOK {
		t.Fatalf("submit=%d %s", submit.Code, submit.Body.String())
	}
	creatorDecision := metaAdminCall(t, h.handler, h.tokens[maker], http.MethodPost, "/api/v1/admin/meta-senders/"+sender.ID+"/decision", map[string]any{"expectedVersion": 2, "approve": true, "reason": "creator must not approve own sender"})
	if creatorDecision.Code != http.StatusUnprocessableEntity {
		t.Fatalf("creator decision=%d %s", creatorDecision.Code, creatorDecision.Body.String())
	}
	decision := metaAdminCall(t, h.handler, h.tokens[checker], http.MethodPost, "/api/v1/admin/meta-senders/"+sender.ID+"/decision", map[string]any{"expectedVersion": 2, "approve": true, "reason": "independent checker approved sender"})
	if decision.Code != http.StatusOK {
		t.Fatalf("decision=%d %s", decision.Code, decision.Body.String())
	}
	assertNoMetaSecrets(t, decision.Body.String())
	if err := json.Unmarshal(decision.Body.Bytes(), &sender); err != nil || sender.Status != metacloud.StatusActive || sender.Version != 3 {
		t.Fatalf("approved sender=%#v err=%v", sender, err)
	}
	verify := metaAdminCall(t, h.handler, h.tokens[checker], http.MethodPost, "/api/v1/admin/meta-senders/"+sender.ID+"/verify", map[string]any{"expectedVersion": 3, "reason": "verified WABA phone ownership"})
	if verify.Code != http.StatusOK {
		t.Fatalf("verify=%d %s", verify.Code, verify.Body.String())
	}
	assertNoMetaSecrets(t, verify.Body.String())
	if h.verifier.calls != 1 {
		t.Fatalf("verify calls=%d", h.verifier.calls)
	}
	if strings.Contains(verify.Body.String(), "token-value") || strings.Contains(verify.Body.String(), "verify-token") || strings.Contains(verify.Body.String(), "meta-app-secret") {
		t.Fatalf("verify response leaked credential material: %s", verify.Body.String())
	}
}

func assertNoMetaSecrets(t *testing.T, body string) {
	t.Helper()
	for _, secretField := range []string{"accessToken", "appSecret", "verifyToken", "token-value", "meta-app-secret", "verify-token"} {
		if strings.Contains(body, secretField) {
			t.Fatalf("Meta secret leaked in response: %s", body)
		}
	}
}

type metaTemplateListerStub struct {
	values []metacloud.Template
	err    error
	calls  int
}

func (s *metaTemplateListerStub) ListTemplates(_ context.Context, _ metacloud.TemplateListRequest) ([]metacloud.Template, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	out := make([]metacloud.Template, len(s.values))
	copy(out, s.values)
	return out, nil
}

func TestMetaTemplateSyncListAndMessageBindingUseGovernedSenderEvidence(t *testing.T) {
	h := newMetaAdminHarness(t)
	maker := "00000000-0000-4000-8000-000000000311"
	submitter := "00000000-0000-4000-8000-000000000312"
	checker := "00000000-0000-4000-8000-000000000313"
	draft, err := h.senders.CreateDraft(context.Background(), metacloud.Sender{
		OrganisationID: "00000000-0000-4000-8000-000000000401", SenderPoolID: "00000000-0000-4000-8000-000000000402",
		WABAID: "waba-1", PhoneNumberID: "phone-1", DisplayName: "Groove Meta", BusinessPhoneDisplay: "+234 801 234 5678",
		CredentialKey: "meta-ng", GraphAPIVersion: "v23.0",
	}, maker, "create sender for template test")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := h.senders.Submit(context.Background(), draft.ID, draft.Version, submitter, "submit sender for template test")
	if err != nil {
		t.Fatal(err)
	}
	active, err := h.senders.Decide(context.Background(), pending.ID, pending.Version, true, checker, "approve sender for template test", time.Now().UTC().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	syncResponse := metaAdminCall(t, h.handler, h.tokens[maker], http.MethodPost, "/api/v1/admin/meta-senders/"+active.ID+"/templates/sync", map[string]any{"expectedVersion": active.Version, "reason": "synchronise approved Meta templates"})
	if syncResponse.Code != http.StatusOK {
		t.Fatalf("template sync=%d %s", syncResponse.Code, syncResponse.Body.String())
	}
	listResponse := metaAdminCall(t, h.handler, h.tokens[maker], http.MethodGet, "/api/v1/admin/meta-senders/"+active.ID+"/templates", nil)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), "hello_name") {
		t.Fatalf("template list=%d %s", listResponse.Code, listResponse.Body.String())
	}
	messageDraft, err := h.messages.CreateDraft(context.Background(), message.Input{
		CampaignID: "00000000-0000-4000-8000-000000000501", Type: message.TypeText, Body: "Hello {{first_name}}",
		Variables: []message.Variable{{Name: "first_name", DataType: "TEXT"}}, CreatedBy: maker, IdempotencyKey: "meta-binding-message-0001",
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := h.messages.Approve(context.Background(), messageDraft.ID, checker, messageDraft.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	bindingResponse := metaAdminCall(t, h.handler, h.tokens[checker], http.MethodPost, "/api/v1/message-versions/"+approved.ID+"/meta-binding", map[string]any{
		"metaSenderId": active.ID, "templateName": "hello_name", "language": "en_US",
		"componentBindings": []map[string]any{{"type": "BODY", "subType": "TEXT", "parameterNames": []string{"first_name"}}},
	})
	if bindingResponse.Code != http.StatusCreated {
		t.Fatalf("binding create=%d %s", bindingResponse.Code, bindingResponse.Body.String())
	}
	assertNoMetaSecrets(t, bindingResponse.Body.String())
	var binding metacloud.Binding
	if err := json.Unmarshal(bindingResponse.Body.Bytes(), &binding); err != nil || binding.MessageVersionID != approved.ID || len(binding.ComponentBindings) != 1 || len(binding.TemplateComponentHash) != 64 {
		t.Fatalf("binding=%#v err=%v", binding, err)
	}
	getBinding := metaAdminCall(t, h.handler, h.tokens[maker], http.MethodGet, "/api/v1/message-versions/"+approved.ID+"/meta-binding", nil)
	if getBinding.Code != http.StatusOK || !strings.Contains(getBinding.Body.String(), "hello_name") {
		t.Fatalf("binding get=%d %s", getBinding.Code, getBinding.Body.String())
	}
}
