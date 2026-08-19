package campaign

import (
	"context"
	"errors"
	"testing"
	"time"
)

type rejectingClonePurposePolicy struct{ err error }

func (p rejectingClonePurposePolicy) ValidatePurpose(context.Context, string, string) error {
	return p.err
}

func TestCloneRevalidatesCurrentPurposePolicy(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	now := time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC)
	source, err := New(CreateInput{OrganisationID: "org", Name: "source", PurposeID: "purpose", ConsentReviewID: "review", MaximumUniqueRecipients: 10, MaximumMessagesPerRecipient: 1, CreatedBy: "maker", Transport: TransportSelection{Channel: "WHATSAPP", Provider: ProviderOpenWA, Engine: EngineBaileys, GatewayPoolID: "gateway", AdapterVersion: "1.0.0", RoutingMode: RoutingSenderPool, SenderPoolID: "pool", FallbackMode: FallbackNone, RoutingPolicyVersion: "route-v1", CapacityEvidenceVersion: "capacity-v1"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, source); err != nil {
		t.Fatal(err)
	}
	policyErr := errors.New("purpose is no longer permitted")
	service := NewService(repo).WithOrganisationPolicies(rejectingClonePurposePolicy{err: policyErr})
	if _, err := service.Clone(ctx, source.ID, CloneInput{Name: "clone", ActorID: "operator"}); !errors.Is(err, policyErr) {
		t.Fatalf("clone bypassed current purpose policy: %v", err)
	}
}
