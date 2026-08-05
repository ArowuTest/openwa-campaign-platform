package filter

import "testing"

func TestDefaultRegistryIncludesRequiredDimensions(t *testing.T) {
	registry, err := NewRegistry(DefaultDefinitions()...)
	if err != nil {
		t.Fatalf("create registry: %v", err)
	}
	for _, code := range []string{"COUNTRY", "STATE", "LGA", "REPORTED_AGE", "GENDER"} {
		if _, exists := registry.Get(code); !exists {
			t.Fatalf("required filter %s was not registered", code)
		}
	}
}

func TestGroupRejectsUnsupportedOperator(t *testing.T) {
	registry, _ := NewRegistry(DefaultDefinitions()...)
	group := Group{Join: JoinAnd, Rules: []Rule{{DefinitionCode: "COUNTRY", Operator: OperatorContains, Values: []any{"NG"}}}}
	if err := group.Validate(registry); err == nil {
		t.Fatal("expected unsupported operator validation error")
	}
}

func TestGroupRejectsInvalidBetweenValues(t *testing.T) {
	registry, _ := NewRegistry(DefaultDefinitions()...)
	group := Group{Join: JoinAnd, Rules: []Rule{{DefinitionCode: "REPORTED_AGE", Operator: OperatorBetween, Values: []any{35.0, 18.0}}}}
	if err := group.Validate(registry); err == nil {
		t.Fatal("expected invalid range error")
	}
}

func TestRegistryAcceptsFutureFilter(t *testing.T) {
	registry, _ := NewRegistry(DefaultDefinitions()...)
	err := registry.Register(Definition{
		Code: "OCCUPATION", DisplayName: "Occupation", DataType: DataTypeSingleSelect,
		Operators: []Operator{OperatorIn, OperatorNotIn}, Filterable: true, Reportable: true,
		Storage: StorageContactAttribute, AttributeValueColumn: "value_text", IndexStrategy: "btree", DisplayOrder: 80, Active: true,
	})
	if err != nil {
		t.Fatalf("register future filter: %v", err)
	}
	if _, exists := registry.Get("OCCUPATION"); !exists {
		t.Fatal("future filter was not registered")
	}
}
