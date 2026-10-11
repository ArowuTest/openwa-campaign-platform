package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/jobs"
)

func cohortEstimateHTTPFixture(t *testing.T) (*Server, *cohort.MemoryEstimateRepository) {
	t.Helper()
	registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	queue := jobs.NewMemoryRepository()
	repository := cohort.NewMemoryEstimateRepository(queue)
	service := &cohort.EstimateService{
		Repository: repository,
		Compiler:   cohort.NewCompiler(registry),
		Clock: func() time.Time {
			return time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
		},
	}
	return &Server{deps: Dependencies{CohortEstimates: service}}, repository
}

func cohortEstimateRequest(t *testing.T, actorID, requestID string) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"organisationId":  "11111111-1111-4111-8111-111111111111",
		"purposeId":       "22222222-2222-4222-8222-222222222222",
		"channel":         "WHATSAPP",
		"clientRequestId": requestID,
		"definition": audiencefilter.Group{
			Join: audiencefilter.JoinAnd,
			Rules: []audiencefilter.Rule{{
				DefinitionCode: "COUNTRY",
				Operator:       audiencefilter.OperatorIn,
				Values:         []any{"NG"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/cohort-estimates", bytes.NewReader(body))
	principal := identity.Principal{User: identity.User{
		ID:          actorID,
		Status:      identity.StatusActive,
		Permissions: map[string]struct{}{"audience.read": {}},
	}}
	return request.WithContext(identity.WithPrincipal(request.Context(), principal))
}

func TestScheduleCohortEstimateIsDurableIdempotentAndPrincipalBound(t *testing.T) {
	server, _ := cohortEstimateHTTPFixture(t)
	actorID := "33333333-3333-4333-8333-333333333333"

	firstResponse := httptest.NewRecorder()
	server.scheduleCohortEstimate(firstResponse, cohortEstimateRequest(t, actorID, "estimate-http-0001"))
	if firstResponse.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", firstResponse.Code, firstResponse.Body.String())
	}
	var first cohortEstimateResponse
	if err := json.Unmarshal(firstResponse.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || first.RequestedBy != actorID || first.ClientRequestID != "estimate-http-0001" {
		t.Fatalf("unexpected scheduled estimate: %+v", first)
	}
	if first.Status != string(jobs.StatusPending) {
		t.Fatalf("job status=%s want PENDING", first.Status)
	}

	replayResponse := httptest.NewRecorder()
	server.scheduleCohortEstimate(replayResponse, cohortEstimateRequest(t, actorID, "estimate-http-0001"))
	if replayResponse.Code != http.StatusOK {
		t.Fatalf("replay status=%d body=%s", replayResponse.Code, replayResponse.Body.String())
	}
	var replay cohortEstimateResponse
	if err := json.Unmarshal(replayResponse.Body.Bytes(), &replay); err != nil {
		t.Fatal(err)
	}
	if replay.ID != first.ID {
		t.Fatalf("replay id=%s want=%s", replay.ID, first.ID)
	}
}

func TestGetCohortEstimateRequiresMatchingOrganisationContext(t *testing.T) {
	server, _ := cohortEstimateHTTPFixture(t)
	actorID := "33333333-3333-4333-8333-333333333333"
	createResponse := httptest.NewRecorder()
	server.scheduleCohortEstimate(createResponse, cohortEstimateRequest(t, actorID, "estimate-http-0002"))
	if createResponse.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	var created cohortEstimateResponse
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/cohort-estimates/"+created.ID+"?organisationId=11111111-1111-4111-8111-111111111111", nil)
	request.SetPathValue("id", created.ID)
	response := httptest.NewRecorder()
	server.getCohortEstimate(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", response.Code, response.Body.String())
	}

	wrongOrg := httptest.NewRequest(http.MethodGet, "/api/v1/cohort-estimates/"+created.ID+"?organisationId=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", nil)
	wrongOrg.SetPathValue("id", created.ID)
	wrongOrgResponse := httptest.NewRecorder()
	server.getCohortEstimate(wrongOrgResponse, wrongOrg)
	if wrongOrgResponse.Code != http.StatusNotFound {
		t.Fatalf("wrong-org status=%d body=%s", wrongOrgResponse.Code, wrongOrgResponse.Body.String())
	}
}
