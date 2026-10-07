package cohort

import (
	"strings"
	"testing"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
)

func TestCompileEligibilityAndSegment(t *testing.T) {
	registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	compiler := NewCompiler(registry)
	query, err := compiler.Compile(audiencefilter.Group{
		Join: audiencefilter.JoinAnd,
		Rules: []audiencefilter.Rule{
			{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"NG"}},
			{DefinitionCode: "STATE", Operator: audiencefilter.OperatorIn, Values: []any{"LAGOS"}},
			{DefinitionCode: "REPORTED_AGE", Operator: audiencefilter.OperatorBetween, Values: []any{18.0, 35.0}},
		},
	}, EligibilityContext{
		OrganisationID: "org-1", PurposeID: "purpose-1", Channel: "WHATSAPP",
		AsOf: time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, expected := range []string{"EXISTS", "NOT EXISTS", "geo.iso2", "geo.code", "c.reported_age BETWEEN", "newer.granted_at", "o.status = 'ACTIVE'"} {
		if !strings.Contains(query.SQL, expected) {
			t.Fatalf("query missing %q: %s", expected, query.SQL)
		}
	}
	if len(query.Args) != 8 {
		t.Fatalf("unexpected argument count %d", len(query.Args))
	}
}

func TestCompilerDoesNotUseUserFieldNames(t *testing.T) {
	registry, _ := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	compiler := NewCompiler(registry)
	_, err := compiler.Compile(audiencefilter.Group{
		Join:  audiencefilter.JoinAnd,
		Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY; DROP TABLE contacts", Operator: audiencefilter.OperatorIn, Values: []any{"x"}}},
	}, EligibilityContext{OrganisationID: "org", PurposeID: "purpose"})
	if err == nil {
		t.Fatal("expected unknown filter definition rejection")
	}
}

func TestCompileDynamicAttributeWithoutFieldInjection(t *testing.T) {
	registry, _ := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	err := registry.Register(audiencefilter.Definition{
		Code: "MUSIC_INTEREST", DisplayName: "Music interest",
		DataType:             audiencefilter.DataTypeSingleSelect,
		Operators:            []audiencefilter.Operator{audiencefilter.OperatorIn},
		Storage:              audiencefilter.StorageContactAttribute,
		AttributeValueColumn: "value_text",
		Filterable:           true, Reportable: true, Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	query, err := NewCompiler(registry).Compile(audiencefilter.Group{
		Join:  audiencefilter.JoinAnd,
		Rules: []audiencefilter.Rule{{DefinitionCode: "MUSIC_INTEREST", Operator: audiencefilter.OperatorIn, Values: []any{"AFROBEATS"}}},
	}, EligibilityContext{OrganisationID: "org", PurposeID: "purpose"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query.SQL, "contact_attribute_values") || strings.Contains(query.SQL, "AFROBEATS") {
		t.Fatalf("dynamic attribute was not safely parameterised: %s", query.SQL)
	}
}

func TestGeographyFiltersUseStableReferenceCodesInsteadOfComparingCodesToUUIDs(t *testing.T) {
	registry, _ := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	query, err := NewCompiler(registry).Compile(audiencefilter.Group{
		Join: audiencefilter.JoinAnd,
		Rules: []audiencefilter.Rule{
			{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"NG"}},
			{DefinitionCode: "STATE", Operator: audiencefilter.OperatorIn, Values: []any{"LAGOS"}},
			{DefinitionCode: "LGA", Operator: audiencefilter.OperatorIn, Values: []any{"IKEJA"}},
		},
	}, EligibilityContext{OrganisationID: "org", PurposeID: "purpose"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(query.SQL, "c.country_id IN") || strings.Contains(query.SQL, "c.state_id IN") || strings.Contains(query.SQL, "c.lga_id IN") {
		t.Fatalf("geography code was compared directly to UUID columns: %s", query.SQL)
	}
	for _, fragment := range []string{"countries geo", "administrative_areas geo", "upper(geo.iso2)", "upper(geo.code)"} {
		if !strings.Contains(query.SQL, fragment) {
			t.Fatalf("missing geography reference lookup %q: %s", fragment, query.SQL)
		}
	}
}

func TestCompileForPermissionsRejectsHiddenSensitiveFilter(t *testing.T) {
	registry, _ := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	_, err := NewCompiler(registry).CompileForPermissions(audiencefilter.Group{
		Join:  audiencefilter.JoinAnd,
		Rules: []audiencefilter.Rule{{DefinitionCode: "REPORTED_AGE", Operator: audiencefilter.OperatorBetween, Values: []any{18, 35}}},
	}, EligibilityContext{OrganisationID: "org", PurposeID: "purpose"}, func(string) bool { return false })
	if err == nil {
		t.Fatal("expected sensitive filter permission rejection")
	}
}

func TestDynamicNegativeFilterUsesNotExistsAndContainsEscapesWildcards(t *testing.T) {
	registry, _ := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err := registry.Register(audiencefilter.Definition{
		Code: "INTEREST_TEXT", DisplayName: "Interest text", DataType: audiencefilter.DataTypeText,
		Operators: []audiencefilter.Operator{audiencefilter.OperatorNotEquals, audiencefilter.OperatorContains},
		Storage:   audiencefilter.StorageContactAttribute, AttributeValueColumn: "value_text",
		Filterable: true, Reportable: true, Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	compiler := NewCompiler(registry)
	negative, err := compiler.Compile(audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "INTEREST_TEXT", Operator: audiencefilter.OperatorNotEquals, Values: []any{"sports"}}}}, EligibilityContext{OrganisationID: "org", PurposeID: "purpose"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(negative.SQL, "NOT EXISTS") || strings.Contains(negative.SQL, "value_text <>") {
		t.Fatalf("negative dynamic filter has unsafe multi-value semantics: %s", negative.SQL)
	}
	contains, err := compiler.Compile(audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "INTEREST_TEXT", Operator: audiencefilter.OperatorContains, Values: []any{"50%_off"}}}}, EligibilityContext{OrganisationID: "org", PurposeID: "purpose"})
	if err != nil {
		t.Fatal(err)
	}
	last := contains.Args[len(contains.Args)-1]
	if last != `%50\%\_off%` {
		t.Fatalf("LIKE literal was not escaped: %#v", last)
	}
}

func TestCompileBreakdownUsesSharedGovernedEligibilityStages(t *testing.T) {
	registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	query, err := NewCompiler(registry).CompileBreakdownForPermissions(audiencefilter.Group{
		Join: audiencefilter.JoinOr,
		Rules: []audiencefilter.Rule{
			{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"NG"}},
		},
		Children: []audiencefilter.Group{{
			Join: audiencefilter.JoinAnd,
			Rules: []audiencefilter.Rule{
				{DefinitionCode: "STATE", Operator: audiencefilter.OperatorIn, Values: []any{"LAGOS"}},
				{DefinitionCode: "REPORTED_AGE", Operator: audiencefilter.OperatorBetween, Values: []any{18, 35}},
			},
		}},
	}, EligibilityContext{
		OrganisationID: "org-1", PurposeID: "purpose-1", Channel: "WHATSAPP",
		AsOf: time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"WITH matched_profiles AS",
		"consent_eligible AS",
		"unsuppressed AS",
		"final_eligible AS",
		"consent_grants",
		"suppressions",
		"organisation_policy_versions",
		"frequency_caps",
		" OR ",
		" AND ",
	} {
		if !strings.Contains(query.SQL, fragment) {
			t.Fatalf("breakdown SQL missing %q: %s", fragment, query.SQL)
		}
	}
	if len(query.Args) < 7 {
		t.Fatalf("breakdown query did not preserve governed parameters: %#v", query.Args)
	}
}

func TestCompileAppliesGovernedFrequencyCaps(t *testing.T) {
	registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	query, err := NewCompiler(registry).Compile(audiencefilter.Group{
		Join:  audiencefilter.JoinAnd,
		Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"NG"}}},
	}, EligibilityContext{OrganisationID: "org-1", PurposeID: "purpose-1", Channel: "WHATSAPP", AsOf: time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"organisation_policy_versions", "frequency_caps", "campaign_recipients", "make_interval"} {
		if !strings.Contains(query.SQL, expected) {
			t.Fatalf("compiled SQL missing %s", expected)
		}
	}
}

func TestCompileFrequencyCapWindowStopsAtGovernedAsOf(t *testing.T) {
	registry, err := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	compiler := NewCompiler(registry)
	now := time.Date(2099, 2, 1, 12, 0, 0, 0, time.UTC)
	group := audiencefilter.Group{
		Join:  audiencefilter.JoinAnd,
		Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"NG"}}},
	}
	eligibility := EligibilityContext{OrganisationID: "org-1", PurposeID: "purpose-1", Channel: "WHATSAPP", AsOf: now}
	for _, test := range []struct {
		name    string
		compile func() (CompiledQuery, error)
	}{
		{"members", func() (CompiledQuery, error) { return compiler.Compile(group, eligibility) }},
		{"breakdown", func() (CompiledQuery, error) { return compiler.CompileBreakdownForPermissions(group, eligibility, nil) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			query, err := test.compile()
			if err != nil {
				t.Fatal(err)
			}
			for _, bound := range []string{"recent.authorised_at > $4 - make_interval", "recent.authorised_at <= $4"} {
				if !strings.Contains(query.SQL, bound) {
					t.Fatalf("frequency-cap query is missing governed window bound %q: %s", bound, query.SQL)
				}
			}
			if len(query.Args) < 4 || query.Args[3] != now {
				t.Fatalf("frequency-cap window did not bind the requested as-of time: %+v", query.Args)
			}
		})
	}
}
