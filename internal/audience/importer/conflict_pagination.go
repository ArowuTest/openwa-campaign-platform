package importer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidConflictCursor = errors.New("invalid audience-conflict pagination cursor")

type ConflictPage struct {
	Items      []ProfileConflict `json:"items"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

type conflictPageCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type conflictPageRepository interface {
	ListConflictPage(context.Context, string, ConflictStatus, int, *time.Time, string) ([]ProfileConflict, error)
}

func decodeConflictCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidConflictCursor
	}
	var cursor conflictPageCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidConflictCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidConflictCursor
	}
	at := cursor.CreatedAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

func encodeConflictCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(conflictPageCursor{CreatedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *ConflictService) ListPage(ctx context.Context, importID string, status ConflictStatus, limit int, cursor string) (ConflictPage, error) {
	if s == nil || s.Repository == nil {
		return ConflictPage{}, errors.New("conflict repository is required")
	}
	importID = strings.TrimSpace(importID)
	if importID == "" {
		return ConflictPage{}, errors.New("import ID is required")
	}
	if status == "" {
		status = ConflictPending
	}
	if status != ConflictPending && status != ConflictResolved && status != ConflictRejected {
		return ConflictPage{}, errors.New("invalid conflict status")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	repository, ok := s.Repository.(conflictPageRepository)
	if !ok {
		return ConflictPage{}, errors.New("audience-conflict pagination is unavailable")
	}
	after, afterID, err := decodeConflictCursor(cursor)
	if err != nil {
		return ConflictPage{}, err
	}
	items, err := repository.ListConflictPage(ctx, importID, status, limit+1, after, afterID)
	if err != nil {
		return ConflictPage{}, err
	}
	page := ConflictPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeConflictCursor(last.CreatedAt, last.ID)
	return page, err
}
