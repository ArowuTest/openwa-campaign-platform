package metacloud

import (
	"sort"
	"strings"

	"campaign-platform/internal/message"
)

type ComponentBinding struct {
	Type           string   `json:"type"`
	SubType        string   `json:"subType,omitempty"`
	Index          int      `json:"index,omitempty"`
	ParameterNames []string `json:"parameterNames,omitempty"`
}

func ValidateBinding(version message.Version, template Template, binding Binding) error {
	_, err := CanonicalizeBinding(version, template, binding)
	return err
}

func CanonicalizeBinding(version message.Version, template Template, binding Binding) (Binding, error) {
	binding.MessageVersionID = strings.TrimSpace(binding.MessageVersionID)
	binding.TemplateName = strings.TrimSpace(binding.TemplateName)
	binding.Language = strings.TrimSpace(binding.Language)
	binding.MediaHeaderType = strings.ToUpper(strings.TrimSpace(binding.MediaHeaderType))
	binding.TemplateComponentHash = strings.TrimSpace(binding.TemplateComponentHash)
	binding.CreatedBy = strings.TrimSpace(binding.CreatedBy)
	for i := range binding.BodyVariableNames {
		binding.BodyVariableNames[i] = strings.TrimSpace(binding.BodyVariableNames[i])
	}
	if len(binding.ComponentBindings) == 0 {
		binding.ComponentBindings = componentsFromLegacyBinding(binding)
	} else {
		components, bodyNames, mediaHeader, err := normalizeComponentBindings(binding.ComponentBindings)
		if err != nil {
			return Binding{}, err
		}
		if len(binding.BodyVariableNames) > 0 && !sameStrings(binding.BodyVariableNames, bodyNames) {
			return Binding{}, ErrBindingInvalid
		}
		if binding.MediaHeaderType != "" && binding.MediaHeaderType != mediaHeader {
			return Binding{}, ErrBindingInvalid
		}
		binding.ComponentBindings = components
		binding.BodyVariableNames = bodyNames
		binding.MediaHeaderType = mediaHeader
	}
	if err := validateLegacyBinding(version, template, binding); err != nil {
		return Binding{}, err
	}
	return binding, nil
}

func componentsFromLegacyBinding(binding Binding) []ComponentBinding {
	components := make([]ComponentBinding, 0, 2)
	if binding.MediaHeaderType != "" {
		components = append(components, ComponentBinding{Type: "HEADER", SubType: binding.MediaHeaderType})
	}
	if len(binding.BodyVariableNames) > 0 {
		components = append(components, ComponentBinding{Type: "BODY", SubType: "TEXT", ParameterNames: append([]string(nil), binding.BodyVariableNames...)})
	}
	return components
}

func normalizeComponentBindings(input []ComponentBinding) ([]ComponentBinding, []string, string, error) {
	components := make([]ComponentBinding, 0, len(input))
	var bodyNames []string
	mediaHeader := ""
	seenBody, seenHeader := false, false
	for _, raw := range input {
		value := ComponentBinding{Type: strings.ToUpper(strings.TrimSpace(raw.Type)), SubType: strings.ToUpper(strings.TrimSpace(raw.SubType)), Index: raw.Index}
		if value.Index < 0 {
			return nil, nil, "", ErrBindingInvalid
		}
		for _, rawName := range raw.ParameterNames {
			name := strings.TrimSpace(rawName)
			if name == "" {
				return nil, nil, "", ErrBindingInvalid
			}
			value.ParameterNames = append(value.ParameterNames, name)
		}
		switch value.Type {
		case "BODY":
			if seenBody || value.Index != 0 {
				return nil, nil, "", ErrBindingInvalid
			}
			seenBody = true
			if value.SubType == "" {
				value.SubType = "TEXT"
			}
			if value.SubType != "TEXT" {
				return nil, nil, "", ErrBindingInvalid
			}
			bodyNames = append([]string(nil), value.ParameterNames...)
		case "HEADER":
			if seenHeader || value.Index != 0 || len(value.ParameterNames) != 0 {
				return nil, nil, "", ErrBindingInvalid
			}
			seenHeader = true
			switch value.SubType {
			case "IMAGE", "VIDEO", "DOCUMENT":
				mediaHeader = value.SubType
			default:
				return nil, nil, "", ErrBindingInvalid
			}
		default:
			return nil, nil, "", ErrBindingInvalid
		}
		components = append(components, value)
	}
	sort.SliceStable(components, func(i, j int) bool {
		left, right := componentSortRank(components[i]), componentSortRank(components[j])
		if left != right {
			return left < right
		}
		return components[i].Index < components[j].Index
	})
	return components, bodyNames, mediaHeader, nil
}

func componentSortRank(value ComponentBinding) int {
	switch value.Type {
	case "HEADER":
		return 0
	case "BODY":
		return 1
	case "BUTTON":
		return 2
	default:
		return 3
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
