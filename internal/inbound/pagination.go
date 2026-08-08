package inbound

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
	ErrInvalidReplyCursor    = errors.New("invalid inbound-reply pagination cursor")
	ErrInvalidRotationCursor = errors.New("invalid re-encryption-run pagination cursor")
)

type ReplyPage struct {
	Items      []Reply `json:"items"`
	NextCursor string  `json:"nextCursor,omitempty"`
}

type RotationPage struct {
	Items      []RotationRun `json:"items"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

type inboundCursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

type replyPageRepository interface {
	ListReplyPage(context.Context, int, *time.Time, string) ([]Reply, error)
}

type rotationPageRepository interface {
	ListRunPage(context.Context, int, *time.Time, string) ([]RotationRun, error)
}

func decodeInboundCursor(value string, invalid error) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", invalid
	}
	var cursor inboundCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", invalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.At.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", invalid
	}
	at := cursor.At.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

func encodeInboundCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(inboundCursor{At: at.UTC(), ID: id})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Service) ListPage(ctx context.Context, limit int, cursor string) (ReplyPage, error) {
	if s == nil || s.Repository == nil {
		return ReplyPage{}, errors.New("inbound repository is required")
	}
	repository, ok := s.Repository.(replyPageRepository)
	if !ok {
		return ReplyPage{}, errors.New("inbound-reply pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	before, beforeID, err := decodeInboundCursor(cursor, ErrInvalidReplyCursor)
	if err != nil {
		return ReplyPage{}, err
	}
	items, err := repository.ListReplyPage(ctx, limit+1, before, beforeID)
	if err != nil {
		return ReplyPage{}, err
	}
	page := ReplyPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeInboundCursor(last.CreatedAt, last.ID)
	return page, err
}

func (s *RotationService) ListPage(ctx context.Context, limit int, cursor string) (RotationPage, error) {
	if s == nil || s.Repository == nil {
		return RotationPage{}, errors.New("rotation repository is required")
	}
	repository, ok := s.Repository.(rotationPageRepository)
	if !ok {
		return RotationPage{}, errors.New("re-encryption-run pagination is unavailable")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	before, beforeID, err := decodeInboundCursor(cursor, ErrInvalidRotationCursor)
	if err != nil {
		return RotationPage{}, err
	}
	items, err := repository.ListRunPage(ctx, limit+1, before, beforeID)
	if err != nil {
		return RotationPage{}, err
	}
	page := RotationPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeInboundCursor(last.RequestedAt, last.ID)
	return page, err
}
