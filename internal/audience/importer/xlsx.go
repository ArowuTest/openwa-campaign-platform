package importer

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	maxXLSXSheets        = 100
	maxXLSXColumns       = 500
	maxXLSXSharedStrings = 1_000_000
	maxXLSXSharedBytes   = 128 << 20
)

type workbookDocument struct {
	Sheets []workbookSheet `xml:"sheets>sheet"`
}

type workbookSheet struct {
	Name  string `xml:"name,attr"`
	State string `xml:"state,attr"`
	RID   string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
}

type relationshipsDocument struct {
	Items []relationship `xml:"Relationship"`
}

type relationship struct {
	ID     string `xml:"Id,attr"`
	Target string `xml:"Target,attr"`
	Type   string `xml:"Type,attr"`
}

type worksheetDocument struct {
	Rows []worksheetRow `xml:"sheetData>row"`
}

type worksheetRow struct {
	Number int             `xml:"r,attr"`
	Cells  []worksheetCell `xml:"c"`
}

type worksheetCell struct {
	Reference string          `xml:"r,attr"`
	Type      string          `xml:"t,attr"`
	Formula   *string         `xml:"f"`
	Value     string          `xml:"v"`
	Inline    inlineStringXML `xml:"is"`
}

type inlineStringXML struct {
	Text string    `xml:"t"`
	Runs []richRun `xml:"r"`
}

type richRun struct {
	Text string `xml:"t"`
}

func (v inlineStringXML) String() string {
	if v.Text != "" {
		return v.Text
	}
	var b strings.Builder
	for _, run := range v.Runs {
		b.WriteString(run.Text)
	}
	return b.String()
}

// ProcessXLSX converts one explicitly selected visible worksheet into the same
// protected row pipeline used by CSV. Formulas, hidden sheets and ambiguous
// worksheet selection fail closed.
func ProcessXLSX(ctx context.Context, reader io.ReaderAt, size int64, worksheet string, options PreviewOptions, consumer CandidateConsumer) (PreviewResult, error) {
	csvReader, err := xlsxCSVReader(ctx, reader, size, worksheet)
	if err != nil {
		return PreviewResult{}, err
	}
	defer csvReader.Close()
	return ProcessCSV(ctx, csvReader, options, consumer)
}

func PreviewXLSX(ctx context.Context, reader io.ReaderAt, size int64, worksheet string, options PreviewOptions) (PreviewResult, error) {
	csvReader, err := xlsxCSVReader(ctx, reader, size, worksheet)
	if err != nil {
		return PreviewResult{}, err
	}
	defer csvReader.Close()
	return scanCSV(ctx, csvReader, options, true, true, nil)
}

func xlsxCSVReader(ctx context.Context, reader io.ReaderAt, size int64, worksheet string) (io.ReadCloser, error) {
	if reader == nil || size <= 0 {
		return nil, errors.New("non-empty XLSX object is required")
	}
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return nil, fmt.Errorf("open XLSX archive: %w", err)
	}
	files := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		name := normaliseXLSXPath(file.Name)
		files[name] = file
	}
	workbookFile := files["xl/workbook.xml"]
	relsFile := files["xl/_rels/workbook.xml.rels"]
	if workbookFile == nil || relsFile == nil {
		return nil, errors.New("XLSX workbook relationships are missing")
	}
	var workbook workbookDocument
	if err := decodeBoundedXML(workbookFile, 8<<20, &workbook); err != nil {
		return nil, fmt.Errorf("decode workbook: %w", err)
	}
	if len(workbook.Sheets) == 0 || len(workbook.Sheets) > maxXLSXSheets {
		return nil, errors.New("XLSX worksheet count is outside permitted range")
	}
	var rels relationshipsDocument
	if err := decodeBoundedXML(relsFile, 8<<20, &rels); err != nil {
		return nil, fmt.Errorf("decode workbook relationships: %w", err)
	}
	targetByID := make(map[string]string, len(rels.Items))
	for _, rel := range rels.Items {
		if strings.Contains(strings.ToLower(rel.Type), "/worksheet") {
			targetByID[rel.ID] = normaliseXLSXPath("xl/" + strings.TrimPrefix(rel.Target, "/"))
		}
	}
	selectedName := strings.TrimSpace(worksheet)
	var selected workbookSheet
	visible := 0
	for _, sheet := range workbook.Sheets {
		state := strings.ToLower(strings.TrimSpace(sheet.State))
		if state == "hidden" || state == "veryhidden" {
			return nil, fmt.Errorf("XLSX contains hidden worksheet %q", sheet.Name)
		}
		visible++
		if selectedName != "" && strings.EqualFold(strings.TrimSpace(sheet.Name), selectedName) {
			selected = sheet
		}
	}
	if selectedName == "" {
		if visible != 1 {
			return nil, errors.New("XLSX contains multiple visible worksheets; worksheet selection is required")
		}
		selected = workbook.Sheets[0]
	}
	if strings.TrimSpace(selected.Name) == "" {
		return nil, fmt.Errorf("worksheet %q was not found", selectedName)
	}
	target := targetByID[selected.RID]
	worksheetFile := files[target]
	if target == "" || worksheetFile == nil {
		return nil, errors.New("selected XLSX worksheet relationship is invalid")
	}
	shared, err := loadSharedStrings(files["xl/sharedstrings.xml"])
	if err != nil {
		return nil, err
	}
	pipeReader, pipeWriter := io.Pipe()
	go func() {
		err := writeWorksheetCSV(ctx, worksheetFile, shared, pipeWriter)
		_ = pipeWriter.CloseWithError(err)
	}()
	return pipeReader, nil
}

