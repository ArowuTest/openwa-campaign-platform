package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/audience/materialisation"
	"campaign-platform/internal/campaign"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/segment"
)

type materialisationEstimateRepository struct {
	record cohort.EstimateJobRecord
}

func (r materialisationEstimateRepository) Schedule(context.Context, cohort.EstimateJobRequest, time.Time) (cohort.EstimateJobRecord, bool, error) {
	return cohort.EstimateJobRecord{}, false, errors.New("not implemented")
}
func (r materialisationEstimateRepository) Get(_ context.Context, id string) (cohort.EstimateJobRecord, error) {
	if id != r.record.ID {
		return cohort.EstimateJobRecord{}, cohort.ErrEstimateNotFound
	}
	return r.record, nil
}
func (r materialisationEstimateRepository) ListPage(context.Context, string, int, string) (cohort.EstimateJobPage, error) {
	return cohort.EstimateJobPage{}, errors.New("not implemented")
}
func (r materialisationEstimateRepository) StoreResult(context.Context, string, string, int64, cohort.Estimate, cohort.EstimateGovernanceEvidence, time.Time) error {
	return errors.New("not implemented")
}

func TestScheduleAudienceMaterialisationConsumesCompletedDurableEstimateWithoutSynchronousCohortService(t *testing.T) {
	entity, estimate, definition := materialisationEstimateFixture()
	entity.Status = campaign.StatusAudienceBuilding
	entity.Version = 1
	entity.CreatedAt = estimate.AsOf.Add(-time.Hour)
	entity.UpdatedAt = entity.CreatedAt

	campaignRepo := campaign.NewMemoryRepository()
	if err := campaignRepo.Create(context.Background(), entity); err != nil {
		t.Fatal(err)
	}
	segmentRepo := segment.NewMemoryDefinitionRepository()
	saved := segment.Definition{
		ID:             "88888888-8888-4888-8888-888888888888",
		OrganisationID: entity.OrganisationID,
		Name:           "Nigeria audience",
		Definition:     definition,
		Status:         segment.StatusActive,
		Version:        1,
		CreatedBy:      "99999999-9999-4999-8999-999999999999",
		UpdatedBy:      "99999999-9999-4999-8999-999999999999",
		CreatedAt:      entity.CreatedAt,
		UpdatedAt:      entity.UpdatedAt,
	}
	if _, err := segmentRepo.Create(context.Background(), saved, "test seed"); err != nil {
		t.Fatal(err)
	}

	registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	materialisationRepo := materialisation.NewMemoryMaterialisationRepository()
	server := &Server{deps: Dependencies{
		Registry:                 registry,
		Campaigns:                campaign.NewService(campaignRepo),
		SegmentDefinitions:       &segment.DefinitionService{Repository: segmentRepo},
		CohortEstimates:          &cohort.EstimateService{Repository: materialisationEstimateRepository{record: estimate}},
		AudienceMaterialisations: &materialisation.MaterialisationService{Repository: materialisationRepo},
		// Cohorts is deliberately nil: scheduling must never execute a fresh
		// potentially multi-million cohort query in the HTTP request path.
	}}

	body, err := json.Marshal(map[string]any{
		"segmentId":         saved.ID,
		"definitionVersion": saved.Version,
		"estimateId":        estimate.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/"+entity.ID+"/audience-materialisations", bytes.NewReader(body))
	request.SetPathValue("id", entity.ID)
	request = request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{
		User: identity.User{ID: "99999999-9999-4999-8999-999999999999", Permissions: map[string]struct{}{"audience.write": {}}},
	}))
	response := httptest.NewRecorder()

	server.scheduleAudienceMaterialisation(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var job materialisation.MaterialisationJob
	if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.ExpectedCount != estimate.Result.EligibleCount {
		t.Fatalf("expectedCount=%d want=%d", job.ExpectedCount, estimate.Result.EligibleCount)
	}
	if !job.Eligibility.AsOf.Equal(estimate.AsOf) {
		t.Fatalf("asOf=%s want=%s", job.Eligibility.AsOf, estimate.AsOf)
	}
	if job.ConsentPolicyVersion != "consent-review/33333333-3333-4333-8333-333333333333/v4/wording/wording-v7" ||
		job.ConfigurationVersion != "organisation-policy/66666666-6666-4666-8666-666666666666/v6" {
		t.Fatalf("governance evidence=%q %q", job.ConsentPolicyVersion, job.ConfigurationVersion)
	}
}
