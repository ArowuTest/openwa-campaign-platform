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
	"campaign-platform/internal/identity"
	sharedid "campaign-platform/internal/shared/id"
	"campaign-platform/internal/storage"
)

type ExportJobRepository interface {
	ClaimExport(context.Context, string, time.Time, time.Duration) (ExportRequest, error)
	CompleteExport(context.Context, string, string, string, string, int64, time.Time, time.Time) error
	FailExport(context.Context, string, string, string, time.Time) error
	ClaimExpiringExports(context.Context, time.Time, int) ([]ExportRequest, error)
	CompleteExpiration(context.Context, string, time.Time) error
	FailExpiration(context.Context, string, string, time.Time) error
	CampaignReport(context.Context, string, time.Time) (CampaignReport, error)
}

type PrivacyPackageResolver interface {
	ResolvePrivacyPackage(json.RawMessage) (json.RawMessage, error)
}

// AuthenticationEventExportRow is the deliberately redacted representation of
// an authentication event that may leave the controlled export boundary.
type AuthenticationEventExportRow struct {
	Source        string    `json:"source"`
	ID            string    `json:"id"`
	ActorID       string    `json:"actorId,omitempty"`
	EventType     string    `json:"eventType"`
	CorrelationID string    `json:"correlationId"`
	OccurredAt    time.Time `json:"occurredAt"`
}

// AuditExportPayload keeps the hash-chained business ledger and the separate
// authentication stream distinguishable in every export format.
type AuditExportPayload struct {
	BusinessEvents            []audit.Event                  `json:"businessEvents"`
	AuthenticationEvents      []AuthenticationEventExportRow `json:"authenticationEvents"`
	BusinessHeadSequence      uint64                         `json:"businessHeadSequence,omitempty"`
	BusinessHeadHash          string                         `json:"businessHeadHash,omitempty"`
	AuthenticationEventAsOf   *time.Time                     `json:"authenticationEventAsOf,omitempty"`
	AuthenticationEventHeadID string                         `json:"authenticationEventHeadId,omitempty"`
}

const maxAuthenticationExportRows = identity.MaxAuthenticationEventSnapshotRows

type ExportWorker struct {
	Repository           ExportJobRepository
	AuditRepository      audit.Repository
	AuthenticationEvents identity.AuthenticationEventQueryRepository
	PrivacyPackages      PrivacyPackageResolver
	Objects              storage.ObjectStore
	WorkerID             string
	LeaseDuration        time.Duration
	ReadyTTL             time.Duration
	Clock                func() time.Time
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
		failErr := w.Repository.FailExport(ctx, job.ID, "RENDER_FAILED", safeExportError(err), w.now())
		return true, errors.Join(err, failErr)
	}
	key := fmt.Sprintf("exports/%s/%s.%s", job.Kind, job.ID, strings.ToLower(job.Format))
	meta, err := w.Objects.Put(ctx, key, bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		failErr := w.Repository.FailExport(ctx, job.ID, "STORE_FAILED", safeExportError(err), w.now())
		return true, errors.Join(err, failErr)
	}
	ttl := w.ReadyTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	now := w.now()
	if err := w.Repository.CompleteExport(ctx, job.ID, meta.Key, contentType, meta.SHA256, meta.Size, now, now.Add(ttl)); err != nil {
		deleteErr := w.Objects.Delete(context.Background(), meta.Key)
		return true, errors.Join(err, deleteErr)
	}
	return true, nil
}

