package materialisation

import (
	"context"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/audience/cohort"
	"campaign-platform/internal/segment"
)

func TestAppendMembersReturnsJobToPendingForNextPage(t *testing.T) {
	now := time.Date(2026, 8, 8, 18, 0, 0, 0, time.UTC)
	repo := NewMemoryMaterialisationRepository()
	service := &MaterialisationService{Repository: repo, Clock: func() time.Time { return now }}
	job, err := service.Schedule(context.Background(), ScheduleMaterialisation{
		CampaignID: "campaign-1", Definition: testDefinition(), DefinitionVersion: 1,
		Eligibility:          cohort.EligibilityContext{OrganisationID: "org-1", PurposeID: "purpose-1", Channel: "WHATSAPP"},
		ConsentPolicyVersion: "consent-v1", ConfigurationVersion: "config-v1", RequestedBy: "user-1", ExpectedCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.Claim(context.Background(), "worker-1", 1, time.Minute, now)
	if err != nil || len(claimed) != 1 || claimed[0].ID != job.ID {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	updated, err := repo.AppendMembers(context.Background(), job.ID, claimed[0].LeaseToken,
		[]segment.Member{{ContactID: "00000000-0000-4000-8000-000000000001", EligibilityEvidenceHash: strings.Repeat("a", 64)}},
		"00000000-0000-4000-8000-000000000001", strings.Repeat("b", 64), 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != MaterialisationPending || updated.LeaseOwner != "" || updated.LeaseExpiresAt != nil {
		t.Fatalf("checkpointed job did not return to pending: %+v", updated)
	}
}
