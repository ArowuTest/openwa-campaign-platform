package operations

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"sort"
	"strings"
	"time"

	"campaign-platform/internal/audit"
	"campaign-platform/internal/storage"
)

type ExportJobRepository interface {
	ClaimExport(context.Context, string, time.Time, time.Duration) (ExportRequest, error)
	CompleteExport(context.Context, string, string, string, string, int64, time.Time, time.Time) error
	FailExport(context.Context, string, string, string, time.Time) error
	ExpireExports(context.Context, time.Time, int) ([]ExportRequest, error)
	CampaignReport(context.Context, string, time.Time) (CampaignReport, error)
}

type ExportWorker struct {
	Repository      ExportJobRepository
	AuditRepository audit.Repository
	Objects         storage.ObjectStore
	WorkerID        string
	LeaseDuration   time.Duration
	ReadyTTL        time.Duration
	Clock           func() time.Time
}

func (w *ExportWorker) now() time.Time {
	if w.Clock != nil {
		return w.Clock().UTC()
	}
	return time.Now().UTC()
}

func (w *ExportWorker) ProcessOne(ctx context.Context) (bool, error) {
	if w.Repository == nil || w.Objects == nil || strings.TrimSpace(w.WorkerID) == "" {
		return false, errors.New("export worker is not configured")
	}
	lease := w.LeaseDuration
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	job, err := w.Repository.ClaimExport(ctx, w.WorkerID, w.now(), lease)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	payload, contentType, err := w.render(ctx, job)
	if err != nil {
		_ = w.Repository.FailExport(ctx, job.ID, "RENDER_FAILED", safeExportError(err), w.now())
		return true, err
	}
	key := fmt.Sprintf("exports/%s/%s.%s", job.Kind, job.ID, strings.ToLower(job.Format))
	meta, err := w.Objects.Put(ctx, key, bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		_ = w.Repository.FailExport(ctx, job.ID, "STORE_FAILED", safeExportError(err), w.now())
		return true, err
	}
	ttl := w.ReadyTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	now := w.now()
	if err := w.Repository.CompleteExport(ctx, job.ID, meta.Key, contentType, meta.SHA256, meta.Size, now, now.Add(ttl)); err != nil {
		return true, err
	}
	return true, nil
}

func (w *ExportWorker) Expire(ctx context.Context, limit int) (int, error) {
	jobs, err := w.Repository.ExpireExports(ctx, w.now(), limit)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, j := range jobs {
		if j.ObjectKey != "" {
			if err := w.Objects.Delete(ctx, j.ObjectKey); err != nil && !errors.Is(err, storage.ErrNotFound) {
				continue
			}
		}
		count++
	}
	return count, nil
}

func (w *ExportWorker) render(ctx context.Context, job ExportRequest) ([]byte, string, error) {
	var value any
	switch job.Kind {
	case "CAMPAIGN_REPORT":
		report, err := w.Repository.CampaignReport(ctx, job.ObjectID, w.now())
		if err != nil {
			return nil, "", err
		}
		value = report
	case "AUDIT_LOG":
		if w.AuditRepository == nil {
			return nil, "", errors.New("audit repository unavailable")
		}
		events, err := w.AuditRepository.List(ctx, 0, 10000)
		if err != nil {
			return nil, "", err
		}
		value = events
	default:
		return nil, "", ErrInvalid
	}
	switch job.Format {
	case "JSON":
		b, e := json.MarshalIndent(value, "", "  ")
		return b, "application/json", e
	case "CSV":
		b, e := renderCSV(value)
		return b, "text/csv; charset=utf-8", e
	case "PDF":
		b, e := renderSimplePDF(value)
		return b, "application/pdf", e
	case "XLSX":
		b, e := renderSimpleXLSX(value)
		return b, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", e
	default:
		return nil, "", ErrInvalid
	}
}

