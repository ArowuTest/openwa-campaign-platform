package segment

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidDefinitionCursor = errors.New("invalid segment pagination cursor")

type DefinitionPage struct {
	Items      []Definition `json:"items"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

type definitionCursor struct {
	UpdatedAt time.Time `json:"updatedAt"`
	ID        string    `json:"id"`
}

type definitionPageRepository interface {
	ListPage(context.Context, string, int, *time.Time, string) ([]Definition, error)
}

func decodeDefinitionCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidDefinitionCursor
	}
	var cursor definitionCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidDefinitionCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.UpdatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidDefinitionCursor
	}
	at := cursor.UpdatedAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

func encodeDefinitionCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(definitionCursor{UpdatedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *DefinitionService) ListPage(ctx context.Context, organisationID string, limit int, cursor string) (DefinitionPage, error) {
	if s == nil || s.Repository == nil {
		return DefinitionPage{}, errors.New("segment definition repository is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	repository, ok := s.Repository.(definitionPageRepository)
	if !ok {
		return DefinitionPage{}, errors.New("segment pagination is unavailable")
	}
	before, beforeID, err := decodeDefinitionCursor(cursor)
	if err != nil {
		return DefinitionPage{}, err
	}
	items, err := repository.ListPage(ctx, strings.TrimSpace(organisationID), limit+1, before, beforeID)
	if err != nil {
		return DefinitionPage{}, err
	}
	page := DefinitionPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeDefinitionCursor(last.UpdatedAt, last.ID)
	return page, err
}
