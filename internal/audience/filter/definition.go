package filter

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type DataType string

const (
	DataTypeText         DataType = "text"
	DataTypeInteger      DataType = "integer"
	DataTypeDecimal      DataType = "decimal"
	DataTypeBoolean      DataType = "boolean"
	DataTypeDate         DataType = "date"
	DataTypeSingleSelect DataType = "single_select"
	DataTypeMultiSelect  DataType = "multi_select"
	DataTypeGeography    DataType = "geography"
)

type Operator string

const (
	OperatorEquals      Operator = "equals"
	OperatorNotEquals   Operator = "not_equals"
	OperatorContains    Operator = "contains"
	OperatorIn          Operator = "in"
	OperatorNotIn       Operator = "not_in"
	OperatorBetween     Operator = "between"
	OperatorGreaterThan Operator = "greater_than"
	OperatorLessThan    Operator = "less_than"
	OperatorIsKnown     Operator = "is_known"
	OperatorIsUnknown   Operator = "is_unknown"
)

type Storage string

const (
	StorageCoreColumn       Storage = "core_column"
	StorageContactAttribute Storage = "contact_attribute"
)

type Definition struct {
	Code                 string     `json:"code"`
	DisplayName          string     `json:"displayName"`
	Description          string     `json:"description"`
	DataType             DataType   `json:"dataType"`
	Operators            []Operator `json:"operators"`
	Core                 bool       `json:"core"`
	Filterable           bool       `json:"filterable"`
	Reportable           bool       `json:"reportable"`
	Sensitive            bool       `json:"sensitive"`
	RequiresPermission   string     `json:"requiresPermission,omitempty"`
	Storage              Storage    `json:"storage"`
	QueryableField       string     `json:"queryableField,omitempty"`
	AttributeValueColumn string     `json:"attributeValueColumn,omitempty"`
	IndexStrategy        string     `json:"indexStrategy"`
	AllowedValues        []string   `json:"allowedValues,omitempty"`
	DisplayOrder         int        `json:"displayOrder"`
	Active               bool       `json:"active"`
	Version              int64      `json:"version,omitempty"`
	CreatedAt            time.Time  `json:"createdAt,omitempty"`
	UpdatedAt            time.Time  `json:"updatedAt,omitempty"`
}

func (d Definition) Validate() error {
	if strings.TrimSpace(d.Code) == "" {
		return errors.New("filter definition code is required")
	}
	if strings.ToUpper(d.Code) != d.Code {
		return fmt.Errorf("filter definition code %q must be uppercase", d.Code)
	}
	if len(d.Code) > 64 {
		return fmt.Errorf("filter definition code %q is too long", d.Code)
	}
	if strings.TrimSpace(d.DisplayName) == "" {
		return fmt.Errorf("filter definition %s requires a display name", d.Code)
	}
	switch d.DataType {
	case DataTypeText, DataTypeInteger, DataTypeDecimal, DataTypeBoolean, DataTypeDate, DataTypeSingleSelect, DataTypeMultiSelect, DataTypeGeography:
	default:
		return fmt.Errorf("filter definition %s has unsupported data type %q", d.Code, d.DataType)
	}
	if d.Filterable && len(d.Operators) == 0 {
		return fmt.Errorf("filter definition %s requires at least one operator", d.Code)
	}
	if d.Storage != StorageCoreColumn && d.Storage != StorageContactAttribute {
		return fmt.Errorf("filter definition %s has unsupported storage %q", d.Code, d.Storage)
	}
	if d.Storage == StorageCoreColumn {
		if strings.TrimSpace(d.QueryableField) == "" {
			return fmt.Errorf("filter definition %s requires a queryable field", d.Code)
		}
	}
	if d.Storage == StorageContactAttribute {
		if d.Core || d.QueryableField != "" {
			return fmt.Errorf("dynamic filter definition %s cannot declare a core SQL field", d.Code)
		}
		expected := AttributeValueColumnForDataType(d.DataType)
		if expected == "" || d.AttributeValueColumn != expected {
			return fmt.Errorf("filter definition %s requires typed attribute column %s", d.Code, expected)
		}
	}
	if d.Sensitive && strings.TrimSpace(d.RequiresPermission) == "" {
		return fmt.Errorf("sensitive filter definition %s requires a permission", d.Code)
	}
	if strings.ContainsAny(d.RequiresPermission, " \t\r\n") {
		return fmt.Errorf("filter definition %s has an invalid permission code", d.Code)
	}
	if d.DisplayOrder < 0 || d.DisplayOrder > 100000 {
		return fmt.Errorf("filter definition %s has an invalid display order", d.Code)
	}
	seenOps := map[Operator]struct{}{}
	for _, op := range d.Operators {
		if !operatorAllowedForType(d.DataType, op) {
			return fmt.Errorf("operator %s is not valid for %s", op, d.DataType)
		}
		if _, exists := seenOps[op]; exists {
			return fmt.Errorf("filter definition %s contains duplicate operator %s", d.Code, op)
		}
		seenOps[op] = struct{}{}
	}
	seenValues := map[string]struct{}{}
	for _, value := range d.AllowedValues {
		value = strings.TrimSpace(value)
		if value == "" {
			return fmt.Errorf("filter definition %s contains a blank allowed value", d.Code)
		}
		key := strings.ToUpper(value)
		if _, exists := seenValues[key]; exists {
			return fmt.Errorf("filter definition %s contains duplicate allowed value %q", d.Code, value)
		}
		seenValues[key] = struct{}{}
	}
	return nil
}

