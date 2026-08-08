package privacy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	sharedcrypto "campaign-platform/internal/shared/crypto"
)

var ErrInvalidLegalHoldListCursor = errors.New("invalid legal-hold pagination cursor")

type LegalHoldPage struct {
	Items      []LegalHold `json:"items"`
	NextCursor string      `json:"nextCursor,omitempty"`
}

type legalHoldListCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type legalHoldListPageRepository interface {
	ListLegalHoldPage(context.Context, []byte, bool, int, *time.Time, string) ([]LegalHold, error)
}

func decodeLegalHoldListCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidLegalHoldListCursor
	}
	var cursor legalHoldListCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidLegalHoldListCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidLegalHoldListCursor
	}
	at := cursor.CreatedAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

func encodeLegalHoldListCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(legalHoldListCursor{CreatedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Service) ListLegalHoldsPage(ctx context.Context, msisdn string, activeOnly bool, limit int, cursor string) (LegalHoldPage, error) {
	if s == nil || s.Repository == nil || s.Protector == nil {
		return LegalHoldPage{}, errors.New("privacy service is not configured")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	normalized, err := sharedcrypto.NormalizeE164(msisdn)
	if err != nil {
		return LegalHoldPage{}, err
	}
	repo, ok := s.Repository.(legalHoldListPageRepository)
	if !ok {
		return LegalHoldPage{}, errors.New("privacy legal-hold pagination is unavailable")
	}
	before, beforeID, err := decodeLegalHoldListCursor(cursor)
	if err != nil {
		return LegalHoldPage{}, err
	}
	items, err := repo.ListLegalHoldPage(ctx, s.Protector.LookupHMAC(normalized), activeOnly, limit+1, before, beforeID)
	if err != nil {
		return LegalHoldPage{}, err
	}
	page := LegalHoldPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeLegalHoldListCursor(last.CreatedAt, last.ID)
	return page, err
}