func writeWorksheetCSV(ctx context.Context, file *zip.File, shared []string, destination io.Writer) error {
	source, err := file.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	decoder := xml.NewDecoder(io.LimitReader(source, 2<<30))
	writer := csv.NewWriter(destination)
	defer writer.Flush()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return writer.Error()
		}
		if err != nil {
			return fmt.Errorf("decode worksheet XML: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "row" {
			continue
		}
		var row worksheetRow
		if err := decoder.DecodeElement(&row, &start); err != nil {
			return fmt.Errorf("decode worksheet row: %w", err)
		}
		values := make([]string, 0, len(row.Cells))
		for _, cell := range row.Cells {
			if cell.Formula != nil {
				return fmt.Errorf("XLSX formula cell %s is prohibited", cell.Reference)
			}
			column, err := xlsxColumnIndex(cell.Reference)
			if err != nil || column <= 0 || column > maxXLSXColumns {
				return fmt.Errorf("XLSX cell reference %q is invalid or exceeds column limit", cell.Reference)
			}
			for len(values) < column {
				values = append(values, "")
			}
			value := cell.Value
			switch strings.ToLower(strings.TrimSpace(cell.Type)) {
			case "s":
				index, parseErr := strconv.Atoi(strings.TrimSpace(cell.Value))
				if parseErr != nil || index < 0 || index >= len(shared) {
					return fmt.Errorf("XLSX shared-string reference %q is invalid", cell.Value)
				}
				value = shared[index]
			case "inlinestr":
				value = cell.Inline.String()
			case "b":
				if strings.TrimSpace(cell.Value) == "1" {
					value = "TRUE"
				} else {
					value = "FALSE"
				}
			case "str", "n", "":
			default:
				return fmt.Errorf("XLSX cell type %q is unsupported", cell.Type)
			}
			if len(value) > MaxFieldLength {
				return fmt.Errorf("XLSX cell %s exceeds maximum field length", cell.Reference)
			}
			values[column-1] = value
		}
		if len(values) > 0 {
			if err := writer.Write(values); err != nil {
				return err
			}
			writer.Flush()
			if err := writer.Error(); err != nil {
				return err
			}
		}
	}
}

func loadSharedStrings(file *zip.File) ([]string, error) {
	if file == nil {
		return nil, nil
	}
	source, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer source.Close()
	decoder := xml.NewDecoder(io.LimitReader(source, maxXLSXSharedBytes+1))
	values := make([]string, 0)
	var consumed int64
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return values, nil
		}
		if err != nil {
			return nil, fmt.Errorf("decode shared strings: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "si" {
			continue
		}
		var value inlineStringXML
		if err := decoder.DecodeElement(&value, &start); err != nil {
			return nil, err
		}
		text := value.String()
		consumed += int64(len(text))
		if len(values) >= maxXLSXSharedStrings || consumed > maxXLSXSharedBytes {
			return nil, errors.New("XLSX shared strings exceed permitted limits")
		}
		values = append(values, text)
	}
}

func decodeBoundedXML(file *zip.File, maximum int64, destination any) error {
	source, err := file.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	limited := io.LimitReader(source, maximum+1)
	decoder := xml.NewDecoder(limited)
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	return nil
}

func normaliseXLSXPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	parts := make([]string, 0)
	for _, part := range strings.Split(value, "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		default:
			parts = append(parts, strings.ToLower(part))
		}
	}
	return strings.Join(parts, "/")
}

func xlsxColumnIndex(reference string) (int, error) {
	index := 0
	letters := 0
	for _, character := range strings.ToUpper(strings.TrimSpace(reference)) {
		if character < 'A' || character > 'Z' {
			break
		}
		letters++
		index = index*26 + int(character-'A'+1)
	}
	if letters == 0 {
		return 0, errors.New("cell reference has no column")
	}
	return index, nil
}
