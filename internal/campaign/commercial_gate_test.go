package campaign

import (
	"context"
	"errors"
	"strings"
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

func TestPostCommercialTransitionsRevalidateCurrentCommercialAuthority(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 11, 13, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		status Status
		action Action
	}{
		{"request final", StatusCommercialApproved, ActionRequestFinalApproval},
		{"approve final", StatusFinalApprovalPending, ActionApproveFinal},
		{"start dispatch", StatusScheduled, ActionStartDispatch},
		{"resume dispatch", StatusPaused, ActionResume},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := NewMemoryRepository()
			entity := Campaign{
				ID:             "campaign-commercial-current-" + strings.ReplaceAll(tc.name, " ", "-"),
				OrganisationID: "org-1", PurposeID: "purpose-1", ConsentReviewID: "review-1",
				Name: "Commercial current gate", Status: tc.status, Version: 9,
				MaximumUniqueRecipients: 100, MaximumMessagesPerRecipient: 1,
				CommercialApprovalID: "approval-current",
				Transport: TransportSelection{
					Channel: "WHATSAPP", Provider: ProviderOpenWA, Engine: EngineBaileys,
					RoutingMode: RoutingSenderPool, GatewayPoolID: "gateway-1", SenderPoolID: "pool-1",
					AdapterVersion: "1", FallbackMode: FallbackNone,
					RoutingPolicyVersion: "route-v1", CapacityEvidenceVersion: "capacity-v1",
				},
			}
			if err := repo.Create(ctx, entity); err != nil {
				t.Fatal(err)
			}
			service := NewService(repo).WithCommercialApprovals(commercialGateStub{err: commercial.ErrNotApproved})
			service.clock = func() time.Time { return now }
			_, err := service.PrepareTransition(ctx, entity.ID, TransitionInput{
				Action: tc.action, ActorID: "operator", ExpectedVersion: entity.Version,
				Reason: "continue only with current commercial authority",
			})
			if !errors.Is(err, commercial.ErrNotApproved) {
				t.Fatalf("action %s did not fail closed on revoked commercial evidence: %v", tc.action, err)
			}

			service.commercial = commercialGateStub{id: "different-approval"}
			_, err = service.PrepareTransition(ctx, entity.ID, TransitionInput{
				Action: tc.action, ActorID: "operator", ExpectedVersion: entity.Version,
				Reason: "continue only with same frozen commercial authority",
			})
			if err == nil || !strings.Contains(err.Error(), "no longer current") {
				t.Fatalf("action %s accepted a different approval identity: %v", tc.action, err)
			}
		})
	}
}
