package importer

import (
	"encoding/csv"
	"errors"
	"io"
	"strconv"
	"strings"
)

// WriteIssueCSV emits a masked, spreadsheet-safe validation report. Prefixing
// formula-like cells with an apostrophe prevents CSV formula execution when an
// operator opens the report in spreadsheet software.
func WriteIssueCSV(writer io.Writer, issues []RowIssue) error {
	if writer == nil {
		return errors.New("issue export writer is required")
	}
	csvWriter := csv.NewWriter(writer)
	if err := csvWriter.Write([]string{"row_number", "field", "issue_code", "issue_message"}); err != nil {
		return err
	}
	for _, issue := range issues {
		record := []string{
			strconv.Itoa(issue.RowNumber),
			neutralizeSpreadsheetCell(issue.Field),
			neutralizeSpreadsheetCell(issue.Code),
			neutralizeSpreadsheetCell(issue.Message),
		}
		if err := csvWriter.Write(record); err != nil {
			return err
		}
	}
	csvWriter.Flush()
	return csvWriter.Error()
}

func neutralizeSpreadsheetCell(value string) string {
	trimmedLeft := strings.TrimLeft(value, " \t\r\n")
	if trimmedLeft == "" {
		return value
	}
	switch trimmedLeft[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	default:
		return value
	}
}
