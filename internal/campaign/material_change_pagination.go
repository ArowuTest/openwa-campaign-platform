package campaign

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

var ErrInvalidMaterialChangeCursor = errors.New("invalid campaign material-change pagination cursor")

type MaterialChangePage struct {
	Items      []MaterialChangeEvent `json:"items"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

type materialChangeCursor struct {
	Sequence int64 `json:"sequence"`
}

type materialChangePageRepository interface {
	ListMaterialChangePage(context.Context, string, int, int64) ([]MaterialChangeEvent, error)
}

func decodeMaterialChangeCursor(value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 256 {
		return 0, ErrInvalidMaterialChangeCursor
	}
	var cursor materialChangeCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return 0, ErrInvalidMaterialChangeCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.Sequence <= 0 {
		return 0, ErrInvalidMaterialChangeCursor
	}
	return cursor.Sequence, nil
}

func encodeMaterialChangeCursor(sequence int64) (string, error) {
	raw, err := json.Marshal(materialChangeCursor{Sequence: sequence})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Service) ListMaterialChangesPage(ctx context.Context, campaignID string, limit int, cursor string) (MaterialChangePage, error) {
	if s == nil || s.repository == nil {
		return MaterialChangePage{}, errors.New("campaign repository is required")
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return MaterialChangePage{}, ErrNotFound
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if _, err := s.repository.Get(ctx, campaignID); err != nil {
		return MaterialChangePage{}, err
	}
	repository, ok := s.repository.(materialChangePageRepository)
	if !ok {
		return MaterialChangePage{}, errors.New("campaign material-change pagination is unavailable")
	}
	afterSequence, err := decodeMaterialChangeCursor(cursor)
	if err != nil {
		return MaterialChangePage{}, err
	}
	items, err := repository.ListMaterialChangePage(ctx, campaignID, limit+1, afterSequence)
	if err != nil {
		return MaterialChangePage{}, err
	}
	page := MaterialChangePage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	page.NextCursor, err = encodeMaterialChangeCursor(page.Items[len(page.Items)-1].Sequence)
	return page, err
}
