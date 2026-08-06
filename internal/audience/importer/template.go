package importer

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"html"
	"strings"
)

const CurrentTemplateVersion = "audience-v1"

var templateColumns = []string{"msisdn", "country", "state", "lga", "age", "gender"}

func RenderTemplate(version, format string) ([]byte, string, string, error) {
	version = strings.ToLower(strings.TrimSpace(version))
	format = strings.ToLower(strings.TrimSpace(format))
	if version == "" {
		version = CurrentTemplateVersion
	}
	if version != CurrentTemplateVersion {
		return nil, "", "", errors.New("unsupported audience import template version")
	}
	switch format {
	case "", "csv":
		var b bytes.Buffer
		w := csv.NewWriter(&b)
		if err := w.Write(templateColumns); err != nil {
			return nil, "", "", err
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return nil, "", "", err
		}
		return b.Bytes(), "text/csv; charset=utf-8", version + ".csv", nil
	case "xlsx":
		payload, err := renderTemplateXLSX()
		return payload, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", version + ".xlsx", err
	default:
		return nil, "", "", errors.New("template format must be csv or xlsx")
	}
}
func renderTemplateXLSX() ([]byte, error) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	var worksheet strings.Builder
	worksheet.WriteString(`<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1">`)
	for index, value := range templateColumns {
		worksheet.WriteString(fmt.Sprintf(`<c r="%c1" t="inlineStr"><is><t>%s</t></is></c>`, rune('A'+index), html.EscapeString(value)))
	}
	worksheet.WriteString(`</row></sheetData></worksheet>`)
	files := []struct {
		name    string
		content string
	}{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Audience" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`},
		{"xl/worksheets/sheet1.xml", worksheet.String()},
	}
	for _, file := range files {
		writer, err := archive.Create(file.name)
		if err != nil {
			_ = archive.Close()
			return nil, err
		}
		if _, err = writer.Write([]byte(file.content)); err != nil {
			_ = archive.Close()
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
