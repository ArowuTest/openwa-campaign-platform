package sender

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidPacingListCursor = errors.New("invalid pacing-policy pagination cursor")

type PacingPolicyPage struct {
	Items      []PacingPolicy `json:"items"`
	NextCursor string         `json:"nextCursor,omitempty"`
}
type pacingListCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}
type pacingListPageStore interface {
	ListPacingPage(context.Context, int, *time.Time, string) ([]PacingPolicy, error)
}

func decodePacingListCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidPacingListCursor
	}
	var cursor pacingListCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidPacingListCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidPacingListCursor
	}
	at := cursor.CreatedAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}
func encodePacingListCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(pacingListCursor{CreatedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *PacingAdministration) ListPage(ctx context.Context, limit int, cursor string) (PacingPolicyPage, error) {
	if s == nil || s.Store == nil {
		return PacingPolicyPage{}, errors.New("pacing store is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	store, ok := s.Store.(pacingListPageStore)
	if !ok {
		return PacingPolicyPage{}, errors.New("pacing-policy pagination is unavailable")
	}
	before, beforeID, err := decodePacingListCursor(cursor)
	if err != nil {
		return PacingPolicyPage{}, err
	}
	items, err := store.ListPacingPage(ctx, limit+1, before, beforeID)
	if err != nil {
		return PacingPolicyPage{}, err
	}
	page := PacingPolicyPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodePacingListCursor(last.CreatedAt, last.ID)
	return page, err
}
