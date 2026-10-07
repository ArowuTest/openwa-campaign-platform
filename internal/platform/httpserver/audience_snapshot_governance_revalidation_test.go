package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/campaign"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/organisation"
	"campaign-platform/internal/segment"
)

// Governance read before selection cannot authorise a snapshot when the
// authoritative records changed while the member query was running.
func TestSynchronousAudienceSnapshotRevalidatesGovernanceAfterMemberSelection(t *testing.T) {
	for _, route := range []string{"create", "materialise"} {
		for _, change := range []string{"unchanged", "policy-version", "future-policy-rotation", "consent-wording"} {
			t.Run(route+"/"+change, func(t *testing.T) {
				now := time.Now().UTC()
				server, entity := snapshotEvidenceFixture(t, now)
				entity.Status = campaign.StatusAudienceBuilding
				entity.Version = 1
				entity.PurposeID = "55555555-5555-4555-8555-555555555555"
				entity.MaximumUniqueRecipients = 10
				entity.CreatedAt = now.Add(-time.Hour)
				entity.UpdatedAt = entity.CreatedAt
				campaignRepo := campaign.NewMemoryRepository()
				if err := campaignRepo.Create(context.Background(), entity); err != nil {
					t.Fatal(err)
				}
				server.deps.Campaigns = campaign.NewService(campaignRepo)
				registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
				if err != nil {
					t.Fatal(err)
				}
				server.deps.Registry = registry
				review, err := server.deps.ConsentReviews.Get(context.Background(), entity.ConsentReviewID)
				if err != nil {
					t.Fatal(err)
				}
				reviewRepo := consent.NewMemoryRepository()
				if err := reviewRepo.Create(context.Background(), review); err != nil {
					t.Fatal(err)
				}
				server.deps.ConsentReviews = consent.NewService(reviewRepo)
				policyStore := server.deps.OrganisationPolicies.Store
				queryRepo := &governanceChangingMemberRepository{
					MemoryQueryRepository: &cohort.MemoryQueryRepository{EligibleContactIDs: []string{"77777777-7777-4777-8777-777777777777"}},
					duringMembers: func() {
						switch change {
						case "policy-version":
							policy, err := policyStore.Get(context.Background(), "33333333-3333-4333-8333-333333333333")
							if err != nil {
								t.Fatal(err)
							}
							expectedVersion := policy.Version
							policy.Version++
							if _, err := policyStore.CompareAndSwap(context.Background(), policy, expectedVersion); err != nil {
								t.Fatal(err)
							}
						case "future-policy-rotation":
							// Activation retires P. Q is effective after the query
							// AsOf, so the post-query resolver must not fall back
							// to P or silently accept an uncapped member result.
							next := organisation.Policy{
								ID: "66666666-6666-4666-8666-666666666666", OrganisationID: entity.OrganisationID,
								Status: organisation.PolicyDraft, Version: 1, EffectiveFrom: now.Add(time.Hour),
								ContactRetentionDays: 365, CampaignRetentionDays: 365,
								FrequencyCaps: []organisation.FrequencyCap{{Channel: "WHATSAPP", MaxMessages: 1, WindowHours: 24}},
								CreatedAt:     now, UpdatedAt: now,
							}
							if _, err := policyStore.Create(context.Background(), next); err != nil {
								t.Fatal(err)
							}
							next.Status, next.Version = organisation.PolicyActive, 2
							if _, err := policyStore.CompareAndSwap(context.Background(), next, 1); err != nil {
								t.Fatal(err)
							}
						case "consent-wording":
							updated := review
							updated.Version++
							updated.WordingVersion = "wording-v8"
							if err := reviewRepo.CompareAndSwap(context.Background(), updated, review.Version); err != nil {
								t.Fatal(err)
							}
						}
					},
				}
				server.deps.Cohorts = cohort.NewExecutionService(cohort.NewCompiler(registry), queryRepo)
				snapshotStore := &persistedSnapshotTrackingStore{MemoryRepository: segment.NewMemoryRepository()}
				server.deps.Snapshots = segment.NewService(snapshotStore)
				definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{
					DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"NG"},
				}}}
				var input any = segment.CreateInput{Definition: definition, DefinitionVersion: 1}
				if route == "materialise" {
					input = materialiseAudienceSnapshotRequest{Definition: definition, DefinitionVersion: 1, AsOf: &now, Limit: 10}
				}
				body, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				path := "/api/v1/campaigns/" + entity.ID + "/audience-snapshots"
				if route == "materialise" {
					path += "/materialise"
				}
				request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
				request.SetPathValue("id", entity.ID)
				request = request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{User: identity.User{
					ID: "snapshot-operator", Permissions: map[string]struct{}{"audience.write": {}},
				}}))
				response := httptest.NewRecorder()
				if route == "create" {
					server.createAudienceSnapshot(response, request)
				} else {
					server.materialiseAudienceSnapshot(response, request)
				}
				if queryRepo.memberSelections != 1 {
					t.Fatalf("member query ran %d times want 1", queryRepo.memberSelections)
				}
				if change != "unchanged" {
					if response.Code != http.StatusUnprocessableEntity {
						t.Errorf("status=%d want=422 body=%s", response.Code, response.Body.String())
					}
					if len(snapshotStore.persistedIDs) != 0 {
						t.Fatalf("changed governance persisted %d snapshots", len(snapshotStore.persistedIDs))
					}
					if !strings.Contains(strings.ToLower(response.Body.String()), "re-estimate") {
						t.Fatalf("response does not tell operator to re-estimate: %s", response.Body.String())
					}
					return
				}
				if response.Code != http.StatusCreated {
					t.Fatalf("unchanged governance status=%d body=%s", response.Code, response.Body.String())
				}
				if len(snapshotStore.persistedIDs) != 1 {
					t.Fatalf("unchanged governance persisted %d snapshots want 1", len(snapshotStore.persistedIDs))
				}
				snapshot, err := snapshotStore.Get(context.Background(), snapshotStore.persistedIDs[0])
				if err != nil {
					t.Fatal(err)
				}
				if snapshot.EligibleCount != 1 ||
					snapshot.ConsentPolicyVersion != "consent-review/22222222-2222-4222-8222-222222222222/v4/wording/wording-v7" ||
					snapshot.ConfigurationVersion != "organisation-policy/33333333-3333-4333-8333-333333333333/v6" {
					t.Fatalf("wrong persisted member/evidence: %+v", snapshot)
				}
			})
		}
	}
}

type governanceChangingMemberRepository struct {
	*cohort.MemoryQueryRepository
	duringMembers    func()
	memberSelections int
}

func (r *governanceChangingMemberRepository) Members(ctx context.Context, compiled cohort.CompiledQuery, limit int) ([]string, error) {
	r.memberSelections++
	r.duringMembers()
	return r.MemoryQueryRepository.Members(ctx, compiled, limit)
}

type persistedSnapshotTrackingStore struct {
	*segment.MemoryRepository
	persistedIDs []string
}

func (s *persistedSnapshotTrackingStore) EnsureWithMembers(ctx context.Context, snapshot segment.Snapshot, members []segment.Member) (segment.Snapshot, bool, error) {
	stored, created, err := s.MemoryRepository.EnsureWithMembers(ctx, snapshot, members)
	if err == nil && created {
		s.persistedIDs = append(s.persistedIDs, stored.ID)
	}
	return stored, created, err
}
