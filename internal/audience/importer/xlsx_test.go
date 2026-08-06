package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"testing"

	sharedcrypto "campaign-platform/internal/shared/crypto"
)

func buildXLSX(t *testing.T, sheets []struct{ name, state, xml string }) []byte {
	t.Helper()
	var out bytes.Buffer
	archive := zip.NewWriter(&out)
	write := func(name, value string) {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	write("[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/></Types>`)
	var workbook bytes.Buffer
	workbook.WriteString(`<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	var rels bytes.Buffer
	rels.WriteString(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for index, sheet := range sheets {
		state := ""
		if sheet.state != "" {
			state = fmt.Sprintf(` state="%s"`, sheet.state)
		}
		fmt.Fprintf(&workbook, `<sheet name="%s" sheetId="%d" r:id="rId%d"%s/>`, sheet.name, index+1, index+1, state)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, index+1, index+1)
		write(fmt.Sprintf("xl/worksheets/sheet%d.xml", index+1), sheet.xml)
	}
	workbook.WriteString(`</sheets></workbook>`)
	rels.WriteString(`</Relationships>`)
	write("xl/workbook.xml", workbook.String())
	write("xl/_rels/workbook.xml.rels", rels.String())
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func testSheet(formula bool) string {
	formulaXML := ""
	if formula {
		formulaXML = `<f>1+1</f>`
	}
	return `<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` +
		`<row r="1"><c r="A1" t="inlineStr"><is><t>msisdn</t></is></c><c r="B1" t="inlineStr"><is><t>country</t></is></c></row>` +
		`<row r="2"><c r="A2" t="inlineStr"><is><t>08012345678</t></is>` + formulaXML + `</c><c r="B2" t="inlineStr"><is><t>NG</t></is></c></row>` +
		`</sheetData></worksheet>`
}

func TestProcessXLSXStreamsSelectedWorksheet(t *testing.T) {
	payload := buildXLSX(t, []struct{ name, state, xml string }{{"Audience", "", testSheet(false)}})
	protector, _ := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32))
	var staged int
	result, err := ProcessXLSX(context.Background(), bytes.NewReader(payload), int64(len(payload)), "Audience", PreviewOptions{Mapping: ColumnMapping{MSISDN: "msisdn", Country: "country"}, Protector: protector}, func(_ context.Context, candidate ContactCandidate) (bool, error) {
		staged++
		if candidate.E164 != "" || len(candidate.EncryptedMSISDN) == 0 || len(candidate.LookupHMAC) == 0 {
			t.Fatal("candidate was not protected")
		}
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ValidRows != 1 || staged != 1 {
		t.Fatalf("result=%+v staged=%d", result, staged)
	}
}

func TestProcessXLSXRequiresSelectionAndRejectsHiddenOrFormula(t *testing.T) {
	protector, _ := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32))
	options := PreviewOptions{Mapping: ColumnMapping{MSISDN: "msisdn"}, Protector: protector}
	multiple := buildXLSX(t, []struct{ name, state, xml string }{{"One", "", testSheet(false)}, {"Two", "", testSheet(false)}})
	if _, err := ProcessXLSX(context.Background(), bytes.NewReader(multiple), int64(len(multiple)), "", options, func(context.Context, ContactCandidate) (bool, error) { return false, nil }); err == nil {
		t.Fatal("ambiguous worksheet accepted")
	}
	hidden := buildXLSX(t, []struct{ name, state, xml string }{{"One", "", testSheet(false)}, {"Secret", "hidden", testSheet(false)}})
	if _, err := ProcessXLSX(context.Background(), bytes.NewReader(hidden), int64(len(hidden)), "One", options, func(context.Context, ContactCandidate) (bool, error) { return false, nil }); err == nil {
		t.Fatal("hidden worksheet accepted")
	}
	formula := buildXLSX(t, []struct{ name, state, xml string }{{"One", "", testSheet(true)}})
	if _, err := ProcessXLSX(context.Background(), bytes.NewReader(formula), int64(len(formula)), "One", options, func(context.Context, ContactCandidate) (bool, error) { return false, nil }); err == nil {
		t.Fatal("formula cell accepted")
	}
}
