package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	audiencefilter "campaign-platform/internal/audience/filter"
)

func TestScheduleAudienceMaterialisationAcceptsSavedNoValueFilterWithRebuiltEmptyValues(t *testing.T) {
	savedDefinition := audiencefilter.Group{Join: audiencefilter.JoinAnd, Children: []audiencefilter.Group{{
		Join: audiencefilter.JoinOr, Rules: []audiencefilter.Rule{{
			DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIsKnown,
		}},
	}}}
	server, entity, request := materialisationAuthorizationHTTPFixture(t, savedDefinition, true, false)
	var rebuiltDefinition audiencefilter.Group
	if err := json.Unmarshal([]byte(`{"join":"AND","rules":[],"children":[{"join":"OR","rules":[{"definitionCode":"COUNTRY","operator":"is_known","values":[]}],"children":[]}]}`), &rebuiltDefinition); err != nil {
		t.Fatal(err)
	}
	repository := server.deps.CohortEstimates.Repository.(materialisationEstimateRepository)
	repository.record.Definition = rebuiltDefinition
	server.deps.CohortEstimates.Repository = repository
	response := httptest.NewRecorder()
	server.scheduleAudienceMaterialisation(response, request)
	assertScheduledMaterialisations(t, server, entity.ID, response, http.StatusAccepted, 1)
	saved, err := server.deps.SegmentDefinitions.Get(context.Background(), "88888888-8888-4888-8888-888888888888")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Definition.Children[0].Rules[0].Values != nil {
		t.Fatal("scheduling mutated saved no-value filter")
	}
}

func TestSameFilterGroupNormalizesOnlyEmptyWireArraysWithoutMutatingInputs(t *testing.T) {
	tests := []struct {
		name        string
		left, right string
		want        bool
	}{
		{
			name:  "nil and empty values",
			left:  `{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"is_known","values":null}]}`,
			right: `{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"is_known","values":[]}]}`,
			want:  true,
		},
		{
			name:  "nested nil and empty arrays",
			left:  `{"join":"AND","children":[{"join":"OR","rules":[{"definitionCode":"COUNTRY","operator":"is_unknown","values":null}]}]}`,
			right: `{"join":"AND","rules":[],"children":[{"join":"OR","rules":[{"definitionCode":"COUNTRY","operator":"is_unknown","values":[]}],"children":[]}]}`,
			want:  true,
		},
		{
			name:  "nil and empty groups",
			left:  `{"join":"AND","rules":null,"children":null}`,
			right: `{"join":"AND","rules":[],"children":[]}`,
			want:  true,
		},
		{
			name:  "null value is not empty values",
			left:  `{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"is_known","values":[null]}]}`,
			right: `{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"is_known","values":[]}]}`,
			want:  false,
		},
		{
			name:  "primitive type is preserved",
			left:  `{"join":"AND","rules":[{"definitionCode":"REPORTED_AGE","operator":"equals","values":[18]}]}`,
			right: `{"join":"AND","rules":[{"definitionCode":"REPORTED_AGE","operator":"equals","values":["18"]}]}`,
			want:  false,
		},
		{
			name:  "boolean is not a number",
			left:  `{"join":"AND","rules":[{"definitionCode":"X","operator":"equals","values":[false]}]}`,
			right: `{"join":"AND","rules":[{"definitionCode":"X","operator":"equals","values":[0]}]}`,
			want:  false,
		},
		{
			name:  "actual values differ",
			left:  `{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"in","values":["NG"]}]}`,
			right: `{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"in","values":["GH"]}]}`,
			want:  false,
		},
		{
			name:  "value order is preserved",
			left:  `{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"in","values":["NG","GH"]}]}`,
			right: `{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"in","values":["GH","NG"]}]}`,
			want:  false,
		},
		{
			name:  "rule order is preserved",
			left:  `{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"is_known","values":[]},{"definitionCode":"STATE","operator":"is_known","values":[]}]}`,
			right: `{"join":"AND","rules":[{"definitionCode":"STATE","operator":"is_known","values":[]},{"definitionCode":"COUNTRY","operator":"is_known","values":[]}]}`,
			want:  false,
		},
		{
			name:  "child order is preserved",
			left:  `{"join":"AND","children":[{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"is_known","values":[]}]},{"join":"OR","rules":[{"definitionCode":"STATE","operator":"is_known","values":[]}]}]}`,
			right: `{"join":"AND","children":[{"join":"OR","rules":[{"definitionCode":"STATE","operator":"is_known","values":[]}]},{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"is_known","values":[]}]}]}`,
			want:  false,
		},
		{
			name:  "join is preserved",
			left:  `{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"is_known","values":[]}]}`,
			right: `{"join":"OR","rules":[{"definitionCode":"COUNTRY","operator":"is_known","values":[]}]}`,
			want:  false,
		},
		{
			name:  "operator is preserved",
			left:  `{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"is_known","values":[]}]}`,
			right: `{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"is_unknown","values":[]}]}`,
			want:  false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var left, right, originalLeft, originalRight audiencefilter.Group
			for _, input := range []struct {
				wire   string
				target *audiencefilter.Group
			}{{test.left, &left}, {test.right, &right}, {test.left, &originalLeft}, {test.right, &originalRight}} {
				if err := json.Unmarshal([]byte(input.wire), input.target); err != nil {
					t.Fatal(err)
				}
			}
			if got := sameFilterGroup(left, right); got != test.want {
				t.Errorf("sameFilterGroup=%t want=%t", got, test.want)
			}
			if got := sameFilterGroup(right, left); got != test.want {
				t.Errorf("reverse sameFilterGroup=%t want=%t", got, test.want)
			}
			if !reflect.DeepEqual(left, originalLeft) || !reflect.DeepEqual(right, originalRight) {
				t.Fatal("filter comparison mutated its inputs")
			}
		})
	}
}
