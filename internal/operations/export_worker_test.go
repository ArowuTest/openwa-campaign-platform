package operations

import (
	"archive/zip"
	"bytes"
	"io"
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