func (w *ExportWorker) Expire(ctx context.Context, limit int) (int, error) {
	now := w.now()
	jobs, err := w.Repository.ClaimExpiringExports(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	count := 0
	var failures []error
	for _, job := range jobs {
		if job.ObjectKey != "" {
			if err := w.Objects.Delete(ctx, job.ObjectKey); err != nil && !errors.Is(err, storage.ErrNotFound) {
				failures = append(failures, err)
				if recordErr := w.Repository.FailExpiration(ctx, job.ID, safeExportError(err), now); recordErr != nil {
					failures = append(failures, recordErr)
				}
				continue
			}
		}
		if err := w.Repository.CompleteExpiration(ctx, job.ID, now); err != nil {
			failures = append(failures, err)
			continue
		}
		count++
	}
	return count, errors.Join(failures...)
}

func (w *ExportWorker) render(ctx context.Context, job ExportRequest) ([]byte, string, error) {
	var value any
	switch job.Kind {
	case "CAMPAIGN_REPORT", "PRIVACY_PACKAGE":
		if len(job.FrozenPayload) == 0 {
			return nil, "", errors.New("export payload was not frozen at approval")
		}
		if job.Kind == "CAMPAIGN_REPORT" {
			var report CampaignReport
			if err := json.Unmarshal(job.FrozenPayload, &report); err != nil {
				return nil, "", fmt.Errorf("decode frozen campaign report: %w", err)
			}
			value = report
		} else {
			if w.PrivacyPackages == nil {
				return nil, "", errors.New("privacy package resolver is unavailable")
			}
			resolved, err := w.PrivacyPackages.ResolvePrivacyPackage(job.FrozenPayload)
			if err != nil {
				return nil, "", err
			}
			var payload any
			if err := json.Unmarshal(resolved, &payload); err != nil {
				return nil, "", fmt.Errorf("decode resolved privacy package: %w", err)
			}
			value = payload
		}
	case "AUDIT_LOG":
		payload, err := w.loadFrozenAudit(ctx, job)
		if err != nil {
			return nil, "", err
		}
		value = payload
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
		b, e := renderSimplePDFWithWatermark(value, job.WatermarkText)
		return b, "application/pdf", e
	case "XLSX":
		b, e := renderSimpleXLSX(value)
		return b, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", e
	default:
		return nil, "", ErrInvalid
	}
}

type auditExportCriteria struct {
	ActorID        string     `json:"actorId"`
	Action         string     `json:"action"`
	ObjectType     string     `json:"objectType"`
	ObjectID       string     `json:"objectId"`
	OrganisationID string     `json:"organisationId"`
	Outcome        string     `json:"outcome"`
	Sensitivity    string     `json:"sensitivity"`
	CorrelationID  string     `json:"correlationId"`
	IPAddress      string     `json:"ipAddress"`
	UserID         string     `json:"userId"`
	EventType      string     `json:"eventType"`
	From           *time.Time `json:"from"`
	To             *time.Time `json:"to"`
}

func decodeAuditExportCriteria(raw json.RawMessage) (auditExportCriteria, error) {
	criteria := auditExportCriteria{}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return criteria, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&criteria); err != nil {
		return auditExportCriteria{}, fmt.Errorf("decode audit export criteria: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return auditExportCriteria{}, errors.New("decode audit export criteria: expected one JSON value")
	}
	criteria.ActorID = strings.TrimSpace(criteria.ActorID)
	criteria.Action = strings.TrimSpace(criteria.Action)
	criteria.ObjectType = strings.TrimSpace(criteria.ObjectType)
	criteria.ObjectID = strings.TrimSpace(criteria.ObjectID)
	criteria.OrganisationID = strings.TrimSpace(criteria.OrganisationID)
	criteria.Outcome = strings.TrimSpace(criteria.Outcome)
	criteria.Sensitivity = strings.TrimSpace(criteria.Sensitivity)
	criteria.CorrelationID = strings.TrimSpace(criteria.CorrelationID)
	criteria.IPAddress = strings.TrimSpace(criteria.IPAddress)
	criteria.UserID = strings.TrimSpace(criteria.UserID)
	criteria.EventType = strings.ToUpper(strings.TrimSpace(criteria.EventType))
	return criteria, nil
}

func validateAuditExportCriteria(criteria auditExportCriteria) error {
	return identity.ValidateAuthenticationEventQuery(identity.AuthenticationEventQuery{
		UserID: criteria.UserID, EventType: identity.AuthenticationEventType(criteria.EventType),
		CorrelationID: criteria.CorrelationID, From: criteria.From, To: criteria.To, Limit: 1,
	})
}

func authenticationExportQuery(criteria auditExportCriteria) (identity.AuthenticationEventQuery, bool, error) {
	if err := validateAuditExportCriteria(criteria); err != nil {
		return identity.AuthenticationEventQuery{}, false, err
	}
	query := identity.AuthenticationEventQuery{
		UserID: criteria.UserID, EventType: identity.AuthenticationEventType(criteria.EventType),
		CorrelationID: criteria.CorrelationID, From: criteria.From, To: criteria.To, Limit: 500,
	}
	// Business-ledger-only filters must not silently broaden the authentication
	// stream. A UUID actorId is a safe shorthand for userId only when it is the
	// sole non-temporal filter; otherwise an explicit authentication scope is
	// required for authentication rows.
	authScoped := query.UserID != "" || query.EventType != "" || query.CorrelationID != ""
	businessFilters := criteria.Action != "" || criteria.ObjectType != "" || criteria.ObjectID != "" ||
		criteria.OrganisationID != "" || criteria.Outcome != "" || criteria.Sensitivity != "" ||
		criteria.IPAddress != ""
	if !authScoped && criteria.ActorID != "" && !businessFilters && sharedid.IsUUID(criteria.ActorID) {
		query.UserID = criteria.ActorID
		authScoped = true
	}
	if !authScoped && (criteria.ActorID != "" || businessFilters) {
		return identity.AuthenticationEventQuery{}, false, nil
	}
	return query, true, nil
}

func (w *ExportWorker) loadFrozenAudit(ctx context.Context, job ExportRequest) (AuditExportPayload, error) {
	if w.AuditRepository == nil {
		return AuditExportPayload{}, errors.New("audit repository unavailable")
	}
	criteria, err := decodeAuditExportCriteria(job.Criteria)
	if err != nil {
		return AuditExportPayload{}, err
	}
	query := audit.Query{
		ActorID: criteria.ActorID, Action: criteria.Action, ObjectType: criteria.ObjectType, ObjectID: criteria.ObjectID,
		OrganisationID: criteria.OrganisationID, Outcome: criteria.Outcome, Sensitivity: criteria.Sensitivity,
		CorrelationID: criteria.CorrelationID, IPAddress: criteria.IPAddress, From: criteria.From, To: criteria.To, Limit: 1000,
	}
	items := make([]audit.Event, 0, 1000)
	if job.AuditHeadSequence > 0 {
		head, headErr := w.AuditRepository.List(ctx, job.AuditHeadSequence-1, 1)
		if headErr != nil {
			return AuditExportPayload{}, headErr
		}
		if len(head) != 1 || head[0].Sequence != job.AuditHeadSequence || head[0].Hash != job.AuditHeadHash {
			return AuditExportPayload{}, errors.New("approved audit chain head no longer verifies")
		}
		for {
			page, pageErr := w.AuditRepository.Search(ctx, query)
			if pageErr != nil {
				return AuditExportPayload{}, pageErr
			}
			stop := false
			for _, event := range page.Items {
				if event.Sequence > job.AuditHeadSequence {
					stop = true
					break
				}
				items = append(items, event)
			}
			if stop || page.NextSequence == 0 || page.NextSequence >= job.AuditHeadSequence {
				break
			}
			query.AfterSequence = page.NextSequence
		}
	}
	authentication, err := w.loadFrozenAuthenticationEvents(ctx, job, criteria)
	if err != nil {
		return AuditExportPayload{}, err
	}
	return AuditExportPayload{
		BusinessEvents: items, AuthenticationEvents: authentication,
		BusinessHeadSequence: job.AuditHeadSequence, BusinessHeadHash: job.AuditHeadHash,
		AuthenticationEventAsOf: job.AuthenticationEventAsOf, AuthenticationEventHeadID: job.AuthenticationEventHeadID,
	}, nil
}

func (w *ExportWorker) loadFrozenAuthenticationEvents(_ context.Context, job ExportRequest, criteria auditExportCriteria) ([]AuthenticationEventExportRow, error) {
	return loadAuthenticationEventSnapshot(job, criteria)
}

func renderCSV(value any) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	write := func(row ...string) error {
		for i := range row {
			row[i] = safeSpreadsheetCell(row[i])
		}
		return writer.Write(row)
	}

	switch v := value.(type) {
	case CampaignReport:
		if err := write("section", "metric", "value"); err != nil {
			return nil, err
		}
		fixed := [][]string{
			{"campaign", "id", v.CampaignID},
			{"campaign", "name", v.Name},
			{"campaign", "status", v.Status},
			{"commercial", "status", v.Commercial.Status},
			{"commercial", "quotationReference", v.Commercial.QuotationReference},
			{"commercial", "invoiceReference", v.Commercial.InvoiceReference},
			{"commercial", "currency", v.Commercial.Currency},
			{"commercial", "approvedRecipients", fmt.Sprint(v.Commercial.ApprovedRecipients)},
			{"commercial", "totalAmountMinor", fmt.Sprint(v.Commercial.TotalAmountMinor)},
			{"privacy", "policyId", v.Privacy.PolicyID},
			{"privacy", "policyVersion", fmt.Sprint(v.Privacy.PolicyVersion)},
			{"privacy", "minimumCohortSize", fmt.Sprint(v.Privacy.MinimumCohortSize)},
		}
		for _, row := range fixed {
			if err := write(row...); err != nil {
				return nil, err
			}
		}
		maps := []struct {
			name string
			m    map[string]int64
		}{{"audience", v.Audience}, {"delivery", v.Delivery}, {"engagement", v.Engagement}, {"exceptions", v.Exceptions}}
		for _, group := range maps {
			keys := make([]string, 0, len(group.m))
			for key := range group.m {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if err := write(group.name, key, fmt.Sprint(group.m[key])); err != nil {
					return nil, err
				}
			}
		}
		breakdownNames := make([]string, 0, len(v.Breakdowns))
		for name := range v.Breakdowns {
			breakdownNames = append(breakdownNames, name)
		}
		sort.Strings(breakdownNames)
		for _, name := range breakdownNames {
			for _, cell := range v.Breakdowns[name] {
				value := fmt.Sprint(cell.Count)
				if cell.Suppressed {
					value = "SUPPRESSED"
				}
				if err := write("breakdown:"+name, cell.Label, value); err != nil {
					return nil, err
				}
			}
		}
		for _, pool := range v.Pools {
			prefix := "pool:" + pool.SenderPoolName
			if err := write(prefix, "engine", pool.Engine); err != nil {
				return nil, err
			}
			if err := write(prefix, "gatewayPoolId", pool.GatewayPoolID); err != nil {
				return nil, err
			}
			if err := write(prefix, "reservedMessagesPerMinute", fmt.Sprint(pool.ReservedMessagesPerMinute)); err != nil {
				return nil, err
			}
			keys := make([]string, 0, len(pool.Recipients))
			for key := range pool.Recipients {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if err := write(prefix, key, fmt.Sprint(pool.Recipients[key])); err != nil {
					return nil, err
				}
			}
		}
		for _, warning := range v.Warnings {
			if err := write("warning", warning, "1"); err != nil {
				return nil, err
			}
		}
	case AuditExportPayload:
		businessHeadSequence := fmt.Sprint(v.BusinessHeadSequence)
		authenticationAsOf := ""
		if v.AuthenticationEventAsOf != nil {
			authenticationAsOf = v.AuthenticationEventAsOf.UTC().Format(time.RFC3339Nano)
		}
		if err := write("source", "sequence", "occurred_at", "actor_type", "actor_id", "action", "object_type", "object_id", "reason", "correlation_id", "hash", "event_id", "event_type", "business_head_sequence", "business_head_hash", "authentication_event_as_of", "authentication_event_head_id"); err != nil {
			return nil, err
		}
		for _, event := range v.BusinessEvents {
			if err := write("business", fmt.Sprint(event.Sequence), event.OccurredAt.Format(time.RFC3339Nano), event.ActorType, event.ActorID, event.Action, event.ObjectType, event.ObjectID, event.Reason, event.CorrelationID, event.Hash, "", "", businessHeadSequence, v.BusinessHeadHash, authenticationAsOf, v.AuthenticationEventHeadID); err != nil {
				return nil, err
			}
		}
		for _, event := range v.AuthenticationEvents {
			if err := write(event.Source, "", event.OccurredAt.Format(time.RFC3339Nano), "", event.ActorID, "", "", "", "", event.CorrelationID, "", event.ID, event.EventType, businessHeadSequence, v.BusinessHeadHash, authenticationAsOf, v.AuthenticationEventHeadID); err != nil {
				return nil, err
			}
		}
	case []audit.Event:
		if err := write("sequence", "occurred_at", "actor_type", "actor_id", "action", "object_type", "object_id", "reason", "correlation_id", "hash"); err != nil {
			return nil, err
		}
		for _, event := range v {
			if err := write(fmt.Sprint(event.Sequence), event.OccurredAt.Format(time.RFC3339Nano), event.ActorType, event.ActorID, event.Action, event.ObjectType, event.ObjectID, event.Reason, event.CorrelationID, event.Hash); err != nil {
				return nil, err
			}
		}
	default:
		rows, err := flattenExportValue(v)
		if err != nil {
			return nil, err
		}
		if err := write("path", "value"); err != nil {
			return nil, err
		}
		for _, row := range rows {
			if err := write(row[0], row[1]); err != nil {
				return nil, err
			}
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func safeSpreadsheetCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}

func flattenExportValue(value any) ([][2]string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	rows := make([][2]string, 0, 64)
	var walk func(string, any)
	walk = func(path string, current any) {
		switch typed := current.(type) {
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				next := key
				if path != "" {
					next = path + "." + key
				}
				walk(next, typed[key])
			}
		case []any:
			for index, item := range typed {
				walk(fmt.Sprintf("%s[%d]", path, index), item)
			}
		case nil:
			rows = append(rows, [2]string{path, ""})
		case string:
			rows = append(rows, [2]string{path, typed})
		default:
			rows = append(rows, [2]string{path, fmt.Sprint(typed)})
		}
	}
	walk("", decoded)
	return rows, nil
}

func renderSimplePDF(value any) ([]byte, error) {
	return renderSimplePDFWithWatermark(value, "")
}

func renderSimplePDFWithWatermark(value any, watermark string) ([]byte, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, 128)
	if strings.TrimSpace(watermark) != "" {
		lines = append(lines, "WATERMARK: "+strings.TrimSpace(watermark), "")
	}
	for _, line := range strings.Split(string(raw), "\n") {
		lines = append(lines, wrapPDFLine(line, 100)...)
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	const linesPerPage = 72
	pageCount := (len(lines) + linesPerPage - 1) / linesPerPage
	fontID := 3 + pageCount*2
	objects := make([]string, fontID)
	objects[0] = "<< /Type /Catalog /Pages 2 0 R >>"
	kids := make([]string, 0, pageCount)
	for page := 0; page < pageCount; page++ {
		pageID := 3 + page*2
		contentID := pageID + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", pageID))
		objects[pageID-1] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", fontID, contentID)
		start := page * linesPerPage
		end := start + linesPerPage
		if end > len(lines) {
			end = len(lines)
		}
		var content strings.Builder
		content.WriteString("BT /F1 8 Tf 40 810 Td 10 TL ")
		for index, line := range lines[start:end] {
			if index > 0 {
				content.WriteString("T* ")
			}
			content.WriteString("(" + escapePDFText(line) + ") Tj ")
		}
		content.WriteString("ET")
		stream := content.String()
		objects[contentID-1] = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream)
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pageCount)
	objects[fontID-1] = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"

	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer << /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes(), nil
}

