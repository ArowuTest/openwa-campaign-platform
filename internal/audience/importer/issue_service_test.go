package importer

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestIssueExportPagesAndNeutralisesSpreadsheetFormulae(t *testing.T) {
	staging := NewMemoryStagingRepository()
	staging.issues["import-1"] = map[string]RowIssue{
		"1:A": {RowNumber: 1, Field: "msisdn", Code: "A", Message: "=HYPERLINK(\"https://bad\")"},
		"2:B": {RowNumber: 2, Field: "age", Code: "B", Message: "invalid"},
	}
	service := &IssueExportService{Repository: &MemoryIssueRepository{Staging: staging}, PageSize: 1, MaxRows: 10}
	var output bytes.Buffer
	if err := service.WriteCSV(context.Background(), "import-1", "actor", "request-1", &output); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "'=HYPERLINK") || !strings.Contains(text, "2,age,B,invalid") {
		t.Fatalf("unexpected issue export: %s", text)
	}
}
