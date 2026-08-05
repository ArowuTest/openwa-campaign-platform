package filter

import (
	"context"
	"sync"
	"testing"
)

func TestAdministrationAddsDynamicTypedFilterAndRefreshesRegistry(t *testing.T) {
	registry, _ := NewRegistry(DefaultDefinitions()...)
	store, err := NewMemoryAdministrationStore(DefaultDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	service := NewAdministrationService(store, registry)
	created, err := service.Create(context.Background(), CreateDefinitionInput{Code: "OCCUPATION", DisplayName: "Occupation", DataType: DataTypeSingleSelect, AllowedValues: []string{"Engineer", "Teacher"}, Reportable: true, DisplayOrder: 80, Reason: "new approved segmentation dimension", ActorID: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if created.AttributeValueColumn != "value_text" || created.Core {
		t.Fatalf("unexpected definition: %+v", created)
	}
	if _, ok := registry.Get("OCCUPATION"); !ok {
		t.Fatal("registry was not refreshed")
	}
}
func TestAdministrationPreventsDisablingMandatoryFilter(t *testing.T) {
	registry, _ := NewRegistry(DefaultDefinitions()...)
	store, _ := NewMemoryAdministrationStore(DefaultDefinitions())
	service := NewAdministrationService(store, registry)
	country, _ := store.Get(context.Background(), "COUNTRY")
	_, err := service.Update(context.Background(), "COUNTRY", UpdateDefinitionInput{DisplayName: country.DisplayName, Description: country.Description, Operators: country.Operators, Filterable: false, Reportable: true, Active: false, DisplayOrder: country.DisplayOrder, ExpectedVersion: country.Version, Reason: "attempt disable", ActorID: "admin"})
	if err == nil {
		t.Fatal("expected mandatory-filter protection")
	}
}
func TestConcurrentDefinitionUpdateOnlyOneCommits(t *testing.T) {
	registry, _ := NewRegistry(DefaultDefinitions()...)
	store, _ := NewMemoryAdministrationStore(DefaultDefinitions())
	service := NewAdministrationService(store, registry)
	created, err := service.Create(context.Background(), CreateDefinitionInput{Code: "CUSTOMER_TIER", DisplayName: "Customer tier", DataType: DataTypeSingleSelect, AllowedValues: []string{"Gold", "Silver"}, DisplayOrder: 90, Reason: "approved customer segmentation", ActorID: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			<-start
			_, err := service.Update(context.Background(), created.Code, UpdateDefinitionInput{DisplayName: name, Operators: created.Operators, Filterable: true, Reportable: true, AllowedValues: created.AllowedValues, Active: true, DisplayOrder: created.DisplayOrder, ExpectedVersion: created.Version, Reason: "concurrent approved edit", ActorID: "admin"})
			results <- err
		}(map[bool]string{true: "Tier A", false: "Tier B"}[i == 0])
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if err == ErrDefinitionConflict {
			conflicts++
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
}
func TestGroupLimitsComplexityAndAllowedValues(t *testing.T) {
	registry, _ := NewRegistry(DefaultDefinitions()...)
	_ = registry.Register(Definition{Code: "INTEREST", DisplayName: "Interest", DataType: DataTypeSingleSelect, Operators: []Operator{OperatorIn}, Filterable: true, Storage: StorageContactAttribute, AttributeValueColumn: "value_text", IndexStrategy: "btree", AllowedValues: []string{"Music"}, Active: true})
	group := Group{Join: JoinAnd, Rules: []Rule{{DefinitionCode: "INTEREST", Operator: OperatorIn, Values: []any{"Politics"}}}}
	if err := group.Validate(registry); err == nil {
		t.Fatal("expected allowed-value rejection")
	}
}

func TestGroupRejectsSensitiveFilterWithoutPermission(t *testing.T) {
	registry, err := NewRegistry(DefaultDefinitions()...)
	if err != nil {
		t.Fatal(err)
	}
	group := Group{Join: JoinAnd, Rules: []Rule{{DefinitionCode: "REPORTED_AGE", Operator: OperatorBetween, Values: []any{18, 35}}}}
	if err := group.ValidateForPermissions(registry, func(string) bool { return false }); err == nil {
		t.Fatal("expected sensitive filter permission rejection")
	}
	if err := group.ValidateForPermissions(registry, func(permission string) bool { return permission == "audience.demographics.read" }); err != nil {
		t.Fatalf("authorised filter rejected: %v", err)
	}
}
