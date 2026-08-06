package importer

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"

	"campaign-platform/internal/audit"
)

type IssueCursor struct {
	Row  int
	Code string
}

type IssuePage struct {
	Items []RowIssue
	Next  *IssueCursor
}

type IssueRepository interface {
	PageIssues(context.Context, string, IssueCursor, int) (IssuePage, error)
}

type IssueExportService struct {
	Repository IssueRepository
	Audit      *audit.Recorder
	PageSize   int
	MaxRows    int
}

func (s *IssueExportService) WriteCSV(ctx context.Context, importID, actor, correlation string, writer io.Writer) error {
	if s == nil || s.Repository == nil || writer == nil || strings.TrimSpace(importID) == "" {
		return errors.New("audience import issue export dependencies are required")
	}
	pageSize := s.PageSize
	if pageSize <= 0 || pageSize > 5000 {
		pageSize = 1000
	}
	maximum := s.MaxRows
	if maximum <= 0 || maximum > 2_000_000 {
		maximum = 100_000
	}
	csvWriter := csv.NewWriter(writer)
	if err := csvWriter.Write([]string{"row_number", "field", "issue_code", "issue_message"}); err != nil {
		return err
	}
	cursor := IssueCursor{}
	written := 0
	for {
		page, err := s.Repository.PageIssues(ctx, importID, cursor, pageSize)
		if err != nil {
			return err
		}
		for _, issue := range page.Items {
			written++
			if written > maximum {
				return errors.New("audience import issue export exceeds configured row limit")
			}
			if err := csvWriter.Write([]string{strconv.Itoa(issue.RowNumber), neutralizeSpreadsheetCell(issue.Field), neutralizeSpreadsheetCell(issue.Code), neutralizeSpreadsheetCell(issue.Message)}); err != nil {
				return err
			}
		}
		if page.Next == nil {
			break
		}
		cursor = *page.Next
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return err
	}
	if s.Audit != nil {
		_, err := s.Audit.Record(ctx, audit.Input{ActorType: "USER", ActorID: actor, Action: "AUDIENCE_IMPORT_ISSUES_EXPORTED", ObjectType: "AUDIENCE_IMPORT", ObjectID: importID, Outcome: "SUCCESS", Sensitivity: "HIGH", After: map[string]any{"rows": written, "format": "CSV"}, Reason: "authorised row-level validation issue export", CorrelationID: correlation})
		return err
	}
	return nil
}

type PostgreSQLIssueRepository struct{ DB *sql.DB }

func (r *PostgreSQLIssueRepository) PageIssues(ctx context.Context, importID string, after IssueCursor, limit int) (IssuePage, error) {
	if r == nil || r.DB == nil {
		return IssuePage{}, errors.New("database is required")
	}
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT row_number,coalesce(field_name,''),issue_code,issue_message FROM audience_import_issues WHERE audience_import_id=$1::uuid AND (row_number>$2 OR (row_number=$2 AND issue_code>$3)) ORDER BY row_number,issue_code LIMIT $4`, importID, after.Row, after.Code, limit+1)
	if err != nil {
		return IssuePage{}, err
	}
	defer rows.Close()
	items := make([]RowIssue, 0, limit+1)
	for rows.Next() {
		var item RowIssue
		if err := rows.Scan(&item.RowNumber, &item.Field, &item.Code, &item.Message); err != nil {
			return IssuePage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return IssuePage{}, err
	}
	page := IssuePage{}
	if len(items) > limit {
		last := items[limit-1]
		page.Next = &IssueCursor{Row: last.RowNumber, Code: last.Code}
		items = items[:limit]
	}
	page.Items = items
	return page, nil
}

type MemoryIssueRepository struct{ Staging *MemoryStagingRepository }

func (r *MemoryIssueRepository) PageIssues(_ context.Context, importID string, after IssueCursor, limit int) (IssuePage, error) {
	if r == nil || r.Staging == nil {
		return IssuePage{}, errors.New("memory issue repository is required")
	}
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	r.Staging.mu.Lock()
	defer r.Staging.mu.Unlock()
	items := make([]RowIssue, 0, len(r.Staging.issues[importID]))
	for _, item := range r.Staging.issues[importID] {
		if item.RowNumber > after.Row || item.RowNumber == after.Row && item.Code > after.Code {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].RowNumber != items[j].RowNumber {
			return items[i].RowNumber < items[j].RowNumber
		}
		return items[i].Code < items[j].Code
	})
	page := IssuePage{}
	if len(items) > limit {
		last := items[limit-1]
		page.Next = &IssueCursor{Row: last.RowNumber, Code: last.Code}
		items = items[:limit]
	}
	page.Items = items
	return page, nil
}
