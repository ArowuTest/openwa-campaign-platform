package operations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var (
	ErrInvalidIncidentCursor          = errors.New("invalid incident pagination cursor")
	ErrInvalidDeliveryExceptionCursor = errors.New("invalid delivery-exception pagination cursor")
)

type IncidentPage struct {
	Items      []Incident `json:"items"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

type DeliveryExceptionPage struct {
	Items      []DeliveryException `json:"items"`
	NextCursor string              `json:"nextCursor,omitempty"`
}

type incidentCursor struct {
	SeverityRank int       `json:"severityRank"`
	CreatedAt    time.Time `json:"createdAt"`
	ID           string    `json:"id"`
}

type deliveryExceptionCursor struct {
	UpdatedAt time.Time `json:"updatedAt"`
	ID        string    `json:"id"`
}

type incidentPageRepository interface {
	ListIncidentPage(context.Context, IncidentStatus, int, int, *time.Time, string) ([]Incident, error)
}

type deliveryExceptionPageRepository interface {
	ListExceptionPage(context.Context, string, int, *time.Time, string) ([]DeliveryException, error)
}

func incidentSeverityRank(value Severity) int {
	switch value {
	case SeverityCritical:
		return 1
	case SeverityWarning:
		return 2
	default:
		return 3
	}
}

func encodeOperationsCursor(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeOperationsCursor(value string, target any, invalid error) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return invalid
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return invalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return invalid
	}
	return nil
}

func (s *Service) ListIncidentsPage(ctx context.Context, status IncidentStatus, limit int, cursor string) (IncidentPage, error) {
	if s == nil || s.Repo == nil {
		return IncidentPage{}, errors.New("operations repository is required")
	}
	repository, ok := s.Repo.(incidentPageRepository)
	if !ok {
		return IncidentPage{}, errors.New("incident pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var decoded incidentCursor
	var before *time.Time
	if strings.TrimSpace(cursor) != "" {
		if err := decodeOperationsCursor(cursor, &decoded, ErrInvalidIncidentCursor); err != nil || decoded.SeverityRank < 1 || decoded.SeverityRank > 3 || decoded.CreatedAt.IsZero() || strings.TrimSpace(decoded.ID) == "" {
			return IncidentPage{}, ErrInvalidIncidentCursor
		}
		value := decoded.CreatedAt.UTC()
		before = &value
	}
	items, err := repository.ListIncidentPage(ctx, status, limit+1, decoded.SeverityRank, before, strings.TrimSpace(decoded.ID))
	if err != nil {
		return IncidentPage{}, err
	}
	page := IncidentPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeOperationsCursor(incidentCursor{SeverityRank: incidentSeverityRank(last.Severity), CreatedAt: last.CreatedAt.UTC(), ID: last.ID})
	return page, err
}

func (s *Service) ListExceptionsPage(ctx context.Context, campaignID string, limit int, cursor string) (DeliveryExceptionPage, error) {
	if s == nil || s.Repo == nil {
		return DeliveryExceptionPage{}, errors.New("operations repository is required")
	}
	repository, ok := s.Repo.(deliveryExceptionPageRepository)
	if !ok {
		return DeliveryExceptionPage{}, errors.New("delivery-exception pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var decoded deliveryExceptionCursor
	var before *time.Time
	if strings.TrimSpace(cursor) != "" {
		if err := decodeOperationsCursor(cursor, &decoded, ErrInvalidDeliveryExceptionCursor); err != nil || decoded.UpdatedAt.IsZero() || strings.TrimSpace(decoded.ID) == "" {
			return DeliveryExceptionPage{}, ErrInvalidDeliveryExceptionCursor
		}
		value := decoded.UpdatedAt.UTC()
		before = &value
	}
	items, err := repository.ListExceptionPage(ctx, strings.TrimSpace(campaignID), limit+1, before, strings.TrimSpace(decoded.ID))
	if err != nil {
		return DeliveryExceptionPage{}, err
	}
	page := DeliveryExceptionPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeOperationsCursor(deliveryExceptionCursor{UpdatedAt: last.UpdatedAt.UTC(), ID: last.RecipientID})
	return page, err
}