func DefaultOperators(dataType DataType) []Operator {
	switch dataType {
	case DataTypeInteger, DataTypeDecimal, DataTypeDate:
		return []Operator{OperatorEquals, OperatorNotEquals, OperatorBetween, OperatorGreaterThan, OperatorLessThan, OperatorIsKnown, OperatorIsUnknown}
	case DataTypeBoolean:
		return []Operator{OperatorEquals, OperatorIsKnown, OperatorIsUnknown}
	case DataTypeSingleSelect, DataTypeMultiSelect, DataTypeGeography:
		return []Operator{OperatorIn, OperatorNotIn, OperatorIsKnown, OperatorIsUnknown}
	case DataTypeText:
		return []Operator{OperatorEquals, OperatorNotEquals, OperatorContains, OperatorIn, OperatorNotIn, OperatorIsKnown, OperatorIsUnknown}
	default:
		return nil
	}
}

func AttributeValueColumnForDataType(dataType DataType) string {
	switch dataType {
	case DataTypeInteger:
		return "value_integer"
	case DataTypeDecimal:
		return "value_decimal"
	case DataTypeBoolean:
		return "value_boolean"
	case DataTypeDate:
		return "value_date"
	case DataTypeText, DataTypeSingleSelect, DataTypeMultiSelect, DataTypeGeography:
		return "value_text"
	default:
		return ""
	}
}

func CloneDefinition(d Definition) Definition {
	d.Operators = append([]Operator(nil), d.Operators...)
	d.AllowedValues = append([]string(nil), d.AllowedValues...)
	return d
}

func normaliseDefinition(d Definition) Definition {
	d.Code = strings.ToUpper(strings.TrimSpace(d.Code))
	d.DisplayName = strings.TrimSpace(d.DisplayName)
	d.Description = strings.TrimSpace(d.Description)
	d.RequiresPermission = strings.TrimSpace(d.RequiresPermission)
	d.QueryableField = strings.TrimSpace(d.QueryableField)
	d.AttributeValueColumn = strings.TrimSpace(d.AttributeValueColumn)
	d.IndexStrategy = strings.TrimSpace(d.IndexStrategy)
	d.Operators = append([]Operator(nil), d.Operators...)
	d.AllowedValues = append([]string(nil), d.AllowedValues...)
	sort.SliceStable(d.AllowedValues, func(i, j int) bool { return strings.ToUpper(d.AllowedValues[i]) < strings.ToUpper(d.AllowedValues[j]) })
	return d
}

func operatorAllowedForType(dataType DataType, op Operator) bool {
	for _, allowed := range DefaultOperators(dataType) {
		if allowed == op {
			return true
		}
	}
	return false
}
