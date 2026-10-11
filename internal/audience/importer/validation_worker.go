package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"campaign-platform/internal/storage"
)

var ErrUnsupportedImportFormat = errors.New("import format is not yet supported by the validation worker")

type ValidationWork struct {
	ImportID           string
	ObjectKey          string
	UploadSessionID    string
	OriginalFilename   string
	DetectedMediaType  string
	FileSHA256         string
	ByteSize           int64
	DefaultCountryISO2 string
	Mapping            json.RawMessage
	Lease              ValidationLease
}

type ValidationClaimer interface {
	ClaimReady(context.Context, int) ([]ValidationWork, error)
}

type UploadSessionReader interface {
	Get(context.Context, string) (UploadSession, error)
}

// ValidationWorker claims clean quarantined files directly from PostgreSQL.
// Claiming is deliberately independent of Redis so a lost queue notification
// cannot strand an import. The database lease is the authoritative ownership
// record and all staging writes present its fencing version.
type ValidationWorker struct {
	Claimer               ValidationClaimer
	Staging               StagingRepository
	Store                 storage.ObjectStore
	UploadSessions        UploadSessionReader
	Ingest                *IngestService
	TempDir               string
	Options               PreviewOptions
	Concurrency           int
	ClaimBatch            int
	PollInterval          time.Duration
	ClaimFailureBackoff   time.Duration
	MaximumFailureBackoff time.Duration
	OnError               func(ValidationWork, error)
	active                atomic.Int64
}

func (w *ValidationWorker) Run(ctx context.Context) error {
	if w == nil || w.Claimer == nil || w.Staging == nil || w.Store == nil || w.Ingest == nil {
		return errors.New("validation worker dependencies are required")
	}
	if w.Ingest.Repository != w.Staging {
		return errors.New("validation worker and ingest service must share the same staging repository")
	}
	concurrency := w.Concurrency
	if concurrency <= 0 {
		concurrency = 2
	}
	claimBatch := w.ClaimBatch
	if claimBatch <= 0 || claimBatch > concurrency {
		claimBatch = concurrency
	}
	poll := w.PollInterval
	if poll <= 0 {
		poll = time.Second
	}
	failureBackoff := w.ClaimFailureBackoff
	if failureBackoff <= 0 {
		failureBackoff = time.Second
	}
	maximumBackoff := w.MaximumFailureBackoff
	if maximumBackoff <= 0 {
		maximumBackoff = 30 * time.Second
	}

	semaphore := make(chan struct{}, concurrency)
	var workers sync.WaitGroup
	defer workers.Wait()
	currentBackoff := failureBackoff
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		available := concurrency - int(w.active.Load())
		if available <= 0 {
			if !sleepValidation(ctx, poll) {
				return nil
			}
			continue
		}
		limit := claimBatch
		if limit > available {
			limit = available
		}
		items, err := w.Claimer.ClaimReady(ctx, limit)
		if err != nil {
			if w.OnError != nil {
				w.OnError(ValidationWork{}, fmt.Errorf("claim validation imports: %w", err))
			}
			if !sleepValidation(ctx, currentBackoff) {
				return nil
			}
			currentBackoff *= 2
			if currentBackoff > maximumBackoff {
				currentBackoff = maximumBackoff
			}
			continue
		}
		currentBackoff = failureBackoff
		if len(items) == 0 {
			if !sleepValidation(ctx, poll) {
				return nil
			}
			continue
		}
		for _, item := range items {
			semaphore <- struct{}{}
			w.active.Add(1)
			workers.Add(1)
			go func(work ValidationWork) {
				defer func() {
					<-semaphore
					w.active.Add(-1)
					workers.Done()
				}()
				if err := w.process(ctx, work); err != nil && w.OnError != nil {
					w.OnError(work, err)
				}
			}(item)
		}
	}
}

func (w *ValidationWorker) process(ctx context.Context, work ValidationWork) error {
	if strings.TrimSpace(work.ImportID) == "" {
		return errors.New("validation work is incomplete")
	}
	mapping, err := decodeColumnMapping(work.Mapping)
	if err != nil {
		return w.failBeforeProcessing(work, err)
	}
	options := w.Options
	options.Mapping = mapping
	if strings.TrimSpace(options.DefaultCountryISO2) == "" {
		options.DefaultCountryISO2 = work.DefaultCountryISO2
	}
	if strings.TrimSpace(work.UploadSessionID) != "" {
		return w.processUploadSession(ctx, work, mapping, options)
	}
	if strings.TrimSpace(work.ObjectKey) == "" {
		return errors.New("validation work is missing its source object")
	}
	return w.processLegacyObject(ctx, work, mapping, options)
}

