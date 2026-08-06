package operations

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func TestStandaloneRenderers(t *testing.T) {
	r := CampaignReport{CampaignID: "c1", Name: "Campaign", Status: "COMPLETED", Audience: map[string]int64{"eligible": 10}, Delivery: map[string]int64{"delivered": 9}, Engagement: map[string]int64{}, Exceptions: map[string]int64{"failed": 1}, GeneratedAt: time.Now()}
	csvb, err := renderCSV(r)
	if err != nil || !bytes.Contains(csvb, []byte("delivered")) {
		t.Fatalf("csv: %v", err)
	}
	pdf, err := renderSimplePDF(r)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("pdf: %v", err)
	}
	xlsx, err := renderSimpleXLSX(r)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(xlsx), int64(len(xlsx)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range zr.File {
		if f.Name == "xl/worksheets/sheet1.xml" {
			rc, _ := f.Open()
			data, _ := io.ReadAll(rc)
			_ = rc.Close()
			found = bytes.Contains(data, []byte("delivered"))
		}
	}
	if !found {
		t.Fatal("xlsx sheet missing")
	}
}

func TestRenderCSVCampaignReportIncludesCommercialPoolsAndWarnings(t *testing.T) {
	report := CampaignReport{
		CampaignID:  "c1",
		Name:        "National campaign",
		Status:      "COMPLETED_WITH_EXCEPTIONS",
		Audience:    map[string]int64{"authorised": 2000000},
		Delivery:    map[string]int64{"delivered": 1900000},
		Engagement:  map[string]int64{"optOuts": 1500},
		Exceptions:  map[string]int64{"unknown": 25},
		Commercial:  CampaignCommercialReport{Status: "APPROVED", QuotationReference: "QUO-1", InvoiceReference: "INV-1", Currency: "NGN", ApprovedRecipients: 2000000, TotalAmountMinor: 100000000},
		Pools:       []CampaignPoolReport{{SenderPoolID: "p1", SenderPoolName: "Nigeria WWebJS A", GatewayPoolID: "g1", Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", ReservedMessagesPerMinute: 500, Recipients: map[string]int64{"delivered": 950000, "unknown": 10}}},
		Warnings:    []string{"UNKNOWN_OUTCOMES_REQUIRE_RECONCILIATION"},
		GeneratedAt: time.Now(),
	}
	payload, err := renderCSV(report)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, expected := range []string{"commercial,status,APPROVED", "commercial,quotationReference,QUO-1", "pool:Nigeria WWebJS A,engine,WHATSAPP_WEB_JS", "pool:Nigeria WWebJS A,reservedMessagesPerMinute,500", "warning,UNKNOWN_OUTCOMES_REQUIRE_RECONCILIATION,1"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected CSV to contain %q; got %s", expected, text)
		}
	}
}

func TestPrivacyPackageRendersCSVXLSXAndMultiPagePDF(t *testing.T) {
	value := map[string]any{
		"subject":   map[string]any{"maskedMsisdn": "+234***1234", "status": "ACTIVE"},
		"records":   []any{map[string]any{"type": "consent", "purpose": "events"}},
		"dangerous": "=HYPERLINK(\"https://example.invalid\")",
	}
	csvPayload, err := renderCSV(value)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(csvPayload, []byte("subject.maskedMsisdn")) || !bytes.Contains(csvPayload, []byte("'=HYPERLINK")) {
		t.Fatalf("generic CSV missing flattened or injection-safe values: %s", csvPayload)
	}
	xlsxPayload, err := renderSimpleXLSX(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zip.NewReader(bytes.NewReader(xlsxPayload), int64(len(xlsxPayload))); err != nil {
		t.Fatalf("invalid xlsx: %v", err)
	}
	long := map[string]any{"lines": strings.Repeat("abcdefghij", 1000)}
	pdfPayload, err := renderSimplePDFWithWatermark(long, "CONFIDENTIAL")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(pdfPayload, []byte("/Count 2")) && !bytes.Contains(pdfPayload, []byte("/Count 3")) {
		t.Fatalf("expected multi-page PDF, got %d bytes", len(pdfPayload))
	}
	if bytes.Contains(pdfPayload, []byte("report truncated")) {
		t.Fatal("PDF must not silently truncate report evidence")
	}
}
