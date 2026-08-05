package campaign

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/commercial"
)

type commercialGateStub struct {
	id  string
	err error
}

func (s commercialGateStub) ValidateCampaignApproval(context.Context, string, string, int64) (string, error) {
	return s.id, s.err
}
func TestCommercialTransitionRequiresGovernedApproval(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo).WithCommercialApprovals(commercialGateStub{err: commercial.ErrNotApproved})
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	svc.clock = func() time.Time { return now }
	c, err := New(CreateInput{OrganisationID: "org", Name: "campaign", PurposeID: "purpose", ConsentReviewID: "review", MaximumUniqueRecipients: 10, MaximumMessagesPerRecipient: 1, CreatedBy: "maker", Transport: TransportSelection{Channel: "WHATSAPP", Provider: "OPENWA", Engine: "BAILEYS", RoutingMode: "SENDER_POOL", GatewayPoolID: "pool", SenderPoolID: "sender", AdapterVersion: "v1", RequiredCapabilities: []string{"SEND_TEXT"}, FallbackMode: "NONE", RoutingPolicyVersion: "r1", CapacityEvidenceVersion: "c1"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	c.Status = StatusMessageApproved
	if err = repo.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Transition(context.Background(), c.ID, TransitionInput{Action: ActionApproveCommercial, ActorID: "finance", ExpectedVersion: c.Version})
	if !errors.Is(err, commercial.ErrNotApproved) {
		t.Fatalf("expected commercial rejection, got %v", err)
	}
	svc.commercial = commercialGateStub{id: "approval-1"}
	got, err := svc.Transition(context.Background(), c.ID, TransitionInput{Action: ActionApproveCommercial, ActorID: "finance", ExpectedVersion: c.Version})
	if err != nil {
		t.Fatal(err)
	}
	if got.CommercialApprovalID != "approval-1" {
		t.Fatalf("missing approval evidence: %#v", got)
	}
}
