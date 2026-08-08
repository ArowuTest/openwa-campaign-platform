package platformpolicy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidListCursor = errors.New("invalid platform-policy list pagination cursor")

type ConfigurationPage struct {
	Items      []Configuration `json:"items"`
	NextCursor string          `json:"nextCursor,omitempty"`
}

type MaintenancePage struct {
	Items      []MaintenanceWindow `json:"items"`
	NextCursor string              `json:"nextCursor,omitempty"`
}

type listCursor struct {
	UpdatedAt time.Time `json:"updatedAt"`
	ID        string    `json:"id"`
}

type configurationListPageStore interface {
	ListConfigurationPage(context.Context, ConfigurationQuery, *time.Time, string) ([]Configuration, error)
}

type maintenanceListPageStore interface {
	ListMaintenancePage(context.Context, MaintenanceStatus, int, *time.Time, string) ([]MaintenanceWindow, error)
}

func decodeListCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidListCursor
	}
	var cursor listCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidListCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.UpdatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidListCursor
	}
	at := cursor.UpdatedAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

func encodeListCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(listCursor{UpdatedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (a *ConfigurationAdministration) ListPage(ctx context.Context, query ConfigurationQuery, cursor string) (ConfigurationPage, error) {
	if a == nil || a.Store == nil {
		return ConfigurationPage{}, errors.New("configuration store is required")
	}
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	store, ok := a.Store.(configurationListPageStore)
	if !ok {
		return ConfigurationPage{}, errors.New("configuration pagination is unavailable")
	}
	before, beforeID, err := decodeListCursor(cursor)
	if err != nil {
		return ConfigurationPage{}, err
	}
	query.Limit = limit + 1
	items, err := store.ListConfigurationPage(ctx, query, before, beforeID)
	if err != nil {
		return ConfigurationPage{}, err
	}
	page := ConfigurationPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeListCursor(last.UpdatedAt, last.ID)
	return page, err
}

func (a *MaintenanceAdministration) ListPage(ctx context.Context, status MaintenanceStatus, limit int, cursor string) (MaintenancePage, error) {
	if a == nil || a.Store == nil {
		return MaintenancePage{}, errors.New("maintenance store is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	store, ok := a.Store.(maintenanceListPageStore)
	if !ok {
		return MaintenancePage{}, errors.New("maintenance pagination is unavailable")
	}
	before, beforeID, err := decodeListCursor(cursor)
	if err != nil {
		return MaintenancePage{}, err
	}
	items, err := store.ListMaintenancePage(ctx, status, limit+1, before, beforeID)
	if err != nil {
		return MaintenancePage{}, err
	}
	page := MaintenancePage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeListCursor(last.UpdatedAt, last.ID)
	return page, err
}
