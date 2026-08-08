package segment

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

var ErrInvalidVersionCursor = errors.New("invalid segment-version pagination cursor")

type DefinitionVersionPage struct {
	Items      []DefinitionVersion `json:"items"`
	NextCursor string              `json:"nextCursor,omitempty"`
}

type definitionVersionCursor struct {
	Version int64 `json:"version"`
}

type definitionVersionPageRepository interface {
	VersionsPage(context.Context, string, int, int64) ([]DefinitionVersion, error)
}

func decodeVersionCursor(value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 256 {
		return 0, ErrInvalidVersionCursor
	}
	var cursor definitionVersionCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return 0, ErrInvalidVersionCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.Version <= 0 {
		return 0, ErrInvalidVersionCursor
	}
	return cursor.Version, nil
}

func encodeVersionCursor(version int64) (string, error) {
	raw, err := json.Marshal(definitionVersionCursor{Version: version})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *DefinitionService) VersionsPage(ctx context.Context, id string, limit int, cursor string) (DefinitionVersionPage, error) {
	if s == nil || s.Repository == nil {
		return DefinitionVersionPage{}, errors.New("segment definition repository is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return DefinitionVersionPage{}, ErrDefinitionNotFound
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	repository, ok := s.Repository.(definitionVersionPageRepository)
	if !ok {
		return DefinitionVersionPage{}, errors.New("segment-version pagination is unavailable")
	}
	beforeVersion, err := decodeVersionCursor(cursor)
	if err != nil {
		return DefinitionVersionPage{}, err
	}
	items, err := repository.VersionsPage(ctx, id, limit+1, beforeVersion)
	if err != nil {
		return DefinitionVersionPage{}, err
	}
	page := DefinitionVersionPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	page.NextCursor, err = encodeVersionCursor(page.Items[len(page.Items)-1].Version)
	return page, err
}
