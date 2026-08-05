package filter

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Join string

const (
	JoinAnd Join = "AND"
	JoinOr  Join = "OR"
)

const (
	MaxGroupDepth    = 5
	MaxTotalRules    = 50
	MaxValuesPerRule = 500
)

type Rule struct {
	DefinitionCode string   `json:"definitionCode"`
	Operator       Operator `json:"operator"`
	Values         []any    `json:"values"`
}
type Group struct {
	Join     Join    `json:"join"`
	Rules    []Rule  `json:"rules,omitempty"`
	Children []Group `json:"children,omitempty"`
}

func (g Group) Validate(registry *Registry) error {
	return g.ValidateForPermissions(registry, nil)
}

// ValidateForPermissions applies the same structural and type validation as
// Validate and additionally rejects filters the caller is not authorised to
// use. A nil permission function is treated as a trusted internal caller.
func (g Group) ValidateForPermissions(registry *Registry, hasPermission func(string) bool) error {
	if registry == nil {
		return errors.New("filter registry is required")
	}
	rules := 0
	return g.validate(registry, hasPermission, 1, &rules)
}

func (g Group) validate(registry *Registry, hasPermission func(string) bool, depth int, total *int) error {
	if depth > MaxGroupDepth {
		return fmt.Errorf("filter nesting exceeds maximum depth %d", MaxGroupDepth)
	}
	if g.Join != JoinAnd && g.Join != JoinOr {
		return errors.New("filter group join must be AND or OR")
	}
	if len(g.Rules) == 0 && len(g.Children) == 0 {
		return errors.New("filter group must contain a rule or child group")
	}
	*total += len(g.Rules)
	if *total > MaxTotalRules {
		return fmt.Errorf("filter definition exceeds maximum of %d rules", MaxTotalRules)
	}
	for _, rule := range g.Rules {
		definition, exists := registry.Get(strings.ToUpper(strings.TrimSpace(rule.DefinitionCode)))
		if !exists || !definition.Active || !definition.Filterable {
			return fmt.Errorf("filter definition %s is unavailable", rule.DefinitionCode)
		}
		if definition.RequiresPermission != "" && hasPermission != nil && !hasPermission(definition.RequiresPermission) {
			return fmt.Errorf("filter definition %s is not permitted", rule.DefinitionCode)
		}
		if !containsOperator(definition.Operators, rule.Operator) {
			return fmt.Errorf("operator %s is not permitted for %s", rule.Operator, rule.DefinitionCode)
		}
		if err := validateRuleValues(definition, rule); err != nil {
			return fmt.Errorf("%s: %w", rule.DefinitionCode, err)
		}
	}
	for _, child := range g.Children {
		if err := child.validate(registry, hasPermission, depth+1, total); err != nil {
			return err
		}
	}
	return nil
}

func validateRuleValues(definition Definition, rule Rule) error {
	if len(rule.Values) > MaxValuesPerRule {
		return fmt.Errorf("operator accepts no more than %d values", MaxValuesPerRule)
	}
	switch rule.Operator {
	case OperatorIsKnown, OperatorIsUnknown:
		if len(rule.Values) != 0 {
			return errors.New("operator does not accept values")
		}
		return nil
	case OperatorBetween:
		if len(rule.Values) != 2 {
			return errors.New("between requires exactly two values")
		}
	case OperatorIn, OperatorNotIn:
		if len(rule.Values) == 0 {
			return errors.New("operator requires at least one value")
		}
	default:
		if len(rule.Values) != 1 {
			return errors.New("operator requires exactly one value")
		}
	}
	seen := map[string]struct{}{}
	for _, value := range rule.Values {
		if err := validateValueType(definition.DataType, value); err != nil {
			return err
		}
		canonical := strings.ToUpper(strings.TrimSpace(fmt.Sprint(value)))
		if _, exists := seen[canonical]; exists {
			return fmt.Errorf("duplicate filter value %v", value)
		}
		seen[canonical] = struct{}{}
		if len(definition.AllowedValues) > 0 && !allowedValue(definition.AllowedValues, value) {
			return fmt.Errorf("value %v is not in the configured catalogue", value)
		}
	}
	if rule.Operator == OperatorBetween {
		comparison, err := compareValues(definition.DataType, rule.Values[0], rule.Values[1])
		if err != nil {
			return err
		}
		if comparison > 0 {
			return errors.New("between lower bound cannot exceed upper bound")
		}
	}
	return nil
}

func validateValueType(dataType DataType, value any) error {
	switch dataType {
	case DataTypeInteger:
		if _, ok := integerValue(value); !ok {
			return fmt.Errorf("value %v must be an integer", value)
		}
	case DataTypeDecimal:
		if _, ok := decimalValue(value); !ok {
			return fmt.Errorf("value %v must be a decimal", value)
		}
	case DataTypeBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("value %v must be a boolean", value)
		}
	case DataTypeDate:
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("value %v must be an ISO date", value)
		}
		if _, err := time.Parse("2006-01-02", strings.TrimSpace(text)); err != nil {
			return fmt.Errorf("value %v must be an ISO date", value)
		}
	default:
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return fmt.Errorf("value %v must be a non-empty string", value)
		}
		if len(text) > 500 {
			return errors.New("text filter value is too long")
		}
	}
	return nil
}
func compareValues(dataType DataType, left, right any) (int, error) {
	switch dataType {
	case DataTypeInteger:
		l, _ := integerValue(left)
		r, _ := integerValue(right)
		if l < r {
			return -1, nil
		}
		if l > r {
			return 1, nil
		}
		return 0, nil
	case DataTypeDecimal:
		l, _ := decimalValue(left)
		r, _ := decimalValue(right)
		if l < r {
			return -1, nil
		}
		if l > r {
			return 1, nil
		}
		return 0, nil
	case DataTypeDate:
		l, _ := time.Parse("2006-01-02", strings.TrimSpace(left.(string)))
		r, _ := time.Parse("2006-01-02", strings.TrimSpace(right.(string)))
		if l.Before(r) {
			return -1, nil
		}
		if l.After(r) {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, errors.New("between is unsupported for this data type")
	}
}
func integerValue(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		if typed != float64(int64(typed)) {
			return 0, false
		}
		return int64(typed), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
func decimalValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
func allowedValue(allowed []string, value any) bool {
	target := strings.ToUpper(strings.TrimSpace(fmt.Sprint(value)))
	for _, item := range allowed {
		if strings.ToUpper(strings.TrimSpace(item)) == target {
			return true
		}
	}
	return false
}
func containsOperator(operators []Operator, target Operator) bool {
	for _, operator := range operators {
		if operator == target {
			return true
		}
	}
	return false
}
