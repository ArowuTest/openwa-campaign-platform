package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/audience/materialisation"
	"campaign-platform/internal/campaign"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/segment"
)

const materialisationActorID = "99999999-9999-4999-8999-999999999999"

// A completed estimate is governance/count evidence, not a grant of the
// estimating operator's sensitive-filter permissions to the current operator.
func TestScheduleAudienceMaterialisationRequiresCurrentActorFilterPermissions(t *testing.T) {
	for _, code := range []string{"REPORTED_AGE", "GENDER"} {
		for _, saved := range []bool{false, true} {
			for _, permitted := range []bool{false, true} {
				name := code + "/inline/denied"
				if saved {
					name = code + "/saved/denied"
				}
				if permitted {
					name = strings.TrimSuffix(name, "denied") + "permitted"
				}
				t.Run(name, func(t *testing.T) {
					rule := audiencefilter.Rule{DefinitionCode: code, Operator: audiencefilter.OperatorIn, Values: []any{"FEMALE"}}
					if code == "REPORTED_AGE" {
						rule.Operator = audiencefilter.OperatorBetween
						rule.Values = []any{18, 35}
					}
					definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{rule}}
					server, entity, request := materialisationAuthorizationHTTPFixture(t, definition, saved, permitted)
					response := httptest.NewRecorder()
					server.scheduleAudienceMaterialisation(response, request)
					wantStatus, wantJobs := http.StatusUnprocessableEntity, 0
					if permitted {
						wantStatus, wantJobs = http.StatusAccepted, 1
					}
					assertScheduledMaterialisations(t, server, entity.ID, response, wantStatus, wantJobs)
					if !permitted && !strings.Contains(response.Body.String(), "COHORT_VALIDATION_FAILED") {
						t.Fatalf("missing validation failure: %s", response.Body.String())
					}
					if permitted {
						var job materialisation.MaterialisationJob
						if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
							t.Fatal(err)
						}
						if job.RequestedBy != materialisationActorID || job.ExpectedCount != 42 {
							t.Fatalf("wrong current actor/count: %+v", job)
						}
					}
				})
			}
		}
	}
}

func TestScheduleAudienceMaterialisationFailsClosedWithoutActorOrRegistry(t *testing.T) {
	for _, missing := range []string{"actor", "registry"} {
		t.Run(missing, func(t *testing.T) {
			_, _, definition := materialisationEstimateFixture()
			server, entity, request := materialisationAuthorizationHTTPFixture(t, definition, false, false)
			wantStatus := http.StatusUnprocessableEntity
			if missing == "actor" {
				request = request.WithContext(context.Background())
				wantStatus = http.StatusUnauthorized
			} else {
				server.deps.Registry = nil
			}
			response := httptest.NewRecorder()
			server.scheduleAudienceMaterialisation(response, request)
			assertScheduledMaterialisations(t, server, entity.ID, response, wantStatus, 0)
		})
	}
}

func TestScheduleAudienceMaterialisationRefreshesFilterGovernanceBeforeEstimateConsumption(t *testing.T) {
	for _, change := range []string{"stricter-permission", "inactive", "refresh-failure"} {
		for _, saved := range []bool{false, true} {
			name := change + "/inline"
			if saved {
				name = change + "/saved"
			}
			t.Run(name, func(t *testing.T) {
				definition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{
					DefinitionCode: "PREFERRED_LANGUAGE", Operator: audiencefilter.OperatorIn, Values: []any{"en"},
				}}}
				server, entity, request := materialisationAuthorizationHTTPFixture(t, definition, saved, false)
				catalogue := changedFilterCatalogue(t, server.deps.Registry, change)
				server.deps.FilterDefinitions = catalogue
				response := httptest.NewRecorder()
				server.scheduleAudienceMaterialisation(response, request)
				wantStatus := http.StatusUnprocessableEntity
				if change == "refresh-failure" {
					wantStatus = http.StatusInternalServerError
				}
				assertScheduledMaterialisations(t, server, entity.ID, response, wantStatus, 0)
			})
		}
	}
}