func wrapPDFLine(value string, width int) []string {
	if width <= 0 {
		width = 100
	}
	runes := []rune(value)
	if len(runes) == 0 {
		return []string{""}
	}
	lines := make([]string, 0, len(runes)/width+1)
	for len(runes) > width {
		lines = append(lines, string(runes[:width]))
		runes = runes[width:]
	}
	lines = append(lines, string(runes))
	return lines
}

func escapePDFText(value string) string {
	var out strings.Builder
	for _, character := range value {
		switch character {
		case '\\', '(', ')':
			out.WriteByte('\\')
			out.WriteRune(character)
		default:
			if character < 32 || character > 126 {
				out.WriteByte('?')
			} else {
				out.WriteRune(character)
			}
		}
	}
	return out.String()
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
	for rowIndex, row := range rows {
		fmt.Fprintf(&sheet, "<row r=\"%d\">", rowIndex+1)
		for columnIndex, cell := range row {
			column := xlsxCol(columnIndex + 1)
			fmt.Fprintf(&sheet, "<c r=\"%s%d\" t=\"inlineStr\"><is><t xml:space=\"preserve\">%s</t></is></c>", column, rowIndex+1, html.EscapeString(cell))
		}
		sheet.WriteString("</row>")
	}
	sheet.WriteString("</sheetData></worksheet>")
	var out bytes.Buffer
	archive := zip.NewWriter(&out)
	files := []struct {
		name string
		data string
	}{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Export" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`},
		{"xl/worksheets/sheet1.xml", sheet.String()},
	}
	for _, file := range files {
		entry, createErr := archive.Create(file.name)
		if createErr != nil {
			_ = archive.Close()
			return nil, createErr
		}
		if _, writeErr := io.WriteString(entry, file.data); writeErr != nil {
			_ = archive.Close()
			return nil, writeErr
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func xlsxCol(n int) string {
	var value string
	for n > 0 {
		n--
		value = string(rune('A'+n%26)) + value
		n /= 26
	}
	return value
}
func safeExportError(err error) string {
	sum := sha256.Sum256([]byte(err.Error()))
	return "export operation failed; reference=" + hex.EncodeToString(sum[:6])
}
