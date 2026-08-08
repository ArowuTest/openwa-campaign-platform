package testmessage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidSendCursor = errors.New("invalid test-send pagination cursor")

type SendPage struct {
	Items      []Send `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type sendCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type sendPageRepository interface {
	ListSendPage(context.Context, string, int, *time.Time, string) ([]Send, error)
}

func decodeSendCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidSendCursor
	}
	var cursor sendCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidSendCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidSendCursor
	}
	at := cursor.CreatedAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

func encodeSendCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(sendCursor{CreatedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Service) ListSendsPage(ctx context.Context, campaignID string, limit int, cursor string) (SendPage, error) {
	if s == nil || s.Repository == nil {
		return SendPage{}, errors.New("test-message repository is required")
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return SendPage{}, errors.New("campaign ID is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	repository, ok := s.Repository.(sendPageRepository)
	if !ok {
		return SendPage{}, errors.New("test-send pagination is unavailable")
	}
	before, beforeID, err := decodeSendCursor(cursor)
	if err != nil {
		return SendPage{}, err
	}
	items, err := repository.ListSendPage(ctx, campaignID, limit+1, before, beforeID)
	if err != nil {
		return SendPage{}, err
	}
	page := SendPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeSendCursor(last.CreatedAt, last.ID)
	return page, err
}
