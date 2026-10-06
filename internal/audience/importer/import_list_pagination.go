package importer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidImportListCursor = errors.New("invalid audience-import pagination cursor")
	importListUUIDPattern      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
)

type ImportListItem struct {
	ID               string        `json:"id"`
	OrganisationID   string        `json:"organisationId"`
	ConsentReviewID  string        `json:"consentReviewId"`
	PurposeID        string        `json:"purposeId"`
	SourceName       string        `json:"sourceName"`
	SourceSystem     string        `json:"sourceSystem,omitempty"`
	OriginalFilename string        `json:"originalFilename"`
	ByteSize         int64         `json:"byteSize"`
	Status           ImportStatus  `json:"status"`
	MalwareStatus    MalwareStatus `json:"malwareStatus"`
	UploadedRows     int64         `json:"uploadedRows"`
	ValidRows        int64         `json:"validRows"`
	InvalidRows      int64         `json:"invalidRows"`
	DuplicateRows    int64         `json:"duplicateRows"`
	SuppressedRows   int64         `json:"suppressedRows"`
	InsertedContacts int64         `json:"insertedContacts"`
	UpdatedContacts  int64         `json:"updatedContacts"`
	FailureReason    string        `json:"failureReason,omitempty"`
	CreatedAt        time.Time     `json:"createdAt"`
	UpdatedAt        time.Time     `json:"updatedAt"`
}

type ImportPage struct {
	Items      []ImportListItem `json:"items"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

type importListCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type importListPageRepository interface {
	ListImportPage(context.Context, string, int, *time.Time, string) ([]ImportBatch, error)
}

func summarizeImport(batch ImportBatch) ImportListItem {
	return ImportListItem{
		ID:               batch.ID,
		OrganisationID:   batch.OrganisationID,
		ConsentReviewID:  batch.ConsentReviewID,
		PurposeID:        batch.PurposeID,
		SourceName:       batch.SourceName,
		SourceSystem:     batch.SourceSystem,
		OriginalFilename: batch.OriginalFilename,
		ByteSize:         batch.ByteSize,
		Status:           batch.Status,
		MalwareStatus:    batch.MalwareStatus,
		UploadedRows:     batch.UploadedRows,
		ValidRows:        batch.ValidRows,
		InvalidRows:      batch.InvalidRows,
		DuplicateRows:    batch.DuplicateRows,
		SuppressedRows:   batch.SuppressedRows,
		InsertedContacts: batch.InsertedContacts,
		UpdatedContacts:  batch.UpdatedContacts,
		FailureReason:    batch.FailureReason,
		CreatedAt:        batch.CreatedAt,
		UpdatedAt:        batch.UpdatedAt,
	}
}

func (s *ImportService) ListPage(ctx context.Context, organisationID string, limit int, cursor string) (ImportPage, error) {
	if s == nil || s.Repository == nil {
		return ImportPage{}, errors.New("import repository is required")
	}
	repository, ok := s.Repository.(importListPageRepository)
	if !ok {
		return ImportPage{}, errors.New("audience-import pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	before, beforeID, err := decodeImportListCursor(cursor)
	if err != nil {
		return ImportPage{}, err
	}
	batches, err := repository.ListImportPage(ctx, strings.TrimSpace(organisationID), limit+1, before, beforeID)
	if err != nil {
		return ImportPage{}, err
	}
	page := ImportPage{Items: make([]ImportListItem, 0, min(limit, len(batches)))}
	if len(batches) > limit {
		batches = batches[:limit]
		last := batches[len(batches)-1]
		page.NextCursor, err = encodeImportListCursor(last.CreatedAt, last.ID)
		if err != nil {
			return ImportPage{}, err
		}
	}
	for _, batch := range batches {
		page.Items = append(page.Items, summarizeImport(batch))
	}
	return page, nil
}

func encodeImportListCursor(createdAt time.Time, identifier string) (string, error) {
	raw, err := json.Marshal(importListCursor{CreatedAt: createdAt.UTC(), ID: strings.TrimSpace(identifier)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeImportListCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidImportListCursor
	}
	var cursor importListCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidImportListCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) ||
		cursor.CreatedAt.IsZero() || !importListUUIDPattern.MatchString(strings.TrimSpace(cursor.ID)) {
		return nil, "", ErrInvalidImportListCursor
	}
	at := cursor.CreatedAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

func (r *MemoryImportRepository) ListImportPage(_ context.Context, organisationID string, limit int, before *time.Time, beforeID string) ([]ImportBatch, error) {
	if r == nil {
		return nil, errors.New("import repository is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]ImportBatch, 0, len(r.items))
	for _, batch := range r.items {
		if organisationID != "" && batch.OrganisationID != organisationID {
			continue
		}
		if before != nil {
			if batch.CreatedAt.After(*before) {
				continue
			}
			if batch.CreatedAt.Equal(*before) && strings.Compare(batch.ID, beforeID) >= 0 {
				continue
			}
		}
		items = append(items, cloneImport(batch))
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
