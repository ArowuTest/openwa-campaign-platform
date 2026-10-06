package consent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrInvalidPurposeListCursor = errors.New("invalid consent-purpose pagination cursor")

type Purpose struct {
	ID                       string    `json:"id"`
	OrganisationID           string    `json:"organisationId,omitempty"`
	ConsentReviewID          string    `json:"consentReviewId,omitempty"`
	Code                     string    `json:"code"`
	Name                     string    `json:"name"`
	Description              string    `json:"description,omitempty"`
	Channel                  string    `json:"channel"`
	WordingVersion           string    `json:"wordingVersion"`
	PermittedMessageCategory string    `json:"permittedMessageCategory,omitempty"`
	ExpiresAfterDays         *int      `json:"expiresAfterDays,omitempty"`
	Active                   bool      `json:"active"`
	CreatedAt                time.Time `json:"createdAt"`
	UpdatedAt                time.Time `json:"updatedAt"`
}

type PurposePage struct {
	Items      []Purpose `json:"items"`
	NextCursor string    `json:"nextCursor,omitempty"`
}

type PurposeRepository interface {
	ListPurposePage(context.Context, string, string, bool, int, *time.Time, string) ([]Purpose, error)
}

type PurposeService struct {
	Repository PurposeRepository
}

type purposeCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

func (s *PurposeService) ListPage(ctx context.Context, organisationID, reviewID string, activeOnly bool, limit int, cursor string) (PurposePage, error) {
	if s == nil || s.Repository == nil {
		return PurposePage{}, errors.New("consent purpose repository is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	before, beforeID, err := decodePurposeCursor(cursor)
	if err != nil {
		return PurposePage{}, err
	}
	items, err := s.Repository.ListPurposePage(
		ctx, strings.TrimSpace(organisationID), strings.TrimSpace(reviewID), activeOnly,
		limit+1, before, beforeID,
	)
	if err != nil {
		return PurposePage{}, err
	}
	page := PurposePage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodePurposeCursor(last.CreatedAt, last.ID)
	return page, err
}

func encodePurposeCursor(createdAt time.Time, id string) (string, error) {
	raw, err := json.Marshal(purposeCursor{CreatedAt: createdAt.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodePurposeCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidPurposeListCursor
	}
	var cursor purposeCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidPurposeListCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) ||
		cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidPurposeListCursor
	}
	at := cursor.CreatedAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

type MemoryPurposeRepository struct {
	mu    sync.RWMutex
	items map[string]Purpose
}

func NewMemoryPurposeRepository(values ...Purpose) *MemoryPurposeRepository {
	repository := &MemoryPurposeRepository{items: map[string]Purpose{}}
	for _, value := range values {
		repository.items[value.ID] = value
	}
	return repository
}

func (r *MemoryPurposeRepository) Put(value Purpose) {
	if r == nil || strings.TrimSpace(value.ID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[value.ID] = value
}

func (r *MemoryPurposeRepository) ListPurposePage(_ context.Context, organisationID, reviewID string, activeOnly bool, limit int, before *time.Time, beforeID string) ([]Purpose, error) {
	if r == nil {
		return nil, errors.New("consent purpose repository is required")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]Purpose, 0, len(r.items))
	for _, value := range r.items {
		if organisationID != "" && value.OrganisationID != organisationID {
			continue
		}
		if reviewID != "" && value.ConsentReviewID != reviewID {
			continue
		}
		if activeOnly && !value.Active {
			continue
		}
		if before != nil {
			if value.CreatedAt.After(*before) {
				continue
			}
			if value.CreatedAt.Equal(*before) && strings.Compare(value.ID, beforeID) >= 0 {
				continue
			}
		}
		items = append(items, value)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
