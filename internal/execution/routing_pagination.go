package execution

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

var ErrInvalidRoutingPlanCursor = errors.New("invalid routing-plan pagination cursor")

type RoutingPlanPage struct {
	Items      []RoutingPlan `json:"items"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

type routingPlanCursor struct {
	Version int64 `json:"version"`
}

type routingPlanPageStore interface {
	ListRoutingPlanPage(context.Context, string, int, int64) ([]RoutingPlan, error)
}

func decodeRoutingPlanCursor(value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 256 {
		return 0, ErrInvalidRoutingPlanCursor
	}
	var cursor routingPlanCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return 0, ErrInvalidRoutingPlanCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.Version <= 0 {
		return 0, ErrInvalidRoutingPlanCursor
	}
	return cursor.Version, nil
}

func encodeRoutingPlanCursor(version int64) (string, error) {
	raw, err := json.Marshal(routingPlanCursor{Version: version})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *RoutingAdministration) ListByCampaignPage(ctx context.Context, campaignID string, limit int, cursor string) (RoutingPlanPage, error) {
	if s == nil || s.Store == nil {
		return RoutingPlanPage{}, errors.New("routing-plan store is required")
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return RoutingPlanPage{}, ErrRoutingPlanInvalid
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	store, ok := s.Store.(routingPlanPageStore)
	if !ok {
		return RoutingPlanPage{}, errors.New("routing-plan pagination is unavailable")
	}
	beforeVersion, err := decodeRoutingPlanCursor(cursor)
	if err != nil {
		return RoutingPlanPage{}, err
	}
	items, err := store.ListRoutingPlanPage(ctx, campaignID, limit+1, beforeVersion)
	if err != nil {
		return RoutingPlanPage{}, err
	}
	page := RoutingPlanPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	page.NextCursor, err = encodeRoutingPlanCursor(page.Items[len(page.Items)-1].Version)
	return page, err
}
