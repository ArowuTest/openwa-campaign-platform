package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"campaign-platform/internal/security/malware"
)

type UploadFinalisationWorker struct {
	Repository      UploadFinalisationRepository
	Source          *UploadCompositeSource
	Scanner         malware.Scanner
	Imports         *ImportService
	WorkerID        string
	LeaseDuration   time.Duration
	ClaimBatch      int
	PollInterval    time.Duration
	SourceRetention time.Duration
	TempDir         string
	Clock           func() time.Time
	OnError         func(UploadFinalisationWork, error)
	active          atomic.Int64
}

func (w *UploadFinalisationWorker) now() time.Time {
	if w != nil && w.Clock != nil {
		return w.Clock().UTC()
	}
	return time.Now().UTC()
}

func (w *UploadFinalisationWorker) leaseDuration() time.Duration {
	if w.LeaseDuration <= 0 {
		return 5 * time.Minute
	}
	return w.LeaseDuration
}

func (w *UploadFinalisationWorker) claimBatch() int {
	if w.ClaimBatch <= 0 || w.ClaimBatch > 100 {
		return 5
	}
	return w.ClaimBatch
}

func (w *UploadFinalisationWorker) sourceRetention() time.Duration {
	if w.SourceRetention <= 0 {
		return 30 * 24 * time.Hour
	}
	return w.SourceRetention
}

func (w *UploadFinalisationWorker) validate() error {
	if w == nil || w.Repository == nil || w.Source == nil || w.Source.Store == nil || w.Scanner == nil || w.Imports == nil {
		return errors.New("upload finalisation worker dependencies are required")
	}
	if strings.TrimSpace(w.WorkerID) == "" {
		return errors.New("upload finalisation worker ID is required")
	}
	return nil
}

func (w *UploadFinalisationWorker) Active() int64 {
	if w == nil {
		return 0
	}
	return w.active.Load()
}

// Process claims and finalises a bounded batch. It is safe to call repeatedly;
// PostgreSQL SKIP LOCKED claims and finalisation fencing allow multiple worker
// processes to execute concurrently without double-finalising one session.
func (w *UploadFinalisationWorker) Process(ctx context.Context) (int, error) {
	if err := w.validate(); err != nil {
		return 0, err
	}
	now := w.now()
	items, err := w.Repository.ClaimReadyFinalisations(ctx, w.WorkerID, w.leaseDuration(), w.claimBatch(), now)
	if err != nil {
		return 0, err
	}
	processed := 0
	var failures []error
	for _, work := range items {
		w.active.Add(1)
		err := w.processWork(ctx, work)
		w.active.Add(-1)
		if err != nil {
			failures = append(failures, fmt.Errorf("finalise upload %s: %w", work.Session.ID, err))
			if w.OnError != nil {
				w.OnError(work, err)
			}
			continue
		}
		processed++
	}
	return processed, errors.Join(failures...)
}

