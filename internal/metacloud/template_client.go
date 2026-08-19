package metacloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type TemplateListRequest struct {
	CredentialKey   string
	GraphAPIVersion string
	WABAID          string
}

type templatePage struct {
	Data []struct {
		ID           string          `json:"id"`
		Name         string          `json:"name"`
		Language     string          `json:"language"`
		Category     string          `json:"category"`
		Status       string          `json:"status"`
		QualityScore json.RawMessage `json:"quality_score"`
		Components   json.RawMessage `json:"components"`
	} `json:"data"`
	Paging struct {
		Next string `json:"next"`
	} `json:"paging"`
}

func (c *Client) ListTemplates(ctx context.Context, request TemplateListRequest) ([]Template, error) {
	if c == nil || c.Credentials == nil {
		return nil, APIError{Code: "META_CONFIGURATION_INVALID", Safety: SafetyPermanent}
	}
	credential, err := c.Credentials.Resolve(strings.TrimSpace(request.CredentialKey))
	if err != nil {
		return nil, APIError{Code: "META_CREDENTIAL_UNAVAILABLE", Safety: SafetyPermanent, Err: err}
	}
	version, wabaID := strings.TrimSpace(request.GraphAPIVersion), strings.TrimSpace(request.WABAID)
	if !graphVersionPattern.MatchString(version) || wabaID == "" || strings.ContainsAny(wabaID, "/?#\\") {
		return nil, APIError{Code: "META_REQUEST_INVALID", Safety: SafetyPermanent, Err: errors.New("graph version and WABA ID are required")}
	}
	base, err := c.messageBaseURL()
	if err != nil {
		return nil, APIError{Code: "META_CONFIGURATION_INVALID", Safety: SafetyPermanent, Err: err}
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + url.PathEscape(version) + "/" + url.PathEscape(wabaID) + "/message_templates"
	query := base.Query()
	query.Set("fields", "id,name,language,category,status,quality_score,components")
	query.Set("limit", "100")
	base.RawQuery = query.Encode()
	originScheme, originHost, expectedPath := base.Scheme, base.Host, base.Path
	items := []Template{}
	next := base.String()
	for page := 0; next != "" && page < 100; page++ {
		values, nextURL, fetchErr := c.fetchTemplatePage(ctx, next, credential.AccessToken)
		if fetchErr != nil {
			return nil, fetchErr
		}
		items = append(items, values...)
		if len(items) > 10000 {
			return nil, APIError{Code: "META_TEMPLATE_LIMIT_EXCEEDED", Safety: SafetyPermanent}
		}
		if nextURL != "" {
			parsed, parseErr := url.Parse(nextURL)
			if parseErr != nil || parsed.Scheme != originScheme || parsed.Host != originHost || parsed.Path != expectedPath {
				return nil, APIError{Code: "META_PAGINATION_INVALID", Safety: SafetyPermanent, Err: errors.New("template pagination left the governed endpoint")}
			}
		}
		next = nextURL
	}
	if next != "" {
		return nil, APIError{Code: "META_TEMPLATE_PAGE_LIMIT", Safety: SafetyPermanent}
	}
	return items, nil
}

func (c *Client) fetchTemplatePage(ctx context.Context, target, accessToken string) ([]Template, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, "", APIError{Code: "META_REQUEST_INVALID", Safety: SafetyPermanent, Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := c.hardenedHTTPClient().Do(req)
	if err != nil {
		return nil, "", APIError{Code: "META_TEMPLATE_TRANSPORT_FAILED", Safety: SafetySafeToRetry, Err: err}
	}
	defer response.Body.Close()
	limit := c.MaximumResponseBytes
	if limit <= 0 || limit > 1<<20 {
		limit = 1 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, "", APIError{Code: "META_TEMPLATE_RESPONSE_READ_FAILED", Safety: SafetySafeToRetry, Err: err}
	}
	if int64(len(raw)) > limit {
		return nil, "", APIError{Code: "META_TEMPLATE_RESPONSE_TOO_LARGE", Safety: SafetyPermanent}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		safety := SafetyPermanent
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			safety = SafetySafeToRetry
		}
		return nil, "", APIError{Code: fmt.Sprintf("META_TEMPLATE_HTTP_%d", response.StatusCode), Safety: safety, Detail: redactedMetaDetail(raw)}
	}
	var page templatePage
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, "", APIError{Code: "META_TEMPLATE_RESPONSE_INVALID", Safety: SafetySafeToRetry, Err: err}
	}
	out := make([]Template, 0, len(page.Data))
	for _, item := range page.Data {
		hash, hashErr := CanonicalComponentHash(item.Components)
		if hashErr != nil || strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Language) == "" || strings.TrimSpace(item.Category) == "" || strings.TrimSpace(item.Status) == "" {
			return nil, "", APIError{Code: "META_TEMPLATE_RESPONSE_INVALID", Safety: SafetyPermanent, Err: ErrTemplateInvalid}
		}
		out = append(out, Template{MetaTemplateID: strings.TrimSpace(item.ID), Name: strings.TrimSpace(item.Name), Language: strings.TrimSpace(item.Language), Category: strings.ToUpper(strings.TrimSpace(item.Category)), Status: strings.ToUpper(strings.TrimSpace(item.Status)), QualitySignal: decodeQualitySignal(item.QualityScore), Components: append([]byte(nil), item.Components...), ComponentHash: hash})
	}
	return out, strings.TrimSpace(page.Paging.Next), nil
}

func decodeQualitySignal(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var object struct {
		Score string `json:"score"`
	}
	if json.Unmarshal(raw, &object) == nil {
		return strings.TrimSpace(object.Score)
	}
	return ""
}

func DecodeTemplatePage(raw []byte, organisationID, wabaID string, syncedAt time.Time) ([]Template, error) {
	var page templatePage
	if err := json.Unmarshal(raw, &page); err != nil || strings.TrimSpace(organisationID) == "" || strings.TrimSpace(wabaID) == "" || syncedAt.IsZero() {
		return nil, ErrTemplateInvalid
	}
	return decodeTemplateItems(page, organisationID, wabaID, syncedAt.UTC())
}

func decodeTemplateItems(page templatePage, organisationID, wabaID string, syncedAt time.Time) ([]Template, error) {
	out := make([]Template, 0, len(page.Data))
	for _, item := range page.Data {
		hash, err := CanonicalComponentHash(item.Components)
		if err != nil || strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Language) == "" || strings.TrimSpace(item.Category) == "" || strings.TrimSpace(item.Status) == "" {
			return nil, ErrTemplateInvalid
		}
		out = append(out, Template{
			OrganisationID: strings.TrimSpace(organisationID), WABAID: strings.TrimSpace(wabaID),
			MetaTemplateID: strings.TrimSpace(item.ID), Name: strings.TrimSpace(item.Name), Language: strings.TrimSpace(item.Language),
			Category: strings.ToUpper(strings.TrimSpace(item.Category)), Status: strings.ToUpper(strings.TrimSpace(item.Status)),
			QualitySignal: decodeQualitySignal(item.QualityScore), Components: append([]byte(nil), item.Components...),
			ComponentHash: hash, LastSyncedAt: syncedAt.UTC(),
		})
	}
	return out, nil
}
