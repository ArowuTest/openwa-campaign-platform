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
)

type SenderVerification struct {
	PhoneNumberID      string `json:"phoneNumberId"`
	DisplayPhoneNumber string `json:"displayPhoneNumber"`
	VerifiedName       string `json:"verifiedName"`
	QualityRating      string `json:"qualityRating"`
}

type phoneNumberPage struct {
	Data []struct {
		ID                 string `json:"id"`
		DisplayPhoneNumber string `json:"display_phone_number"`
		VerifiedName       string `json:"verified_name"`
		QualityRating      string `json:"quality_rating"`
	} `json:"data"`
	Paging struct {
		Next string `json:"next"`
	} `json:"paging"`
}

func (c *Client) VerifySender(ctx context.Context, sender Sender) (SenderVerification, error) {
	if c == nil || c.Credentials == nil {
		return SenderVerification{}, APIError{Code: "META_CONFIGURATION_INVALID", Safety: SafetyPermanent, Err: errors.New("credential resolver is required")}
	}
	credential, err := c.Credentials.Resolve(strings.TrimSpace(sender.CredentialKey))
	if err != nil {
		return SenderVerification{}, APIError{Code: "META_CREDENTIAL_UNAVAILABLE", Safety: SafetyPermanent, Err: err}
	}
	version := strings.TrimSpace(sender.GraphAPIVersion)
	wabaID := strings.TrimSpace(sender.WABAID)
	phoneID := strings.TrimSpace(sender.PhoneNumberID)
	if !graphVersionPattern.MatchString(version) || wabaID == "" || phoneID == "" || strings.ContainsAny(wabaID+phoneID, "/?#\\") {
		return SenderVerification{}, APIError{Code: "META_REQUEST_INVALID", Safety: SafetyPermanent, Err: errors.New("graph version, WABA ID and phone number ID are required")}
	}
	base, err := c.messageBaseURL()
	if err != nil {
		return SenderVerification{}, APIError{Code: "META_CONFIGURATION_INVALID", Safety: SafetyPermanent, Err: err}
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + url.PathEscape(version) + "/" + url.PathEscape(wabaID) + "/phone_numbers"
	query := base.Query()
	query.Set("fields", "id,display_phone_number,verified_name,quality_rating")
	query.Set("limit", "100")
	base.RawQuery = query.Encode()
	originScheme, originHost, expectedPath := base.Scheme, base.Host, base.Path
	next := base.String()
	for page := 0; next != "" && page < 100; page++ {
		values, nextURL, fetchErr := c.fetchPhoneNumberPage(ctx, next, credential.AccessToken)
		if fetchErr != nil {
			return SenderVerification{}, fetchErr
		}
		for _, value := range values {
			if value.PhoneNumberID == phoneID {
				return value, nil
			}
		}
		if nextURL != "" {
			parsed, parseErr := url.Parse(nextURL)
			if parseErr != nil || parsed.Scheme != originScheme || parsed.Host != originHost || parsed.Path != expectedPath {
				return SenderVerification{}, APIError{Code: "META_PAGINATION_INVALID", Safety: SafetyPermanent, Err: errors.New("phone-number pagination left the governed endpoint")}
			}
		}
		next = nextURL
	}
	if next != "" {
		return SenderVerification{}, APIError{Code: "META_PHONE_NUMBER_PAGE_LIMIT", Safety: SafetyPermanent}
	}
	return SenderVerification{}, APIError{Code: "META_PHONE_NUMBER_MISMATCH", Safety: SafetyPermanent, Err: errors.New("configured phone number ID is not present in the governed WABA")}
}

func (c *Client) fetchPhoneNumberPage(ctx context.Context, target, accessToken string) ([]SenderVerification, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, "", APIError{Code: "META_REQUEST_INVALID", Safety: SafetyPermanent, Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := c.hardenedHTTPClient().Do(req)
	if err != nil {
		return nil, "", APIError{Code: "META_VERIFY_TRANSPORT_FAILED", Safety: SafetySafeToRetry, Err: err}
	}
	defer response.Body.Close()
	limit := c.MaximumResponseBytes
	if limit <= 0 || limit > 1<<20 {
		limit = 1 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, "", APIError{Code: "META_VERIFY_RESPONSE_READ_FAILED", Safety: SafetySafeToRetry, Err: err}
	}
	if int64(len(raw)) > limit {
		return nil, "", APIError{Code: "META_VERIFY_RESPONSE_TOO_LARGE", Safety: SafetyPermanent, Err: errors.New("provider response exceeds limit")}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		safety := SafetyPermanent
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			safety = SafetySafeToRetry
		}
		return nil, "", APIError{Code: fmt.Sprintf("META_VERIFY_HTTP_%d", response.StatusCode), Safety: safety, Detail: redactedMetaDetail(raw)}
	}
	var page phoneNumberPage
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, "", APIError{Code: "META_VERIFY_RESPONSE_INVALID", Safety: SafetySafeToRetry, Err: err}
	}
	out := make([]SenderVerification, 0, len(page.Data))
	for _, item := range page.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			return nil, "", APIError{Code: "META_VERIFY_RESPONSE_INVALID", Safety: SafetyPermanent, Err: errors.New("phone number entry omitted ID")}
		}
		out = append(out, SenderVerification{
			PhoneNumberID: id, DisplayPhoneNumber: strings.TrimSpace(item.DisplayPhoneNumber),
			VerifiedName: strings.TrimSpace(item.VerifiedName), QualityRating: strings.ToUpper(strings.TrimSpace(item.QualityRating)),
		})
	}
	return out, strings.TrimSpace(page.Paging.Next), nil
}
