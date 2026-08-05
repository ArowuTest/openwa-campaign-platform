package message

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var placeholderPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z][a-zA-Z0-9_.-]{0,63})\s*\}\}`)

type RenderMode string

const (
	RenderPreview  RenderMode = "PREVIEW"
	RenderDispatch RenderMode = "DISPATCH"
)

type RenderInput struct {
	Values        map[string]string `json:"values"`
	Mode          RenderMode        `json:"mode"`
	MaskSensitive bool              `json:"maskSensitive"`
}

type RenderResult struct {
	Body           string            `json:"body"`
	Resolved       map[string]string `json:"resolved"`
	UsedFallbacks  []string          `json:"usedFallbacks"`
	Missing        []string          `json:"missing"`
	Undeclared     []string          `json:"undeclared"`
	CharacterCount int               `json:"characterCount"`
	ByteCount      int               `json:"byteCount"`
}

func Render(v Version, input RenderInput) (RenderResult, error) {
	if v.Status != StatusDraft && v.Status != StatusApproved {
		return RenderResult{}, errors.New("message version is not renderable")
	}
	if input.Mode == "" {
		input.Mode = RenderPreview
	}
	if input.Mode != RenderPreview && input.Mode != RenderDispatch {
		return RenderResult{}, errors.New("unsupported render mode")
	}
	declared := make(map[string]Variable, len(v.Variables))
	for _, variable := range v.Variables {
		name := strings.TrimSpace(variable.Name)
		if name == "" {
			return RenderResult{}, errors.New("message variable name is required")
		}
		if _, exists := declared[name]; exists {
			return RenderResult{}, fmt.Errorf("duplicate message variable %q", name)
		}
		declared[name] = variable
	}

	matches := placeholderPattern.FindAllStringSubmatch(v.Body, -1)
	referenced := map[string]struct{}{}
	undeclaredSet := map[string]struct{}{}
	for _, match := range matches {
		name := match[1]
		referenced[name] = struct{}{}
		if _, ok := declared[name]; !ok {
			undeclaredSet[name] = struct{}{}
		}
	}
	if len(undeclaredSet) > 0 {
		undeclared := sortedKeys(undeclaredSet)
		return RenderResult{Undeclared: undeclared}, fmt.Errorf("message contains undeclared placeholders: %s", strings.Join(undeclared, ", "))
	}

	resolved := map[string]string{}
	usedFallbacks := []string{}
	missing := []string{}
	for name := range referenced {
		variable := declared[name]
		value := strings.TrimSpace(input.Values[name])
		if value == "" {
			value = strings.TrimSpace(variable.Fallback)
			if value != "" {
				usedFallbacks = append(usedFallbacks, name)
			}
		}
		if value == "" {
			missing = append(missing, name)
			continue
		}
		if input.MaskSensitive && isSensitiveVariable(variable) {
			value = maskValue(value)
		}
		resolved[name] = value
	}
	sort.Strings(usedFallbacks)
	sort.Strings(missing)
	if input.Mode == RenderDispatch && len(missing) > 0 {
		return RenderResult{Resolved: resolved, UsedFallbacks: usedFallbacks, Missing: missing}, fmt.Errorf("required message values are missing: %s", strings.Join(missing, ", "))
	}

	rendered := placeholderPattern.ReplaceAllStringFunc(v.Body, func(token string) string {
		sub := placeholderPattern.FindStringSubmatch(token)
		if len(sub) != 2 {
			return token
		}
		if value, ok := resolved[sub[1]]; ok {
			return value
		}
		if input.Mode == RenderPreview {
			return "[missing:" + sub[1] + "]"
		}
		return ""
	})
	return RenderResult{
		Body:           rendered,
		Resolved:       resolved,
		UsedFallbacks:  usedFallbacks,
		Missing:        missing,
		CharacterCount: utf8.RuneCountInString(rendered),
		ByteCount:      len([]byte(rendered)),
	}, nil
}

func ValidateTemplate(v Version) error {
	_, err := Render(v, RenderInput{Mode: RenderPreview})
	return err
}

func isSensitiveVariable(v Variable) bool {
	value := strings.ToUpper(strings.TrimSpace(v.DataType))
	return value == "MSISDN" || value == "EMAIL" || value == "PERSONAL" || value == "SENSITIVE"
}

func maskValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= 4 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:2]) + strings.Repeat("*", len(runes)-4) + string(runes[len(runes)-2:])
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
