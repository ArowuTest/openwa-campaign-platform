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
	svc := &Service{Repository: repo, Protector: testProtector(t), Messages: msgs, Routes: RouteValidatorFunc(func(context.Context, string, string, string, string, string) error { return nil })}
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
	svc := &Service{Repository: repo, Protector: testProtector(t), Messages: msgs, Routes: RouteValidatorFunc(func(context.Context, string, string, string, string, string) error { return nil })}
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
	svc := &Service{Repository: repo, Protector: testProtector(t), Messages: msgs, Routes: RouteValidatorFunc(func(context.Context, string, string, string, string, string) error { return nil })}
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
	p := &Processor{Repository: repo, Protector: svc.Protector, Messages: msgs, Gateway: acceptingGateway{}}
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
