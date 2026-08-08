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

var ErrInvalidRecipientCursor = errors.New("invalid test-recipient pagination cursor")

type RecipientPage struct {
	Items      []Recipient `json:"items"`
	NextCursor string      `json:"nextCursor,omitempty"`
}

type recipientCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type recipientPageRepository interface {
	ListRecipientPage(context.Context, int, *time.Time, string) ([]Recipient, error)
}

func decodeRecipientCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidRecipientCursor
	}
	var cursor recipientCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidRecipientCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidRecipientCursor
	}
	at := cursor.CreatedAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

func encodeRecipientCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(recipientCursor{CreatedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Service) ListRecipientsPage(ctx context.Context, limit int, cursor string) (RecipientPage, error) {
	if s == nil || s.Repository == nil {
		return RecipientPage{}, errors.New("test-message repository is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	repository, ok := s.Repository.(recipientPageRepository)
	if !ok {
		return RecipientPage{}, errors.New("test-recipient pagination is unavailable")
	}
	before, beforeID, err := decodeRecipientCursor(cursor)
	if err != nil {
		return RecipientPage{}, err
	}
	items, err := repository.ListRecipientPage(ctx, limit+1, before, beforeID)
	if err != nil {
		return RecipientPage{}, err
	}
	page := RecipientPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeRecipientCursor(last.CreatedAt, last.ID)
	return page, err
}
