package sender

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

var ErrInvalidInventoryCursor = errors.New("invalid sender inventory pagination cursor")

type PoolPage struct {
	Items      []Pool `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}
type NodePage struct {
	Items      []Node `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}
type GatewayPoolPage struct {
	Items      []GatewayPool `json:"items"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

type inventoryCursor struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}
type poolPageStore interface {
	ListPoolPage(context.Context, int, string, string) ([]Pool, error)
}
type nodePageStore interface {
	ListNodePage(context.Context, int, string, string) ([]Node, error)
}
type gatewayPoolPageStore interface {
	ListGatewayPoolPage(context.Context, int, string, string) ([]GatewayPool, error)
}

func decodeInventoryCursor(value string) (string, string, error) {
	if strings.TrimSpace(value) == "" {
		return "", "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return "", "", ErrInvalidInventoryCursor
	}
	var cursor inventoryCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return "", "", ErrInvalidInventoryCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || strings.TrimSpace(cursor.Name) == "" || strings.TrimSpace(cursor.ID) == "" {
		return "", "", ErrInvalidInventoryCursor
	}
	return strings.TrimSpace(cursor.Name), strings.TrimSpace(cursor.ID), nil
}

func encodeInventoryCursor(name, id string) (string, error) {
	raw, err := json.Marshal(inventoryCursor{Name: strings.TrimSpace(name), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *GovernanceService) ListPoolsPage(ctx context.Context, limit int, cursor string) (PoolPage, error) {
	if s == nil || s.Store == nil {
		return PoolPage{}, errors.New("sender governance store is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	store, ok := s.Store.(poolPageStore)
	if !ok {
		return PoolPage{}, errors.New("sender-pool pagination is unavailable")
	}
	name, id, err := decodeInventoryCursor(cursor)
	if err != nil {
		return PoolPage{}, err
	}
	items, err := store.ListPoolPage(ctx, limit+1, name, id)
	if err != nil {
		return PoolPage{}, err
	}
	page := PoolPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeInventoryCursor(last.Name, last.ID)
	return page, err
}

func (s *GovernanceService) ListNodesPage(ctx context.Context, limit int, cursor string) (NodePage, error) {
	if s == nil || s.Store == nil {
		return NodePage{}, errors.New("sender governance store is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	store, ok := s.Store.(nodePageStore)
	if !ok {
		return NodePage{}, errors.New("sender-node pagination is unavailable")
	}
	name, id, err := decodeInventoryCursor(cursor)
	if err != nil {
		return NodePage{}, err
	}
	items, err := store.ListNodePage(ctx, limit+1, name, id)
	if err != nil {
		return NodePage{}, err
	}
	page := NodePage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeInventoryCursor(last.Name, last.ID)
	return page, err
}

func (a *GatewayPoolAdministration) ListPage(ctx context.Context, limit int, cursor string) (GatewayPoolPage, error) {
	if a == nil || a.Store == nil {
		return GatewayPoolPage{}, errors.New("gateway pool governance store is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	store, ok := a.Store.(gatewayPoolPageStore)
	if !ok {
		return GatewayPoolPage{}, errors.New("gateway-pool pagination is unavailable")
	}
	name, id, err := decodeInventoryCursor(cursor)
	if err != nil {
		return GatewayPoolPage{}, err
	}
	items, err := store.ListGatewayPoolPage(ctx, limit+1, name, id)
	if err != nil {
		return GatewayPoolPage{}, err
	}
	page := GatewayPoolPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeInventoryCursor(last.Name, last.ID)
	return page, err
}
