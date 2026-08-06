package testmessage

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/dispatch"
	"campaign-platform/internal/message"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

func testProtector(t *testing.T) *sharedcrypto.MSISDNProtector {
	t.Helper()
	p, err := sharedcrypto.NewMSISDNProtector(make([]byte, 32), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func approvedRecipient(t *testing.T, svc *Service) Recipient {
	t.Helper()
	ctx := context.Background()
	r, err := svc.CreateRecipient(ctx, "Operations test", "+2348012345678", "maker", "approved QA number")
	if err != nil {
		t.Fatal(err)
	}
	r, err = svc.SubmitRecipient(ctx, r.ID, "maker", "submit for approval", r.Version)
	if err != nil {
		t.Fatal(err)
	}
	r, err = svc.DecideRecipient(ctx, r.ID, "checker", "independently approved", r.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func draftMessage(t *testing.T) *message.Service {
	t.Helper()
	svc := message.NewService(message.NewMemoryRepository())
	_, err := svc.CreateDraft(context.Background(), message.Input{CampaignID: "campaign-1", Type: message.TypeText, Body: "Hello {{name}}", Variables: []message.Variable{{Name: "name", DataType: "TEXT"}}, CreatedBy: "maker", IdempotencyKey: "test-message-draft-0001"})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}
func TestApprovedTestRecipientAndIdempotentScheduling(t *testing.T) {
	repo := NewMemoryRepository()
	msgs := draftMessage(t)
	svc := &Service{Repository: repo, Protector: testProtector(t), Messages: msgs, Routes: RouteValidatorFunc(func(context.Context, RouteRequirements) (RouteEvidence, error) {
		return RouteEvidence{GatewayPoolVersion: 1, AdapterVersion: "0.13.0", ProviderDefinitionID: "definition-1", ProviderDefinitionVersion: 1, GatewayNodeID: "node-1", GatewayNodeVersion: 1, SessionLeaseVersion: 1, SessionConfigurationVersion: 1, AuthorityExpiresAt: time.Now().UTC().Add(10 * time.Minute)}, nil
	})}
	r := approvedRecipient(t, svc)
	versions, _ := msgs.ListByCampaign(context.Background(), "campaign-1")
	send, err := svc.Schedule(context.Background(), "campaign-1", versions[0].ID, r.ID, "gateway-1", "pool-1", "OPENWA", "BAILEYS", "session-1", "operator", "pre-launch content check", "test-send-idempotency-0001", map[string]string{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Schedule(context.Background(), "campaign-1", versions[0].ID, r.ID, "gateway-1", "pool-1", "OPENWA", "BAILEYS", "session-1", "operator", "pre-launch content check", "test-send-idempotency-0001", map[string]string{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if send.ID != again.ID {
		t.Fatal("idempotent replay created a second test send")
	}
}
func TestTestSendRejectsUnapprovedRecipient(t *testing.T) {
	repo := NewMemoryRepository()
	msgs := draftMessage(t)
	svc := &Service{Repository: repo, Protector: testProtector(t), Messages: msgs, Routes: RouteValidatorFunc(func(context.Context, RouteRequirements) (RouteEvidence, error) {
		return RouteEvidence{GatewayPoolVersion: 1, AdapterVersion: "0.13.0", ProviderDefinitionID: "definition-1", ProviderDefinitionVersion: 1, GatewayNodeID: "node-1", GatewayNodeVersion: 1, SessionLeaseVersion: 1, SessionConfigurationVersion: 1, AuthorityExpiresAt: time.Now().UTC().Add(10 * time.Minute)}, nil
	})}
	r, err := svc.CreateRecipient(context.Background(), "Draft", "+2348012345678", "maker", "draft test recipient")
	if err != nil {
		t.Fatal(err)
	}
	versions, _ := msgs.ListByCampaign(context.Background(), "campaign-1")
	_, err = svc.Schedule(context.Background(), "campaign-1", versions[0].ID, r.ID, "g", "", "OPENWA", "WHATSAPP_WEB_JS", "s", "operator", "pre-launch content check", "test-send-idempotency-0002", map[string]string{"name": "Ada"})
	if !errors.Is(err, ErrRecipientNotActive) {
		t.Fatalf("expected inactive recipient, got %v", err)
	}
}

type acceptingGateway struct{}

func (acceptingGateway) Send(context.Context, dispatch.GatewayRequest) (dispatch.GatewayResult, error) {
	return dispatch.GatewayResult{Accepted: true, ProviderMessageID: "wamid.test", AcceptedAt: time.Now()}, nil
}
func TestProcessorRendersDraftAndRecordsAcceptance(t *testing.T) {
	repo := NewMemoryRepository()
	msgs := draftMessage(t)
	svc := &Service{Repository: repo, Protector: testProtector(t), Messages: msgs, Routes: RouteValidatorFunc(func(context.Context, RouteRequirements) (RouteEvidence, error) {
		return RouteEvidence{GatewayPoolVersion: 1, AdapterVersion: "0.13.0", ProviderDefinitionID: "definition-1", ProviderDefinitionVersion: 1, GatewayNodeID: "node-1", GatewayNodeVersion: 1, SessionLeaseVersion: 1, SessionConfigurationVersion: 1, AuthorityExpiresAt: time.Now().UTC().Add(10 * time.Minute)}, nil
	})}
	r := approvedRecipient(t, svc)
	versions, _ := msgs.ListByCampaign(context.Background(), "campaign-1")
	send, err := svc.Schedule(context.Background(), "campaign-1", versions[0].ID, r.ID, "gateway-1", "pool-1", "OPENWA", "WHATSAPP_WEB_JS", "session-1", "operator", "pre-launch content check", "test-send-idempotency-0003", map[string]string{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimSends(context.Background(), "worker", time.Now(), time.Minute, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	p := &Processor{Repository: repo, Protector: svc.Protector, Messages: msgs, Routes: svc.Routes, Gateway: acceptingGateway{}}
	if err := p.Process(context.Background(), claimed[0]); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetSend(context.Background(), send.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != SendAccepted || got.ProviderMessageID != "wamid.test" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestScheduleFreezesGovernedRouteEvidence(t *testing.T) {
	repo := NewMemoryRepository()
	msgs := draftMessage(t)
	var captured RouteRequirements
	svc := &Service{Repository: repo, Protector: testProtector(t), Messages: msgs, Routes: RouteValidatorFunc(func(_ context.Context, requirements RouteRequirements) (RouteEvidence, error) {
		captured = requirements
		return RouteEvidence{GatewayPoolVersion: 1, AdapterVersion: "0.13.0", ProviderDefinitionID: "definition-42", ProviderDefinitionVersion: 7, GatewayNodeID: "node-1", GatewayNodeVersion: 1, SessionLeaseVersion: 1, SessionConfigurationVersion: 1, AuthorityExpiresAt: time.Now().UTC().Add(10 * time.Minute)}, nil
	})}
	r := approvedRecipient(t, svc)
	versions, _ := msgs.ListByCampaign(context.Background(), "campaign-1")
	send, err := svc.Schedule(context.Background(), "campaign-1", versions[0].ID, r.ID, "gateway-1", "pool-1", "OPENWA", "BAILEYS", "session-1", "operator", "pre-launch content check", "test-send-idempotency-0004", map[string]string{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if send.ProviderAdapterVersion != "0.13.0" || send.ProviderDefinitionID != "definition-42" || send.ProviderDefinitionVersion != 7 {
		t.Fatalf("governed route evidence was not frozen: %+v", send)
	}
	if len(captured.RequiredCapabilities) != 1 || captured.RequiredCapabilities[0] != "SEND_TEXT" {
		t.Fatalf("unexpected route requirements: %+v", captured)
	}
}

func TestIdempotentReplayRejectsChangedGovernedRoute(t *testing.T) {
	repo := NewMemoryRepository()
	msgs := draftMessage(t)
	definitionVersion := int64(1)
	svc := &Service{Repository: repo, Protector: testProtector(t), Messages: msgs, Routes: RouteValidatorFunc(func(context.Context, RouteRequirements) (RouteEvidence, error) {
		return RouteEvidence{GatewayPoolVersion: 1, AdapterVersion: "0.13.0", ProviderDefinitionID: "definition-1", ProviderDefinitionVersion: definitionVersion, GatewayNodeID: "node-1", GatewayNodeVersion: 1, SessionLeaseVersion: 1, SessionConfigurationVersion: 1, AuthorityExpiresAt: time.Now().UTC().Add(10 * time.Minute)}, nil
	})}
	r := approvedRecipient(t, svc)
	versions, _ := msgs.ListByCampaign(context.Background(), "campaign-1")
	_, err := svc.Schedule(context.Background(), "campaign-1", versions[0].ID, r.ID, "gateway-1", "pool-1", "OPENWA", "BAILEYS", "session-1", "operator", "pre-launch content check", "test-send-idempotency-0005", map[string]string{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	definitionVersion = 2
	_, err = svc.Schedule(context.Background(), "campaign-1", versions[0].ID, r.ID, "gateway-1", "pool-1", "OPENWA", "BAILEYS", "session-1", "operator", "pre-launch content check", "test-send-idempotency-0005", map[string]string{"name": "Ada"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected idempotency conflict after route evidence changed, got %v", err)
	}
}

func TestProcessorFailsClosedWhenGovernedRouteDrifts(t *testing.T) {
	repo := NewMemoryRepository()
	msgs := draftMessage(t)
	scheduleRoutes := RouteValidatorFunc(func(context.Context, RouteRequirements) (RouteEvidence, error) {
		return RouteEvidence{GatewayPoolVersion: 1, AdapterVersion: "0.13.0", ProviderDefinitionID: "definition-1", ProviderDefinitionVersion: 1, GatewayNodeID: "node-1", GatewayNodeVersion: 1, SessionLeaseVersion: 1, SessionConfigurationVersion: 1, AuthorityExpiresAt: time.Now().UTC().Add(10 * time.Minute)}, nil
	})
	svc := &Service{Repository: repo, Protector: testProtector(t), Messages: msgs, Routes: scheduleRoutes}
	r := approvedRecipient(t, svc)
	versions, _ := msgs.ListByCampaign(context.Background(), "campaign-1")
	send, err := svc.Schedule(context.Background(), "campaign-1", versions[0].ID, r.ID, "gateway-1", "pool-1", "OPENWA", "WHATSAPP_WEB_JS", "session-1", "operator", "pre-launch content check", "test-send-idempotency-0006", map[string]string{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimSends(context.Background(), "worker", time.Now(), time.Minute, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	changedRoutes := RouteValidatorFunc(func(context.Context, RouteRequirements) (RouteEvidence, error) {
		return RouteEvidence{GatewayPoolVersion: 1, AdapterVersion: "0.13.0", ProviderDefinitionID: "definition-1", ProviderDefinitionVersion: 2, GatewayNodeID: "node-1", GatewayNodeVersion: 1, SessionLeaseVersion: 1, SessionConfigurationVersion: 1, AuthorityExpiresAt: time.Now().UTC().Add(10 * time.Minute)}, nil
	})
	p := &Processor{Repository: repo, Protector: svc.Protector, Messages: msgs, Routes: changedRoutes, Gateway: acceptingGateway{}}
	if err := p.Process(context.Background(), claimed[0]); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetSend(context.Background(), send.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != SendFailed || got.FailureCode != "TEST_ROUTE_EVIDENCE_CHANGED" {
		t.Fatalf("expected governed route failure, got %+v", got)
	}
}