func TestScheduleCohortEstimateRefreshesFilterGovernanceBeforeScheduling(t *testing.T) {
	for _, change := range []string{"stricter-permission", "inactive", "refresh-failure"} {
		t.Run(change, func(t *testing.T) {
			server, repository := cohortEstimateHTTPFixture(t)
			server.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
			// The catalogue and compiler share the same warm registry, as in
			// control-api runtime; only the persisted catalogue is changed.
			registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
			if err != nil {
				t.Fatal(err)
			}
			server.deps.Registry = registry
			server.deps.CohortEstimates.Compiler = cohort.NewCompiler(registry)
			server.deps.FilterDefinitions = changedFilterCatalogue(t, registry, change)
			body, err := json.Marshal(scheduleCohortEstimateRequest{
				OrganisationID: "11111111-1111-4111-8111-111111111111",
				PurposeID:      "22222222-2222-4222-8222-222222222222",
				Channel:        "WHATSAPP", ClientRequestID: "catalogue-refresh-0001",
				Definition: audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{
					DefinitionCode: "PREFERRED_LANGUAGE", Operator: audiencefilter.OperatorIn, Values: []any{"en"},
				}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/cohort-estimates", bytes.NewReader(body))
			request = request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{User: identity.User{
				ID: materialisationActorID, Permissions: map[string]struct{}{"audience.read": {}},
			}}))
			response := httptest.NewRecorder()
			server.scheduleCohortEstimate(response, request)
			wantStatus := http.StatusUnprocessableEntity
			if change == "refresh-failure" {
				wantStatus = http.StatusInternalServerError
			}
			if response.Code != wantStatus {
				t.Errorf("status=%d want=%d body=%s", response.Code, wantStatus, response.Body.String())
			}
			page, err := repository.ListPage(context.Background(), "11111111-1111-4111-8111-111111111111", 50, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 0 {
				t.Fatalf("rejected definition created %d estimate jobs", len(page.Items))
			}
		})
	}
}

func materialisationAuthorizationHTTPFixture(t *testing.T, definition audiencefilter.Group, saved, permitted bool) (*Server, campaign.Campaign, *http.Request) {
	t.Helper()
	entity, estimate, _ := materialisationEstimateFixture()
	entity.Status, entity.Version = campaign.StatusAudienceBuilding, 1
	entity.CreatedAt = estimate.AsOf.Add(-time.Hour)
	entity.UpdatedAt = entity.CreatedAt
	estimate.Definition = definition
	estimate.RequestedBy = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	campaignRepo := campaign.NewMemoryRepository()
	if err := campaignRepo.Create(context.Background(), entity); err != nil {
		t.Fatal(err)
	}
	registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), deps: Dependencies{
		Registry: registry, Campaigns: campaign.NewService(campaignRepo),
		CohortEstimates:          &cohort.EstimateService{Repository: materialisationEstimateRepository{record: estimate}},
		AudienceMaterialisations: &materialisation.MaterialisationService{Repository: materialisation.NewMemoryMaterialisationRepository()},
		// Cohorts deliberately remains nil: permission checks must not
		// restore synchronous cohort computation on the scheduling path.
	}}
	input := scheduleAudienceMaterialisationRequest{Definition: definition, DefinitionVersion: 1, EstimateID: estimate.ID}
	if saved {
		segmentRepo := segment.NewMemoryDefinitionRepository()
		record := segment.Definition{
			ID: "88888888-8888-4888-8888-888888888888", OrganisationID: entity.OrganisationID,
			Name: "restricted audience", Definition: definition, Status: segment.StatusActive, Version: 1,
			CreatedBy: estimate.RequestedBy, UpdatedBy: estimate.RequestedBy, CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
		}
		if _, err := segmentRepo.Create(context.Background(), record, "test seed"); err != nil {
			t.Fatal(err)
		}
		server.deps.SegmentDefinitions = &segment.DefinitionService{Repository: segmentRepo}
		input.SegmentID = record.ID
		// A benign inline payload must not bypass the resolved saved filter.
		_, _, input.Definition = materialisationEstimateFixture()
	}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/"+entity.ID+"/audience-materialisations", bytes.NewReader(body))
	request.SetPathValue("id", entity.ID)
	permissions := map[string]struct{}{"audience.write": {}}
	if permitted {
		permissions["audience.demographics.read"] = struct{}{}
	}
	request = request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{User: identity.User{
		ID: materialisationActorID, Permissions: permissions,
	}}))
	return server, entity, request
}

func assertScheduledMaterialisations(t *testing.T, server *Server, campaignID string, response *httptest.ResponseRecorder, wantStatus, wantJobs int) {
	t.Helper()
	if response.Code != wantStatus {
		t.Errorf("status=%d want=%d body=%s", response.Code, wantStatus, response.Body.String())
	}
	page, err := server.deps.AudienceMaterialisations.ListPage(context.Background(), campaignID, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != wantJobs {
		t.Fatalf("scheduled %d materialisation jobs want=%d", len(page.Items), wantJobs)
	}
}

type unavailableFilterGenerationStore struct {
	*audiencefilter.MemoryAdministrationStore
}

func (unavailableFilterGenerationStore) Generation(context.Context) (int64, error) {
	return 0, errors.New("filter catalogue temporarily unavailable")
}

func changedFilterCatalogue(t *testing.T, registry *audiencefilter.Registry, change string) *audiencefilter.AdministrationService {
	t.Helper()
	store, err := audiencefilter.NewMemoryAdministrationStore(audiencefilter.DefaultDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	service := audiencefilter.NewAdministrationService(store, registry)
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if change == "refresh-failure" {
		return audiencefilter.NewAdministrationService(unavailableFilterGenerationStore{store}, registry)
	}
	definition, err := store.Get(context.Background(), "PREFERRED_LANGUAGE")
	if err != nil {
		t.Fatal(err)
	}
	expectedVersion := definition.Version
	definition.Version++
	if change == "stricter-permission" {
		definition.RequiresPermission = "audience.demographics.read"
	} else {
		definition.Active = false
	}
	if _, err := store.CompareAndSwap(context.Background(), definition, expectedVersion, "another-admin", "restrict catalogue"); err != nil {
		t.Fatal(err)
	}
	return service
}
