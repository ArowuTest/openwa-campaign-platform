package metacloud

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/message"
)

var (
	ErrTemplateInvalid = errors.New("Meta Cloud template is invalid")
	ErrBindingInvalid  = errors.New("Meta Cloud message binding is invalid")
	ErrBindingConflict = errors.New("Meta Cloud message binding conflicts with immutable evidence")
)

type Template struct {
	ID             string          `json:"id,omitempty"`
	OrganisationID string          `json:"organisationId"`
	WABAID         string          `json:"wabaId"`
	MetaTemplateID string          `json:"metaTemplateId"`
	Name           string          `json:"name"`
	Language       string          `json:"language"`
	Category       string          `json:"category"`
	Status         string          `json:"status"`
	QualitySignal  string          `json:"qualitySignal,omitempty"`
	Components     json.RawMessage `json:"components"`
	ComponentHash  string          `json:"componentHash"`
	LastSyncedAt   time.Time       `json:"lastSyncedAt"`
	CreatedAt      time.Time       `json:"createdAt,omitempty"`
	UpdatedAt      time.Time       `json:"updatedAt,omitempty"`
}

type Binding struct {
	MessageVersionID      string             `json:"messageVersionId"`
	TemplateName          string             `json:"templateName"`
	Language              string             `json:"language"`
	BodyVariableNames     []string           `json:"bodyVariableNames"`
	MediaHeaderType       string             `json:"mediaHeaderType,omitempty"`
	ComponentBindings     []ComponentBinding `json:"componentBindings,omitempty"`
	TemplateComponentHash string             `json:"templateComponentHash"`
	CreatedBy             string             `json:"createdBy"`
	CreatedAt             time.Time          `json:"createdAt"`
}

type TemplateStore interface {
	ReplaceWABATemplates(context.Context, string, string, []Template, time.Time) error
	ListApproved(context.Context, string, string) ([]Template, error)
	FindApproved(context.Context, string, string, string, string) (Template, error)
	CreateBinding(context.Context, Binding) (Binding, error)
	GetBinding(context.Context, string) (Binding, error)
}

func CanonicalComponentHash(raw []byte) (string, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", ErrTemplateInvalid
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", ErrTemplateInvalid
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

var bodyPlaceholderPattern = regexp.MustCompile(`\{\{([1-9][0-9]*)\}\}`)

func validateLegacyBinding(version message.Version, template Template, binding Binding) error {
	if version.Status != message.StatusApproved || strings.ToUpper(strings.TrimSpace(template.Status)) != "APPROVED" {
		return ErrBindingInvalid
	}
	if strings.TrimSpace(binding.MessageVersionID) != version.ID ||
		strings.TrimSpace(binding.TemplateName) != strings.TrimSpace(template.Name) ||
		strings.TrimSpace(binding.Language) != strings.TrimSpace(template.Language) ||
		binding.TemplateComponentHash == "" || binding.TemplateComponentHash != template.ComponentHash {
		return ErrBindingInvalid
	}
	if len(binding.BodyVariableNames) != len(version.Variables) {
		return ErrBindingInvalid
	}
	for i, variable := range version.Variables {
		if strings.TrimSpace(binding.BodyVariableNames[i]) != strings.TrimSpace(variable.Name) {
			return ErrBindingInvalid
		}
	}
	placeholderCount, err := templateBodyPlaceholderCount(template.Components)
	if err != nil || placeholderCount != len(binding.BodyVariableNames) {
		return ErrBindingInvalid
	}
	expectedHeader := ""
	switch version.Type {
	case message.TypeText:
	case message.TypeImageCaption:
		expectedHeader = "IMAGE"
	case message.TypeVideo:
		expectedHeader = "VIDEO"
	case message.TypeDocument:
		expectedHeader = "DOCUMENT"
	default:
		return ErrBindingInvalid
	}
	if strings.ToUpper(strings.TrimSpace(binding.MediaHeaderType)) != expectedHeader {
		return ErrBindingInvalid
	}
	templateHeader, err := templateMediaHeaderType(template.Components)
	if err != nil || templateHeader != expectedHeader {
		return ErrBindingInvalid
	}
	return nil
}

func templateBodyPlaceholderCount(raw []byte) (int, error) {
	var components []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &components); err != nil {
		return 0, ErrTemplateInvalid
	}
	max := 0
	for _, component := range components {
		if strings.ToUpper(strings.TrimSpace(component.Type)) != "BODY" {
			continue
		}
		for _, match := range bodyPlaceholderPattern.FindAllStringSubmatch(component.Text, -1) {
			var value int
			for _, digit := range match[1] {
				value = value*10 + int(digit-'0')
			}
			if value > max {
				max = value
			}
		}
	}
	return max, nil
}

type MemoryTemplateStore struct {
	mu        sync.Mutex
	templates map[string][]Template
	bindings  map[string]Binding
	syncedAt  map[string]time.Time
}

func NewMemoryTemplateStore() *MemoryTemplateStore {
	return &MemoryTemplateStore{templates: map[string][]Template{}, bindings: map[string]Binding{}, syncedAt: map[string]time.Time{}}
}

func templateScope(organisationID, wabaID string) string {
	return strings.TrimSpace(organisationID) + "\x00" + strings.TrimSpace(wabaID)
}

