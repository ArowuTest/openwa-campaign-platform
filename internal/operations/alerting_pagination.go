package operations

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidOperationalAlertCursor        = errors.New("invalid operational-alert pagination cursor")
	ErrInvalidOperationalNotificationCursor = errors.New("invalid operational-notification pagination cursor")
	ErrInvalidOperationalAlertEventCursor   = errors.New("invalid operational-alert-event pagination cursor")
)

type AlertPage struct {
	Items      []Alert `json:"items"`
	NextCursor string  `json:"nextCursor,omitempty"`
}

type NotificationPage struct {
	Items      []Notification `json:"items"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

type AlertEventPage struct {
	Items      []AlertEvent `json:"items"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

type operationalAlertCursor struct {
	LastObservedAt time.Time `json:"lastObservedAt"`
	ID             string    `json:"id"`
}

type operationalNotificationCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type operationalAlertEventCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	ID         string    `json:"id"`
}

type alertPageStore interface {
	ListAlertPage(context.Context, AlertStatus, Severity, int, *time.Time, string) ([]Alert, error)
}

type notificationPageStore interface {
	ListNotificationPage(context.Context, string, int, *time.Time, string) ([]Notification, error)
}

type alertEventPageStore interface {
	ListAlertEventPage(context.Context, string, int, *time.Time, string) ([]AlertEvent, error)
}

func (a *AlertAdministration) ListAlertsPage(ctx context.Context, status AlertStatus, severity Severity, limit int, cursor string) (AlertPage, error) {
	if a == nil || a.Store == nil {
		return AlertPage{}, errors.New("alert store is required")
	}
	store, ok := a.Store.(alertPageStore)
	if !ok {
		return AlertPage{}, errors.New("operational-alert pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var decoded operationalAlertCursor
	var before *time.Time
	if strings.TrimSpace(cursor) != "" {
		if err := decodeOperationsCursor(cursor, &decoded, ErrInvalidOperationalAlertCursor); err != nil || decoded.LastObservedAt.IsZero() || strings.TrimSpace(decoded.ID) == "" {
			return AlertPage{}, ErrInvalidOperationalAlertCursor
		}
		value := decoded.LastObservedAt.UTC()
		before = &value
	}
	items, err := store.ListAlertPage(ctx, status, severity, limit+1, before, strings.TrimSpace(decoded.ID))
	if err != nil {
		return AlertPage{}, err
	}
	page := AlertPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeOperationsCursor(operationalAlertCursor{LastObservedAt: last.LastObservedAt.UTC(), ID: last.ID})
	return page, err
}

func (a *AlertAdministration) NotificationsPage(ctx context.Context, status string, limit int, cursor string) (NotificationPage, error) {
	if a == nil || a.Store == nil {
		return NotificationPage{}, errors.New("alert store is required")
	}
	store, ok := a.Store.(notificationPageStore)
	if !ok {
		return NotificationPage{}, errors.New("operational-notification pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var decoded operationalNotificationCursor
	var before *time.Time
	if strings.TrimSpace(cursor) != "" {
		if err := decodeOperationsCursor(cursor, &decoded, ErrInvalidOperationalNotificationCursor); err != nil || decoded.CreatedAt.IsZero() || strings.TrimSpace(decoded.ID) == "" {
			return NotificationPage{}, ErrInvalidOperationalNotificationCursor
		}
		value := decoded.CreatedAt.UTC()
		before = &value
	}
	items, err := store.ListNotificationPage(ctx, strings.TrimSpace(status), limit+1, before, strings.TrimSpace(decoded.ID))
	if err != nil {
		return NotificationPage{}, err
	}
	page := NotificationPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeOperationsCursor(operationalNotificationCursor{CreatedAt: last.CreatedAt.UTC(), ID: last.ID})
	return page, err
}

func (a *AlertAdministration) AlertEventsPage(ctx context.Context, alertID string, limit int, cursor string) (AlertEventPage, error) {
	if a == nil || a.Store == nil {
		return AlertEventPage{}, errors.New("alert store is required")
	}
	alertID = strings.TrimSpace(alertID)
	if alertID == "" {
		return AlertEventPage{}, errors.New("alert id is required")
	}
	store, ok := a.Store.(alertEventPageStore)
	if !ok {
		return AlertEventPage{}, errors.New("operational-alert-event pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var decoded operationalAlertEventCursor
	var after *time.Time
	if strings.TrimSpace(cursor) != "" {
		if err := decodeOperationsCursor(cursor, &decoded, ErrInvalidOperationalAlertEventCursor); err != nil || decoded.OccurredAt.IsZero() || strings.TrimSpace(decoded.ID) == "" {
			return AlertEventPage{}, ErrInvalidOperationalAlertEventCursor
		}
		value := decoded.OccurredAt.UTC()
		after = &value
	}
	items, err := store.ListAlertEventPage(ctx, alertID, limit+1, after, strings.TrimSpace(decoded.ID))
	if err != nil {
		return AlertEventPage{}, err
	}
	page := AlertEventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeOperationsCursor(operationalAlertEventCursor{OccurredAt: last.OccurredAt.UTC(), ID: last.ID})
	return page, err
}
