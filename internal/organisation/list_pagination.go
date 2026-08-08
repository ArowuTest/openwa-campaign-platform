package organisation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidOrganisationCursor = errors.New("invalid organisation pagination cursor")

type OrganisationPage struct {
	Items      []Organisation `json:"items"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

type organisationCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type organisationPageRepository interface {
	ListPage(context.Context, int, *time.Time, string) ([]Organisation, error)
}

func decodeOrganisationCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidOrganisationCursor
	}
	var cursor organisationCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidOrganisationCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidOrganisationCursor
	}
	at := cursor.CreatedAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

func encodeOrganisationCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(organisationCursor{CreatedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Service) ListPage(ctx context.Context, limit int, cursor string) (OrganisationPage, error) {
	if s == nil || s.repository == nil {
		return OrganisationPage{}, errors.New("organisation repository is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	repository, ok := s.repository.(organisationPageRepository)
	if !ok {
		return OrganisationPage{}, errors.New("organisation pagination is unavailable")
	}
	before, beforeID, err := decodeOrganisationCursor(cursor)
	if err != nil {
		return OrganisationPage{}, err
	}
	items, err := repository.ListPage(ctx, limit+1, before, beforeID)
	if err != nil {
		return OrganisationPage{}, err
	}
	page := OrganisationPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeOrganisationCursor(last.CreatedAt, last.ID)
	return page, err
}
