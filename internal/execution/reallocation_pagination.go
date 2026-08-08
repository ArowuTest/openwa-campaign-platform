package execution

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidReallocationCursor = errors.New("invalid shard-reallocation pagination cursor")

type ReallocationPage struct {
	Items      []ShardReallocation `json:"items"`
	NextCursor string              `json:"nextCursor,omitempty"`
}

type reallocationCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type reallocationPageStore interface {
	ListShardReallocationPage(context.Context, string, int, *time.Time, string) ([]ShardReallocation, error)
}

func decodeReallocationCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidReallocationCursor
	}
	var cursor reallocationCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidReallocationCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidReallocationCursor
	}
	at := cursor.CreatedAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

func encodeReallocationCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(reallocationCursor{CreatedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *ReallocationAdministration) ListPage(ctx context.Context, shardID string, limit int, cursor string) (ReallocationPage, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(shardID) == "" {
		return ReallocationPage{}, ErrReallocationInvalid
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	store, ok := s.Store.(reallocationPageStore)
	if !ok {
		return ReallocationPage{}, errors.New("shard-reallocation pagination is unavailable")
	}
	after, afterID, err := decodeReallocationCursor(cursor)
	if err != nil {
		return ReallocationPage{}, err
	}
	items, err := store.ListShardReallocationPage(ctx, strings.TrimSpace(shardID), limit+1, after, afterID)
	if err != nil {
		return ReallocationPage{}, err
	}
	page := ReallocationPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeReallocationCursor(last.CreatedAt, last.ID)
	return page, err
}
