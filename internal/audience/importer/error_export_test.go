package importer

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
)

func TestIssueCSVNeutralizesSpreadsheetFormulas(t *testing.T) {
	var output bytes.Buffer
	issues := []RowIssue{{RowNumber: 2, Field: "=HYPERLINK(\"https://bad\")", Code: "+CMD", Message: "  @SUM(1,1)"}}
	if err := WriteIssueCSV(&output, issues); err != nil {
		t.Fatal(err)
	}
	reader := csv.NewReader(strings.NewReader(output.String()))
	_, _ = reader.Read()
	record, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	if record[1][0] != '\'' || record[2][0] != '\'' || record[3][0] != '\'' {
		t.Fatalf("formula-like cells were not neutralized: %#v", record)
	}
}
