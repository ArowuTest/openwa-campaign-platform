package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidAccountListCursor = errors.New("invalid internal-user pagination cursor")

type AccountPage struct {
	Items      []Account `json:"items"`
	NextCursor string    `json:"nextCursor,omitempty"`
}

type accountListCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type accountListPageRepository interface {
	ListAccountPage(context.Context, int, *time.Time, string) ([]Account, error)
}

func decodeAccountListCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidAccountListCursor
	}
	var cursor accountListCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidAccountListCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidAccountListCursor
	}
	at := cursor.CreatedAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

func encodeAccountListCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(accountListCursor{CreatedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *AdministrationService) ListPage(ctx context.Context, limit int, cursor string) (AccountPage, error) {
	if s == nil || s.repository == nil {
		return AccountPage{}, errors.New("identity administration repository is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	repo, ok := s.repository.(accountListPageRepository)
	if !ok {
		return AccountPage{}, errors.New("internal-user pagination is unavailable")
	}
	before, beforeID, err := decodeAccountListCursor(cursor)
	if err != nil {
		return AccountPage{}, err
	}
	items, err := repo.ListAccountPage(ctx, limit+1, before, beforeID)
	if err != nil {
		return AccountPage{}, err
	}
	page := AccountPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeAccountListCursor(last.CreatedAt, last.ID)
	return page, err
}