func (m *MemoryTemplateStore) ReplaceWABATemplates(_ context.Context, organisationID, wabaID string, values []Template, syncedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(organisationID) == "" || strings.TrimSpace(wabaID) == "" || syncedAt.IsZero() {
		return ErrTemplateInvalid
	}
	scope := templateScope(organisationID, wabaID)
	syncedAt = syncedAt.UTC()
	if previous, ok := m.syncedAt[scope]; ok && !syncedAt.After(previous) {
		return nil
	}
	copied := make([]Template, 0, len(values))
	for _, value := range values {
		value.OrganisationID, value.WABAID = strings.TrimSpace(organisationID), strings.TrimSpace(wabaID)
		value.Status = strings.ToUpper(strings.TrimSpace(value.Status))
		value.LastSyncedAt = syncedAt.UTC()
		if value.ComponentHash == "" {
			hash, err := CanonicalComponentHash(value.Components)
			if err != nil {
				return err
			}
			value.ComponentHash = hash
		}
		if value.MetaTemplateID == "" || value.Name == "" || value.Language == "" || value.Category == "" {
			return ErrTemplateInvalid
		}
		copied = append(copied, value)
	}
	m.templates[scope] = copied
	m.syncedAt[scope] = syncedAt
	return nil
}

func (m *MemoryTemplateStore) ListApproved(_ context.Context, organisationID, wabaID string) ([]Template, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Template{}
	for _, value := range m.templates[templateScope(organisationID, wabaID)] {
		if strings.EqualFold(value.Status, "APPROVED") {
			out = append(out, value)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Language < out[j].Language
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (m *MemoryTemplateStore) FindApproved(ctx context.Context, organisationID, wabaID, name, language string) (Template, error) {
	values, err := m.ListApproved(ctx, organisationID, wabaID)
	if err != nil {
		return Template{}, err
	}
	for _, value := range values {
		if value.Name == strings.TrimSpace(name) && value.Language == strings.TrimSpace(language) {
			return value, nil
		}
	}
	return Template{}, ErrNotFound
}

func bindingEqual(a, b Binding) bool {
	if a.MessageVersionID != b.MessageVersionID || a.TemplateName != b.TemplateName || a.Language != b.Language || a.MediaHeaderType != b.MediaHeaderType || a.TemplateComponentHash != b.TemplateComponentHash || a.CreatedBy != b.CreatedBy {
		return false
	}
	if !sameStrings(a.BodyVariableNames, b.BodyVariableNames) || len(a.ComponentBindings) != len(b.ComponentBindings) {
		return false
	}
	for i := range a.ComponentBindings {
		left, right := a.ComponentBindings[i], b.ComponentBindings[i]
		if left.Type != right.Type || left.SubType != right.SubType || left.Index != right.Index || !sameStrings(left.ParameterNames, right.ParameterNames) {
			return false
		}
	}
	return true
}

func (m *MemoryTemplateStore) CreateBinding(_ context.Context, value Binding) (Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(value.MessageVersionID) == "" || strings.TrimSpace(value.TemplateName) == "" || strings.TrimSpace(value.Language) == "" || len(value.TemplateComponentHash) != 64 {
		return Binding{}, ErrBindingInvalid
	}
	if current, ok := m.bindings[value.MessageVersionID]; ok {
		if bindingEqual(current, value) {
			return current, nil
		}
		return Binding{}, ErrBindingConflict
	}
	m.bindings[value.MessageVersionID] = value
	return value, nil
}

func (m *MemoryTemplateStore) GetBinding(_ context.Context, messageVersionID string) (Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.bindings[strings.TrimSpace(messageVersionID)]
	if !ok {
		return Binding{}, ErrNotFound
	}
	return value, nil
}

var _ TemplateStore = (*MemoryTemplateStore)(nil)

// ErrTemplateBindingInvalid is the public binding-validation sentinel.
var ErrTemplateBindingInvalid = ErrBindingInvalid

func (t Template) Sendable() bool {
	return strings.EqualFold(strings.TrimSpace(t.Status), "APPROVED") &&
		strings.TrimSpace(t.Name) != "" && strings.TrimSpace(t.Language) != "" &&
		len(strings.TrimSpace(t.ComponentHash)) == 64
}

func BuildBinding(version message.Version, template Template, names []string, mediaHeaderType, actor string, at time.Time) (Binding, error) {
	value := Binding{MessageVersionID: strings.TrimSpace(version.ID), TemplateName: strings.TrimSpace(template.Name),
		Language: strings.TrimSpace(template.Language), BodyVariableNames: append([]string(nil), names...),
		MediaHeaderType: strings.ToUpper(strings.TrimSpace(mediaHeaderType)), TemplateComponentHash: strings.TrimSpace(template.ComponentHash),
		CreatedBy: strings.TrimSpace(actor), CreatedAt: at.UTC()}
	if value.CreatedBy == "" || at.IsZero() {
		return Binding{}, ErrBindingInvalid
	}
	normalized, err := CanonicalizeBinding(version, template, value)
	if err != nil {
		return Binding{}, err
	}
	return normalized, nil
}

func templateMediaHeaderType(raw []byte) (string, error) {
	var components []struct {
		Type   string `json:"type"`
		Format string `json:"format"`
	}
	if err := json.Unmarshal(raw, &components); err != nil {
		return "", ErrTemplateInvalid
	}
	for _, component := range components {
		if strings.EqualFold(strings.TrimSpace(component.Type), "HEADER") {
			format := strings.ToUpper(strings.TrimSpace(component.Format))
			switch format {
			case "IMAGE", "VIDEO", "DOCUMENT":
				return format, nil
			}
		}
	}
	return "", nil
}
