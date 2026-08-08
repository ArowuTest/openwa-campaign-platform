package materialisation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidMaterialisationCursor = errors.New("invalid audience-materialisation pagination cursor")

type Page struct {
	Items      []MaterialisationJob `json:"items"`
	NextCursor string               `json:"nextCursor,omitempty"`
}

type pageCursor struct {
	RequestedAt time.Time `json:"requestedAt"`
	ID          string    `json:"id"`
}

type pageRepository interface {
	ListByCampaignPage(context.Context, string, int, *time.Time, string) ([]MaterialisationJob, error)
}

func decodePageCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidMaterialisationCursor
	}
	var cursor pageCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidMaterialisationCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.RequestedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidMaterialisationCursor
	}
	at := cursor.RequestedAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

func encodePageCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(pageCursor{RequestedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *MaterialisationService) ListPage(ctx context.Context, campaignID string, limit int, cursor string) (Page, error) {
	if s == nil || s.Repository == nil {
		return Page{}, errors.New("materialisation repository is required")
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return Page{}, errors.New("campaign ID is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	repository, ok := s.Repository.(pageRepository)
	if !ok {
		return Page{}, errors.New("audience-materialisation pagination is unavailable")
	}
	before, beforeID, err := decodePageCursor(cursor)
	if err != nil {
		return Page{}, err
	}
	items, err := repository.ListByCampaignPage(ctx, campaignID, limit+1, before, beforeID)
	if err != nil {
		return Page{}, err
	}
	for index := range items {
		items[index] = withProgress(items[index])
	}
	page := Page{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodePageCursor(last.RequestedAt, last.ID)
	return page, err
}
