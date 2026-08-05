package importer

import (
	"archive/zip"
	"bytes"
	"os"
	"testing"
)

func temporaryFile(t *testing.T, payload []byte) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "object-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(payload); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

func TestInspectImportCSVUsesContentNotClaimedMIME(t *testing.T) {
	file := temporaryFile(t, []byte("msisdn,country\n08012345678,NG\n"))
	inspection, err := InspectImportFile("audience.csv", file, 32)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Format != "CSV" || inspection.MediaType != "text/csv" {
		t.Fatalf("unexpected inspection: %+v", inspection)
	}
	bad := temporaryFile(t, []byte{'P', 'K', 3, 4, 0, 0})
	if _, err := InspectImportFile("disguised.csv", bad, 6); err == nil {
		t.Fatal("binary content disguised as CSV was accepted")
	}
}

func TestInspectImportXLSXRequiresWorkbookAndRejectsMacro(t *testing.T) {
	build := func(entries []string) []byte {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		for _, name := range entries {
			entry, err := writer.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = entry.Write([]byte("<xml/>"))
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes()
	}
	valid := build([]string{"[Content_Types].xml", "xl/workbook.xml"})
	file := temporaryFile(t, valid)
	if _, err := InspectImportFile("audience.xlsx", file, int64(len(valid))); err != nil {
		t.Fatal(err)
	}
	macro := build([]string{"[Content_Types].xml", "xl/workbook.xml", "xl/vbaProject.bin"})
	file = temporaryFile(t, macro)
	if _, err := InspectImportFile("audience.xlsx", file, int64(len(macro))); err == nil {
		t.Fatal("macro-enabled XLSX content was accepted")
	}
}
