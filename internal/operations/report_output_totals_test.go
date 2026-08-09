package operations

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"
)

func TestCampaignReportPDFXLSXCSVCarrySameDeliveredTotal(t *testing.T) {
	report := CampaignReport{
		CampaignID: "campaign-output-totals",
		Name:       "Output totals",
		Status:     "COMPLETED",
		Audience:   map[string]int64{"authorised": 10},
		Delivery:   map[string]int64{"submitted": 10, "delivered": 9},
		Engagement: map[string]int64{},
		Exceptions: map[string]int64{"failed": 1},
	}
	csvPayload, err := renderCSV(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(csvPayload, []byte("delivery,delivered,9")) {
		t.Fatalf("CSV missing delivered total: %s", csvPayload)
	}

	pdfPayload, err := renderSimplePDF(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdfPayload, []byte("%PDF")) || !bytes.Contains(pdfPayload, []byte("delivered")) || !bytes.Contains(pdfPayload, []byte("9")) {
		t.Fatal("PDF missing delivered total")
	}
	xlsxPayload, err := renderSimpleXLSX(report)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(xlsxPayload), int64(len(xlsxPayload)))
	if err != nil {
		t.Fatal(err)
	}
	var sheet []byte
	for _, f := range zr.File {
		if f.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		rc, openErr := f.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		sheet, err = io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(sheet) == 0 || !bytes.Contains(sheet, []byte("delivered")) || !bytes.Contains(sheet, []byte(">9<")) {
		t.Fatalf("XLSX missing delivered total: %s", sheet)
	}
}
