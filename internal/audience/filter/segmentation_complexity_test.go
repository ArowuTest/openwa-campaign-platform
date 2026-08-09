package filter

import "testing"

func TestNestedAudienceLogicRespectsDepthAndRuleLimits(t *testing.T) {
	registry, err := NewRegistry(DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	valid := Group{Join: JoinAnd, Rules: []Rule{{DefinitionCode: "COUNTRY", Operator: OperatorIn, Values: []any{"NG"}}}, Children: []Group{{
		Join: JoinOr, Rules: []Rule{{DefinitionCode: "STATE", Operator: OperatorIn, Values: []any{"LAGOS"}}, {DefinitionCode: "PREFERRED_LANGUAGE", Operator: OperatorIn, Values: []any{"EN"}}},
	}}}
	if err := valid.Validate(registry); err != nil {
		t.Fatalf("valid nested AND/OR rejected: %v", err)
	}

	tooDeep := Group{Join: JoinAnd, Rules: []Rule{{DefinitionCode: "COUNTRY", Operator: OperatorIn, Values: []any{"NG"}}}}
	for i := 0; i < MaxGroupDepth; i++ {
		tooDeep = Group{Join: JoinAnd, Children: []Group{tooDeep}}
	}
	if err := tooDeep.Validate(registry); err == nil {
		t.Fatal("filter nesting beyond maximum depth was accepted")
	}

	tooMany := make([]Rule, MaxTotalRules+1)
	for i := range tooMany {
		tooMany[i] = Rule{DefinitionCode: "COUNTRY", Operator: OperatorIn, Values: []any{"NG"}}
	}
	if err := (Group{Join: JoinAnd, Rules: tooMany}).Validate(registry); err == nil {
		t.Fatal("filter definition beyond maximum rule count was accepted")
	}
}