func (w *UploadFinalisationWorker) Run(ctx context.Context) error {
	if err := w.validate(); err != nil {
		return err
	}
	poll := w.PollInterval
	if poll <= 0 {
		poll = time.Second
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		processed, err := w.Process(ctx)
		if err != nil && ctx.Err() == nil && w.OnError == nil {
			return err
		}
		if processed > 0 {
			continue
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (w *UploadFinalisationWorker) processWork(ctx context.Context, work UploadFinalisationWork) error {
	if strings.TrimSpace(work.Session.ID) == "" || strings.TrimSpace(work.Lease.Owner) == "" {
		return errors.New("finalisation work is incomplete")
	}
	leaseState := newFinalisationLeaseState(work.Session, work.Lease)
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	heartbeatErr := make(chan error, 1)
	heartbeatDone := make(chan struct{})
	go w.heartbeat(processCtx, leaseState, heartbeatErr, heartbeatDone)
	defer func() {
		cancel()
		<-heartbeatDone
	}()

	wholeSHA, err := w.hashSource(processCtx, work.Session)
	if err != nil {
		return w.failWork(leaseState, err)
	}
	inspection, err := w.inspectSource(processCtx, work.Session)
	if err != nil {
		return w.failWork(leaseState, err)
	}
	if err := w.scanSource(processCtx, work.Session); err != nil {
		return w.failWork(leaseState, err)
	}
	select {
	case renewErr := <-heartbeatErr:
		return renewErr
	default:
	}

	retentionExpiry := w.now().Add(w.sourceRetention())
	batch, _, err := w.Imports.Create(processCtx, CreateImportInput{
		OrganisationID:        work.Session.OrganisationID,
		ConsentReviewID:       work.Session.ConsentReviewID,
		PurposeID:             work.Session.PurposeID,
		Channel:               work.Session.Channel,
		WordingVersion:        work.Session.WordingVersion,
		SourceName:            work.Session.SourceName,
		SourceSystem:          work.Session.SourceSystem,
		DefaultCountryISO2:    work.Session.DefaultCountryISO2,
		ObjectKey:             uploadSessionSourceReference(work.Session.ID),
		UploadSessionID:       work.Session.ID,
		OriginalFilename:      work.Session.OriginalFilename,
		DetectedMediaType:     inspection.MediaType,
		FileSHA256:            wholeSHA,
		ByteSize:              work.Session.ExpectedBytes,
		TemplateVersion:       work.Session.TemplateVersion,
		MappingDefinitionID:   work.Session.MappingDefinitionID,
		Mapping:               work.Session.Mapping,
		SourceExpiresAt:       &retentionExpiry,
		UpdatePolicy:          work.Session.UpdatePolicy,
		UploadedBy:            work.Session.UploadedBy,
		ClientRequestID:       uploadSessionImportRequestKey(work.Session.ID),
		ContentSignatureValid: true,
	})
	if err != nil {
		return w.failWork(leaseState, err)
	}

	switch {
	case batch.MalwareStatus == MalwareClean && batch.ContentSignatureValid &&
		(batch.Status == ImportValidating || batch.Status == ImportPreviewReady || batch.Status == ImportApproved ||
			batch.Status == ImportImporting || batch.Status == ImportCompleted || batch.Status == ImportCompletedWithExceptions):
		// Exact recovery after the import-side scan committed but the upload
		// session terminal link did not. Do not advance import state again.
	case batch.Status == ImportUploaded || batch.Status == ImportScanning || batch.Status == ImportFailed:
		batch, err = w.Imports.RecordScan(processCtx, batch.ID, MalwareClean, true, "", batch.Version)
		if err != nil {
			return w.failWork(leaseState, err)
		}
	default:
		return w.failWork(leaseState, fmt.Errorf("import %s is in unsafe finalisation recovery state %s", batch.ID, batch.Status))
	}

	select {
	case renewErr := <-heartbeatErr:
		return renewErr
	default:
	}
	session, lease := leaseState.snapshot()
	_, err = w.Repository.CompleteUploadFinalisation(
		processCtx, session.ID, batch.ID, wholeSHA, inspection.MediaType, lease, w.now(),
	)
	if err != nil {
		return err
	}
	return nil
}

func (w *UploadFinalisationWorker) hashSource(ctx context.Context, session UploadSession) (string, error) {
	reader, err := w.Source.Open(ctx, session)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	hash := sha256.New()
	count, err := io.Copy(hash, reader)
	if err != nil {
		return "", fmt.Errorf("hash upload source: %w", err)
	}
	if count != session.ExpectedBytes {
		return "", fmt.Errorf("%w: hashed %d bytes, expected %d", ErrUploadPartIncomplete, count, session.ExpectedBytes)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (w *UploadFinalisationWorker) scanSource(ctx context.Context, session UploadSession) error {
	reader, err := w.Source.Open(ctx, session)
	if err != nil {
		return err
	}
	defer reader.Close()
	counted := &countingReader{Reader: reader}
	result, err := w.Scanner.Scan(ctx, counted)
	if err != nil {
		return fmt.Errorf("malware scan upload source: %w", err)
	}
	if counted.Count != session.ExpectedBytes {
		return fmt.Errorf("malware scanner consumed %d bytes, expected %d", counted.Count, session.ExpectedBytes)
	}
	if result.Infected {
		signature := strings.TrimSpace(result.Signature)
		if signature == "" {
			signature = "unknown malware signature"
		}
		return fmt.Errorf("malware scanner rejected source: %s", signature)
	}
	if !result.Clean {
		return errors.New("malware scanner did not return a clean decision")
	}
	return nil
}

func (w *UploadFinalisationWorker) inspectSource(ctx context.Context, session UploadSession) (FileInspection, error) {
	extension := strings.ToLower(filepath.Ext(strings.TrimSpace(session.OriginalFilename)))
	switch extension {
	case ".csv":
		reader, err := w.Source.Open(ctx, session)
		if err != nil {
			return FileInspection{}, err
		}
		defer reader.Close()
		if err := inspectCSV(reader); err != nil {
			return FileInspection{}, err
		}
		return FileInspection{Format: "CSV", MediaType: "text/csv"}, nil
	case ".xlsx":
		reader, err := w.Source.Open(ctx, session)
		if err != nil {
			return FileInspection{}, err
		}
		defer reader.Close()
		temporary, err := os.CreateTemp(w.TempDir, "campaign-platform-upload-finalise-*.xlsx")
		if err != nil {
			return FileInspection{}, err
		}
		name := temporary.Name()
		defer os.Remove(name)
		defer temporary.Close()
		count, err := io.Copy(temporary, io.LimitReader(reader, session.ExpectedBytes+1))
		if err != nil {
			return FileInspection{}, err
		}
		if count != session.ExpectedBytes {
			return FileInspection{}, fmt.Errorf("%w: copied %d bytes, expected %d", ErrUploadPartIncomplete, count, session.ExpectedBytes)
		}
		if _, err := temporary.Seek(0, io.SeekStart); err != nil {
			return FileInspection{}, err
		}
		return InspectImportFile(session.OriginalFilename, temporary, count)
	default:
		return FileInspection{}, fmt.Errorf("unsupported import extension %q", extension)
	}
}

func (w *UploadFinalisationWorker) failWork(state *finalisationLeaseState, cause error) error {
	session, lease := state.snapshot()
	failCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, failErr := w.Repository.FailUploadFinalisation(failCtx, session.ID, truncate(cause.Error(), 1000), lease, w.now())
	if failErr != nil {
		return errors.Join(cause, fmt.Errorf("record upload finalisation failure: %w", failErr))
	}
	return cause
}

func (w *UploadFinalisationWorker) heartbeat(ctx context.Context, state *finalisationLeaseState, failures chan<- error, done chan<- struct{}) {
	defer close(done)
	duration := w.leaseDuration()
	interval := duration / 3
	if interval < time.Second {
		interval = time.Second
	}
	if interval > 30*time.Second {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			session, lease := state.snapshot()
			renewCtx, cancel := context.WithTimeout(context.Background(), minDuration(interval, 10*time.Second))
			next, renewed, err := w.Repository.RenewUploadFinalisation(renewCtx, session.ID, lease, duration, w.now())
			cancel()
			if err != nil {
				select {
				case failures <- fmt.Errorf("renew upload finalisation lease: %w", err):
				default:
				}
				return
			}
			state.update(next, renewed)
		}
	}
}

type countingReader struct {
	io.Reader
	Count int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.Count += int64(n)
	return n, err
}

type finalisationLeaseState struct {
	mu      sync.Mutex
	session UploadSession
	lease   FinalisationLease
}

func newFinalisationLeaseState(session UploadSession, lease FinalisationLease) *finalisationLeaseState {
	return &finalisationLeaseState{session: cloneUploadSession(session), lease: lease}
}

func (s *finalisationLeaseState) snapshot() (UploadSession, FinalisationLease) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneUploadSession(s.session), s.lease
}

func (s *finalisationLeaseState) update(session UploadSession, lease FinalisationLease) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = cloneUploadSession(session)
	s.lease = lease
}

func uploadSessionImportRequestKey(sessionID string) string {
	return "upload-session:" + strings.TrimSpace(sessionID)
}

func uploadSessionSourceReference(sessionID string) string {
	return "imports/upload-sessions/" + strings.TrimSpace(sessionID) + "/manifest"
}