func (w *ValidationWorker) processLegacyObject(ctx context.Context, work ValidationWork, mapping ColumnMapping, options PreviewOptions) error {
	object, metadata, err := w.Store.Open(ctx, work.ObjectKey)
	if err != nil {
		return w.failBeforeProcessing(work, fmt.Errorf("open import object: %w", err))
	}
	defer object.Close()
	if metadata.Size != work.ByteSize || !strings.EqualFold(metadata.SHA256, work.FileSHA256) {
		return w.failBeforeProcessing(work, errors.New("quarantined import object no longer matches immutable file evidence"))
	}
	switch {
	case strings.EqualFold(work.DetectedMediaType, "text/csv"):
		_, err = w.Ingest.ProcessClaimed(ctx, work.ImportID, work.Lease, object, options)
	case strings.EqualFold(work.DetectedMediaType, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"):
		_, err = w.Ingest.ProcessClaimedWithProcessor(ctx, work.ImportID, work.Lease, func(processCtx context.Context, consumer CandidateConsumer) (PreviewResult, error) {
			return ProcessXLSX(processCtx, object, metadata.Size, mapping.Worksheet, options, consumer)
		})
	default:
		err = fmt.Errorf("%w: %s", ErrUnsupportedImportFormat, work.DetectedMediaType)
	}
	return err
}

func (w *ValidationWorker) processUploadSession(ctx context.Context, work ValidationWork, mapping ColumnMapping, options PreviewOptions) error {
	if w.UploadSessions == nil {
		return w.failBeforeProcessing(work, errors.New("upload session reader is required for resumable import validation"))
	}
	session, err := w.UploadSessions.Get(ctx, work.UploadSessionID)
	if err != nil {
		return w.failBeforeProcessing(work, fmt.Errorf("load upload session: %w", err))
	}
	if session.State != UploadSessionImportCreated ||
		session.LinkedImportID != work.ImportID ||
		session.ExpectedBytes != work.ByteSize ||
		!strings.EqualFold(session.FinalSHA256, work.FileSHA256) ||
		!strings.EqualFold(session.DetectedMediaType, work.DetectedMediaType) {
		return w.failBeforeProcessing(work, errors.New("resumable import source is not terminally linked to immutable upload evidence"))
	}
	source := &UploadCompositeSource{Store: w.Store}
	switch {
	case strings.EqualFold(work.DetectedMediaType, "text/csv"):
		reader, err := source.Open(ctx, session)
		if err != nil {
			return w.failBeforeProcessing(work, fmt.Errorf("open resumable CSV source: %w", err))
		}
		defer reader.Close()
		_, err = w.Ingest.ProcessClaimed(ctx, work.ImportID, work.Lease, reader, options)
		return err
	case strings.EqualFold(work.DetectedMediaType, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"):
		reader, err := source.Open(ctx, session)
		if err != nil {
			return w.failBeforeProcessing(work, fmt.Errorf("open resumable XLSX source: %w", err))
		}
		defer reader.Close()
		temporary, err := os.CreateTemp(w.TempDir, "campaign-platform-validation-*.xlsx")
		if err != nil {
			return w.failBeforeProcessing(work, err)
		}
		name := temporary.Name()
		defer os.Remove(name)
		defer temporary.Close()
		count, err := io.Copy(temporary, io.LimitReader(reader, work.ByteSize+1))
		if err != nil {
			return w.failBeforeProcessing(work, err)
		}
		if count != work.ByteSize {
			return w.failBeforeProcessing(work, fmt.Errorf("%w: copied %d bytes, expected %d", ErrUploadPartIncomplete, count, work.ByteSize))
		}
		if _, err := temporary.Seek(0, io.SeekStart); err != nil {
			return w.failBeforeProcessing(work, err)
		}
		_, err = w.Ingest.ProcessClaimedWithProcessor(ctx, work.ImportID, work.Lease, func(processCtx context.Context, consumer CandidateConsumer) (PreviewResult, error) {
			return ProcessXLSX(processCtx, temporary, count, mapping.Worksheet, options, consumer)
		})
		return err
	default:
		return w.failBeforeProcessing(work, fmt.Errorf("%w: %s", ErrUnsupportedImportFormat, work.DetectedMediaType))
	}
}

func (w *ValidationWorker) failBeforeProcessing(work ValidationWork, cause error) error {
	failCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.Staging.Fail(failCtx, work.ImportID, work.Lease, cause); err != nil {
		return errors.Join(cause, fmt.Errorf("record validation failure: %w", err))
	}
	return cause
}

func decodeColumnMapping(raw json.RawMessage) (ColumnMapping, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ColumnMapping{}, errors.New("import column mapping is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var mapping ColumnMapping
	if err := decoder.Decode(&mapping); err != nil {
		return ColumnMapping{}, fmt.Errorf("decode import column mapping: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ColumnMapping{}, errors.New("import column mapping contains trailing data")
	}
	if strings.TrimSpace(mapping.MSISDN) == "" {
		return ColumnMapping{}, errors.New("import column mapping must identify the MSISDN column")
	}
	return mapping, nil
}

func (w *ValidationWorker) Active() int64 {
	if w == nil {
		return 0
	}
	return w.active.Load()
}

func sleepValidation(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
