package sender

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrInvalidPaginationCursor = errors.New("invalid pagination cursor")

type SessionPage struct {
	Items      []GovernedSession `json:"items"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

type RuntimeEventPage struct {
	Items      []RuntimeEvent `json:"items"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

type sessionPageCursor struct {
	MaskedMSISDN string `json:"maskedMsisdn"`
	ID           string `json:"id"`
}
type runtimeEventPageCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	ID         string    `json:"id"`
}

type sessionPageStore interface {
	ListSessionPage(context.Context, int, string, string) ([]GovernedSession, error)
}

type runtimeEventPageStore interface {
	ListRuntimeEventPage(context.Context, string, int, *time.Time, string) ([]RuntimeEvent, error)
}

func normalisePageLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func encodeOpaqueCursor(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func decodeOpaqueCursor(raw string, target any) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) == 0 || len(decoded) > 1024 {
		return ErrInvalidPaginationCursor
	}
	decoder := json.NewDecoder(strings.NewReader(string(decoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return ErrInvalidPaginationCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return ErrInvalidPaginationCursor
	}
	return nil
}

func (s *GovernanceService) ListSessionsPage(ctx context.Context, limit int, cursor string) (SessionPage, error) {
	store, ok := s.Store.(sessionPageStore)
	if !ok {
		return SessionPage{}, errors.New("sender-session pagination is unavailable")
	}
	limit = normalisePageLimit(limit)
	var after sessionPageCursor
	if err := decodeOpaqueCursor(cursor, &after); err != nil {
		return SessionPage{}, err
	}
	if strings.TrimSpace(cursor) != "" && (strings.TrimSpace(after.MaskedMSISDN) == "" || strings.TrimSpace(after.ID) == "") {
		return SessionPage{}, ErrInvalidPaginationCursor
	}
	items, err := store.ListSessionPage(ctx, limit+1, after.MaskedMSISDN, after.ID)
	if err != nil {
		return SessionPage{}, err
	}
	page := SessionPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeOpaqueCursor(sessionPageCursor{MaskedMSISDN: last.MaskedMSISDN, ID: last.ID})
	return page, err
}

func (s *RuntimeRegistrationService) EventsPage(ctx context.Context, nodeID string, limit int, cursor string) (RuntimeEventPage, error) {
	store, ok := s.Store.(runtimeEventPageStore)
	if !ok {
		return RuntimeEventPage{}, errors.New("gateway runtime-event pagination is unavailable")
	}
	limit = normalisePageLimit(limit)
	var before runtimeEventPageCursor
	if err := decodeOpaqueCursor(cursor, &before); err != nil {
		return RuntimeEventPage{}, err
	}
	var beforeTime *time.Time
	if strings.TrimSpace(cursor) != "" {
		if before.OccurredAt.IsZero() || strings.TrimSpace(before.ID) == "" {
			return RuntimeEventPage{}, ErrInvalidPaginationCursor
		}
		value := before.OccurredAt.UTC()
		beforeTime = &value
	}
	items, err := store.ListRuntimeEventPage(ctx, strings.TrimSpace(nodeID), limit+1, beforeTime, before.ID)
	if err != nil {
		return RuntimeEventPage{}, err
	}
	page := RuntimeEventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeOpaqueCursor(runtimeEventPageCursor{OccurredAt: last.OccurredAt.UTC(), ID: last.ID})
	return page, err
}

type GatewayPoolEventPage struct {
	Items      []GatewayPoolEvent `json:"items"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

type gatewayPoolEventPageCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	ID         string    `json:"id"`
}

type gatewayPoolEventPageStore interface {
	ListGatewayPoolEventPage(context.Context, string, int, *time.Time, string) ([]GatewayPoolEvent, error)
}

func (a *GatewayPoolAdministration) EventsPage(ctx context.Context, poolID string, limit int, cursor string) (GatewayPoolEventPage, error) {
	if a == nil || a.Store == nil {
		return GatewayPoolEventPage{}, errors.New("gateway pool governance store is required")
	}
	store, ok := a.Store.(gatewayPoolEventPageStore)
	if !ok {
		return GatewayPoolEventPage{}, errors.New("gateway-pool event pagination is unavailable")
	}
	limit = normalisePageLimit(limit)
	var decoded gatewayPoolEventPageCursor
	var before *time.Time
	if strings.TrimSpace(cursor) != "" {
		if err := decodeOpaqueCursor(cursor, &decoded); err != nil || decoded.OccurredAt.IsZero() || strings.TrimSpace(decoded.ID) == "" {
			return GatewayPoolEventPage{}, ErrInvalidPaginationCursor
		}
		value := decoded.OccurredAt.UTC()
		before = &value
	}
	items, err := store.ListGatewayPoolEventPage(ctx, strings.TrimSpace(poolID), limit+1, before, strings.TrimSpace(decoded.ID))
	if err != nil {
		return GatewayPoolEventPage{}, err
	}
	page := GatewayPoolEventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeOpaqueCursor(gatewayPoolEventPageCursor{OccurredAt: last.OccurredAt.UTC(), ID: last.ID})
	return page, err
}
