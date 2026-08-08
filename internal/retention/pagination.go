package retention

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidRetentionJobCursor = errors.New("invalid retention-job pagination cursor")

type JobPage struct {
	Items      []Job  `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type retentionJobCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type retentionJobPageStore interface {
	ListJobPage(context.Context, JobStatus, int, *time.Time, string) ([]Job, error)
}

func (a *Administration) JobsPage(ctx context.Context, status JobStatus, limit int, cursor string) (JobPage, error) {
	if a == nil || a.Store == nil {
		return JobPage{}, errors.New("retention store is required")
	}
	store, ok := a.Store.(retentionJobPageStore)
	if !ok {
		return JobPage{}, errors.New("retention-job pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var before *time.Time
	var beforeID string
	if strings.TrimSpace(cursor) != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(raw) == 0 || len(raw) > 1024 {
			return JobPage{}, ErrInvalidRetentionJobCursor
		}
		var decoded retentionJobCursor
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&decoded); err != nil {
			return JobPage{}, ErrInvalidRetentionJobCursor
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || decoded.CreatedAt.IsZero() || strings.TrimSpace(decoded.ID) == "" {
			return JobPage{}, ErrInvalidRetentionJobCursor
		}
		value := decoded.CreatedAt.UTC()
		before = &value
		beforeID = strings.TrimSpace(decoded.ID)
	}
	items, err := store.ListJobPage(ctx, status, limit+1, before, beforeID)
	if err != nil {
		return JobPage{}, err
	}
	page := JobPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	raw, err := json.Marshal(retentionJobCursor{CreatedAt: last.CreatedAt.UTC(), ID: last.ID})
	if err != nil {
		return JobPage{}, err
	}
	page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	return page, nil
}