func renderCSV(value any) ([]byte, error) {
	var buf bytes.Buffer
	wr := csv.NewWriter(&buf)
	switch v := value.(type) {
	case CampaignReport:
		_ = wr.Write([]string{"section", "metric", "value"})
		_ = wr.Write([]string{"campaign", "id", v.CampaignID})
		_ = wr.Write([]string{"campaign", "name", v.Name})
		_ = wr.Write([]string{"campaign", "status", v.Status})
		_ = wr.Write([]string{"commercial", "status", v.Commercial.Status})
		_ = wr.Write([]string{"commercial", "quotationReference", v.Commercial.QuotationReference})
		_ = wr.Write([]string{"commercial", "invoiceReference", v.Commercial.InvoiceReference})
		_ = wr.Write([]string{"commercial", "currency", v.Commercial.Currency})
		_ = wr.Write([]string{"commercial", "approvedRecipients", fmt.Sprint(v.Commercial.ApprovedRecipients)})
		_ = wr.Write([]string{"commercial", "totalAmountMinor", fmt.Sprint(v.Commercial.TotalAmountMinor)})
		maps := []struct {
			name string
			m    map[string]int64
		}{{"audience", v.Audience}, {"delivery", v.Delivery}, {"engagement", v.Engagement}, {"exceptions", v.Exceptions}}
		for _, group := range maps {
			keys := make([]string, 0, len(group.m))
			for k := range group.m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				_ = wr.Write([]string{group.name, k, fmt.Sprint(group.m[k])})
			}
		}
		for _, pool := range v.Pools {
			prefix := "pool:" + pool.SenderPoolName
			_ = wr.Write([]string{prefix, "engine", pool.Engine})
			_ = wr.Write([]string{prefix, "gatewayPoolId", pool.GatewayPoolID})
			_ = wr.Write([]string{prefix, "reservedMessagesPerMinute", fmt.Sprint(pool.ReservedMessagesPerMinute)})
			keys := make([]string, 0, len(pool.Recipients))
			for k := range pool.Recipients {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				_ = wr.Write([]string{prefix, k, fmt.Sprint(pool.Recipients[k])})
			}
		}
		for _, warning := range v.Warnings {
			_ = wr.Write([]string{"warning", warning, "1"})
		}
	case []audit.Event:
		_ = wr.Write([]string{"sequence", "occurred_at", "actor_type", "actor_id", "action", "object_type", "object_id", "reason", "correlation_id", "hash"})
		for _, e := range v {
			_ = wr.Write([]string{fmt.Sprint(e.Sequence), e.OccurredAt.Format(time.RFC3339Nano), e.ActorType, e.ActorID, e.Action, e.ObjectType, e.ObjectID, e.Reason, e.CorrelationID, e.Hash})
		}
	default:
		return nil, ErrInvalid
	}
	wr.Flush()
	return buf.Bytes(), wr.Error()
}

func renderSimplePDF(value any) ([]byte, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(raw), "\\", "\\\\")
	text = strings.ReplaceAll(text, "(", "\\(")
	text = strings.ReplaceAll(text, ")", "\\)")
	lines := strings.Split(text, "\n")
	if len(lines) > 55 {
		lines = lines[:55]
		lines = append(lines, "... report truncated in PDF view; use JSON/XLSX for full detail")
	}
	var content strings.Builder
	content.WriteString("BT /F1 8 Tf 40 800 Td 10 TL ")
	for i, l := range lines {
		if i > 0 {
			content.WriteString("T* ")
		}
		if len(l) > 110 {
			l = l[:110]
		}
		content.WriteString("(" + l + ") Tj ")
	}
	content.WriteString("ET")
	stream := content.String()
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>", fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream), "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, o := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer << /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes(), nil
}

func renderSimpleXLSX(value any) ([]byte, error) {
	csvBytes, err := renderCSV(value)
	if err != nil {
		return nil, err
	}
	rows, err := csv.NewReader(bytes.NewReader(csvBytes)).ReadAll()
	if err != nil {
		return nil, err
	}
	var sheet strings.Builder
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for i, row := range rows {
		fmt.Fprintf(&sheet, "<row r=\"%d\">", i+1)
		for j, cell := range row {
			col := xlsxCol(j + 1)
			fmt.Fprintf(&sheet, "<c r=\"%s%d\" t=\"inlineStr\"><is><t>%s</t></is></c>", col, i+1, html.EscapeString(cell))
		}
		sheet.WriteString("</row>")
	}
	sheet.WriteString("</sheetData></worksheet>")
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	files := map[string]string{"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`, "_rels/.rels": `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`, "xl/workbook.xml": `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Export" sheetId="1" r:id="rId1"/></sheets></workbook>`, "xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`, "xl/worksheets/sheet1.xml": sheet.String()}
	for name, data := range files {
		f, _ := z.Create(name)
		_, _ = io.WriteString(f, data)
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func xlsxCol(n int) string {
	var s string
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
}
func safeExportError(err error) string {
	sum := sha256.Sum256([]byte(err.Error()))
	return "export operation failed; reference=" + hex.EncodeToString(sum[:6])
}
