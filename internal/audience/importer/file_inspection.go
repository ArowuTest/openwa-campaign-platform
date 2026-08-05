package importer

import (
	"archive/zip"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"campaign-platform/internal/storage"
)

type FileInspection struct {
	Format    string
	MediaType string
}

// InspectImportFile validates the actual object signature and a bounded amount
// of structure. It deliberately does not trust the browser MIME type.
func InspectImportFile(filename string, object storage.ReadSeekCloser, size int64) (FileInspection, error) {
	if object == nil || size <= 0 {
		return FileInspection{}, errors.New("non-empty import object is required")
	}
	extension := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	if _, err := object.Seek(0, io.SeekStart); err != nil {
		return FileInspection{}, err
	}
	switch extension {
	case ".csv":
		if err := inspectCSV(object); err != nil {
			return FileInspection{}, err
		}
		return FileInspection{Format: "CSV", MediaType: "text/csv"}, nil
	case ".xlsx":
		readerAt, ok := object.(io.ReaderAt)
		if !ok {
			return FileInspection{}, errors.New("XLSX inspection requires random-access object reader")
		}
		if err := inspectXLSX(readerAt, size); err != nil {
			return FileInspection{}, err
		}
		return FileInspection{Format: "XLSX", MediaType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"}, nil
	default:
		return FileInspection{}, fmt.Errorf("unsupported import extension %q", extension)
	}
}

func inspectCSV(reader io.Reader) error {
	prefixBytes, err := io.ReadAll(io.LimitReader(reader, 64<<10))
	if err != nil {
		return err
	}
	prefix := strings.TrimPrefix(string(prefixBytes), "\ufeff")
	if strings.ContainsRune(prefix, '\x00') || !utf8.ValidString(prefix) {
		return errors.New("CSV must be valid UTF-8 text without NUL bytes")
	}
	parser := csv.NewReader(strings.NewReader(prefix))
	parser.FieldsPerRecord = -1
	headers, err := parser.Read()
	if err != nil {
		return fmt.Errorf("invalid CSV header: %w", err)
	}
	if len(headers) == 0 || len(headers) > 500 {
		return errors.New("CSV header count is outside permitted range")
	}
	for _, header := range headers {
		if strings.TrimSpace(header) != "" {
			return nil
		}
	}
	return errors.New("CSV contains no named columns")
}

func inspectXLSX(reader io.ReaderAt, size int64) error {
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return fmt.Errorf("invalid XLSX ZIP container: %w", err)
	}
	if len(archive.File) == 0 || len(archive.File) > 10_000 {
		return errors.New("XLSX entry count is outside permitted range")
	}
	var contentTypes, workbook bool
	var total uint64
	for _, entry := range archive.File {
		name := strings.ToLower(strings.ReplaceAll(entry.Name, "\\", "/"))
		if strings.Contains(name, "../") || strings.HasPrefix(name, "/") {
			return errors.New("XLSX contains an unsafe entry path")
		}
		switch name {
		case "[content_types].xml":
			contentTypes = true
		case "xl/workbook.xml":
			workbook = true
		}
		if strings.Contains(name, "vbaproject.bin") || strings.HasPrefix(name, "xl/embeddings/") || strings.HasPrefix(name, "xl/externallinks/") {
			return errors.New("XLSX contains prohibited active or external content")
		}
		total += entry.UncompressedSize64
		if total > 2<<30 {
			return errors.New("XLSX expanded size exceeds permitted limit")
		}
		if entry.CompressedSize64 > 0 && entry.UncompressedSize64/entry.CompressedSize64 > 200 {
			return errors.New("XLSX contains a suspicious compression ratio")
		}
	}
	if !contentTypes || !workbook {
		return errors.New("XLSX is missing required workbook structures")
	}
	return nil
}
