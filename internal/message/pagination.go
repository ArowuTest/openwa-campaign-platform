package message

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

var ErrInvalidMessageVersionCursor = errors.New("invalid message-version pagination cursor")

type VersionPage struct {
	Items      []Version `json:"items"`
	NextCursor string    `json:"nextCursor,omitempty"`
}

type versionCursor struct {
	Version int `json:"version"`
}

type versionPageRepository interface {
	ListByCampaignPage(context.Context, string, int, int) ([]Version, error)
}

func decodeMessageVersionCursor(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 256 {
		return 0, ErrInvalidMessageVersionCursor
	}
	var cursor versionCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return 0, ErrInvalidMessageVersionCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.Version <= 0 {
		return 0, ErrInvalidMessageVersionCursor
	}
	return cursor.Version, nil
}

func encodeMessageVersionCursor(version int) (string, error) {
	raw, err := json.Marshal(versionCursor{Version: version})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Service) ListByCampaignPage(ctx context.Context, campaignID string, limit int, cursor string) (VersionPage, error) {
	if s == nil || s.repository == nil {
		return VersionPage{}, errors.New("message repository is required")
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return VersionPage{}, errors.New("campaign ID is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	repository, ok := s.repository.(versionPageRepository)
	if !ok {
		return VersionPage{}, errors.New("message-version pagination is unavailable")
	}
	beforeVersion, err := decodeMessageVersionCursor(cursor)
	if err != nil {
		return VersionPage{}, err
	}
	items, err := repository.ListByCampaignPage(ctx, campaignID, limit+1, beforeVersion)
	if err != nil {
		return VersionPage{}, err
	}
	page := VersionPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	page.NextCursor, err = encodeMessageVersionCursor(page.Items[len(page.Items)-1].Version)
	return page, err
}
