package message

import (
	"campaign-platform/internal/shared/id"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Type string

const (
	TypeText         Type = "TEXT"
	TypeImageCaption Type = "IMAGE_CAPTION"
	TypeVideo        Type = "VIDEO"
	TypeDocument     Type = "DOCUMENT"
)

type Status string

const (
	StatusDraft      Status = "DRAFT"
	StatusApproved   Status = "APPROVED"
	StatusSuperseded Status = "SUPERSEDED"
)

type Media struct {
	ObjectKey  string `json:"objectKey"`
	SHA256     string `json:"sha256"`
	MediaType  string `json:"mediaType"`
	Size       int64  `json:"size"`
	ScanStatus string `json:"scanStatus"`
}
type Link struct {
	URL   string `json:"url"`
	Label string `json:"label,omitempty"`
}
type Variable struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Fallback string `json:"fallback"`
}
type Version struct {
	ID             string     `json:"id"`
	CampaignID     string     `json:"campaignId"`
	Version        int        `json:"version"`
	Type           Type       `json:"type"`
	Body           string     `json:"body,omitempty"`
	Media          *Media     `json:"media,omitempty"`
	Links          []Link     `json:"links"`
	Variables      []Variable `json:"variables"`
	ContentHash    string     `json:"contentHash"`
	IdempotencyKey string     `json:"-"`
	Status         Status     `json:"status"`
	CreatedBy      string     `json:"createdBy"`
	ApprovedBy     string     `json:"approvedBy,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	ApprovedAt     *time.Time `json:"approvedAt,omitempty"`
}
type Input struct {
	CampaignID     string
	Version        int
	Type           Type
	Body           string
	Media          *Media
	Links          []Link
	Variables      []Variable
	CreatedBy      string
	IdempotencyKey string
	AllowedHosts   []string
}

func NewDraft(input Input, now time.Time) (Version, error) {
	if strings.TrimSpace(input.CampaignID) == "" || strings.TrimSpace(input.CreatedBy) == "" || input.Version <= 0 {
		return Version{}, errors.New("campaign, version and creator are required")
	}
	if err := ValidateIdempotencyKey(input.IdempotencyKey); err != nil {
		return Version{}, err
	}
	if input.Type != TypeText && input.Type != TypeImageCaption && input.Type != TypeVideo && input.Type != TypeDocument {
		return Version{}, errors.New("unsupported message type")
	}
	body := strings.TrimSpace(input.Body)
	if input.Type == TypeText && body == "" {
		return Version{}, errors.New("text body is required")
	}
	if len([]byte(body)) > 65536 {
		return Version{}, errors.New("message body exceeds 64 KiB")
	}
	for _, character := range body {
		if character < 0x20 && character != '\n' && character != '\r' && character != '\t' {
			return Version{}, errors.New("message body contains prohibited control characters")
		}
	}
	if input.Type != TypeText && (input.Media == nil || input.Media.ObjectKey == "") {
		return Version{}, errors.New("media reference is required")
	}
	if err := validateVariables(input.Variables); err != nil {
		return Version{}, err
	}
	if input.Media != nil {
		if input.Media.SHA256 == "" || input.Media.Size < 0 || input.Media.ScanStatus != "CLEAN" {
			return Version{}, errors.New("media must have checksum, valid size and CLEAN scan status")
		}
	}
	if err := validateLinks(input.Links, input.AllowedHosts); err != nil {
		return Version{}, err
	}
	identifier, err := id.New()
	if err != nil {
		return Version{}, err
	}
	v := Version{ID: identifier, CampaignID: strings.TrimSpace(input.CampaignID), Version: input.Version, Type: input.Type, Body: strings.TrimSpace(input.Body), Media: cloneMedia(input.Media), Links: append([]Link(nil), input.Links...), Variables: append([]Variable(nil), input.Variables...), IdempotencyKey: strings.TrimSpace(input.IdempotencyKey), Status: StatusDraft, CreatedBy: input.CreatedBy, CreatedAt: now.UTC()}
	if err := ValidateTemplate(v); err != nil {
		return Version{}, err
	}
	v.ContentHash = hash(v)
	return v, nil
}
func (v Version) Approve(actor string, now time.Time) (Version, error) {
	if v.Status != StatusDraft {
		return Version{}, errors.New("only draft message can be approved")
	}
	if strings.TrimSpace(actor) == "" || actor == v.CreatedBy {
		return Version{}, errors.New("approval requires a different authorised user")
	}
	approved := now.UTC()
	v.Status = StatusApproved
	v.ApprovedBy = actor
	v.ApprovedAt = &approved
	return v, nil
}
func (v Version) CloneAsNext(input Input, now time.Time) (Version, error) {
	input.CampaignID = v.CampaignID
	input.Version = v.Version + 1
	return NewDraft(input, now)
}
func ValidateIdempotencyKey(value string) error {
	value = strings.TrimSpace(value)
	if len(value) < 16 || len(value) > 128 {
		return errors.New("idempotency key must contain 16 to 128 characters")
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("-_.:", character) {
			continue
		}
		return errors.New("idempotency key contains unsafe characters")
	}
	return nil
}

func validateVariables(variables []Variable) error {
	if len(variables) > 64 {
		return errors.New("message declares more than 64 variables")
	}
	allowed := map[string]bool{"TEXT": true, "INTEGER": true, "DECIMAL": true, "BOOLEAN": true, "DATE": true, "MSISDN": true, "EMAIL": true, "PERSONAL": true, "SENSITIVE": true}
	seen := map[string]struct{}{}
	for _, variable := range variables {
		name := strings.TrimSpace(variable.Name)
		if !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]{0,63}$`).MatchString(name) {
			return errors.New("message variable name is invalid")
		}
		if _, exists := seen[name]; exists {
			return errors.New("duplicate message variable: " + name)
		}
		seen[name] = struct{}{}
		dataType := strings.ToUpper(strings.TrimSpace(variable.DataType))
		if dataType == "" {
			dataType = "TEXT"
		}
		if !allowed[dataType] {
			return errors.New("unsupported message variable data type: " + dataType)
		}
		if len([]byte(variable.Fallback)) > 1024 {
			return errors.New("message variable fallback exceeds 1024 bytes")
		}
	}
	return nil
}

func validateLinks(links []Link, allowed []string) error {
	hosts := map[string]struct{}{}
	for _, h := range allowed {
		hosts[strings.ToLower(strings.TrimSpace(h))] = struct{}{}
	}
	for _, link := range links {
		parsed, err := url.Parse(strings.TrimSpace(link.URL))
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
			return errors.New("destination links must be valid HTTPS URLs")
		}
		if len(hosts) > 0 {
			if _, ok := hosts[strings.ToLower(parsed.Hostname())]; !ok {
				return errors.New("destination host is not allow-listed")
			}
		}
	}
	return nil
}
func hash(v Version) string {
	links := append([]Link(nil), v.Links...)
	sort.Slice(links, func(i, j int) bool { return links[i].URL < links[j].URL })
	variables := append([]Variable(nil), v.Variables...)
	sort.Slice(variables, func(i, j int) bool { return variables[i].Name < variables[j].Name })
	payload, _ := json.Marshal(struct {
		Type      Type
		Body      string
		Media     *Media
		Links     []Link
		Variables []Variable
	}{v.Type, v.Body, v.Media, links, variables})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
func cloneMedia(m *Media) *Media {
	if m == nil {
		return nil
	}
	v := *m
	return &v
}
