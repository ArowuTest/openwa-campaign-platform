package importer

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type StageBatchResult struct {
	Inserted         int
	SourceDuplicates int
	Replayed         int
}

type ImportSummary struct {
	StagedRows       int
	SourceDuplicates int
}

// ValidationLease is a fencing token, not merely an owner label. Every staging
// mutation must present the version returned by Begin. A later Begin fences an
// older process even when both use the same logical worker name.
type ValidationLease struct {
	Owner     string
	Version   int64
	ExpiresAt time.Time
}

type StagingRepository interface {
	Begin(context.Context, string) (ValidationLease, error)
	Renew(context.Context, string, ValidationLease) error
	StageBatch(context.Context, string, ValidationLease, []ContactCandidate) (StageBatchResult, error)
	SaveIssues(context.Context, string, ValidationLease, []RowIssue) error
	Summary(context.Context, string, ValidationLease) (ImportSummary, error)
	CompleteValidation(context.Context, string, ValidationLease, PreviewResult, ImportSummary) error
	Fail(context.Context, string, ValidationLease, error) error
}

type IngestService struct {
	Repository StagingRepository
	BatchSize  int
}

func (s *IngestService) Process(ctx context.Context, importID string, reader io.Reader, options PreviewOptions) (PreviewResult, error) {
	if s == nil || s.Repository == nil {
		return PreviewResult{}, errors.New("staging repository is required")
	}
	if strings.TrimSpace(importID) == "" {
		return PreviewResult{}, errors.New("import ID is required")
	}
	lease, err := s.Repository.Begin(ctx, importID)
	if err != nil {
		return PreviewResult{}, err
	}
	return s.ProcessClaimed(ctx, importID, lease, reader, options)
}

// ProcessClaimed processes an import using an already-acquired fencing lease.
// A separate heartbeat prevents lease expiry while parsing long files that may
// contain no valid rows and therefore do not trigger StageBatch renewals.
func (s *IngestService) ProcessClaimed(ctx context.Context, importID string, lease ValidationLease, reader io.Reader, options PreviewOptions) (PreviewResult, error) {
	if reader == nil {
		return PreviewResult{}, errors.New("import reader is required")
	}
	return s.ProcessClaimedWithProcessor(ctx, importID, lease, func(processCtx context.Context, consumer CandidateConsumer) (PreviewResult, error) {
		return ProcessCSV(processCtx, reader, options, consumer)
	})
}

type CandidateProcessor func(context.Context, CandidateConsumer) (PreviewResult, error)

func (s *IngestService) ProcessClaimedWithProcessor(ctx context.Context, importID string, lease ValidationLease, processor CandidateProcessor) (PreviewResult, error) {
	if s == nil || s.Repository == nil {
		return PreviewResult{}, errors.New("staging repository is required")
	}
	if strings.TrimSpace(importID) == "" || strings.TrimSpace(lease.Owner) == "" || lease.Version <= 0 || lease.ExpiresAt.IsZero() {
		return PreviewResult{}, errors.New("valid import ID and validation lease are required")
	}
	if processor == nil {
		return PreviewResult{}, errors.New("import processor is required")
	}
	processingCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	heartbeatErr := make(chan error, 1)
	heartbeatDone := make(chan struct{})
	go s.heartbeat(processingCtx, importID, lease, heartbeatErr, heartbeatDone)
	defer func() {
		cancel()
		<-heartbeatDone
	}()

	batchSize := s.BatchSize
	if batchSize <= 0 || batchSize > 10_000 {
		batchSize = 1_000
	}
	batch := make([]ContactCandidate, 0, batchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		_, err := s.Repository.StageBatch(ctx, importID, lease, batch)
		for i := range batch {
			batch[i].EncryptedMSISDN = nil
			batch[i].LookupHMAC = nil
		}
		batch = batch[:0]
		return err
	}
	result, err := processor(processingCtx, func(_ context.Context, candidate ContactCandidate) (bool, error) {
		batch = append(batch, candidate)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return false, err
			}
		}
		return false, nil
	})
	if err == nil {
		err = flush()
	}
	select {
	case renewErr := <-heartbeatErr:
		if err == nil {
			err = renewErr
		}
	default:
	}
	if err != nil {
		_ = s.Repository.Fail(context.Background(), importID, lease, err)
		return result, err
	}
	if err := s.Repository.SaveIssues(ctx, importID, lease, result.Issues); err != nil {
		_ = s.Repository.Fail(context.Background(), importID, lease, err)
		return result, err
	}
	summary, err := s.Repository.Summary(ctx, importID, lease)
	if err != nil {
		_ = s.Repository.Fail(context.Background(), importID, lease, err)
		return result, err
	}
	// The durable staging table is the source of truth across resumed runs.
	result.ValidRows = summary.StagedRows
	result.DuplicateRows = summary.SourceDuplicates
	if err := s.Repository.CompleteValidation(ctx, importID, lease, result, summary); err != nil {
		_ = s.Repository.Fail(context.Background(), importID, lease, err)
		return result, err
	}
	return result, nil
}

func (s *IngestService) heartbeat(ctx context.Context, importID string, lease ValidationLease, failures chan<- error, done chan<- struct{}) {
	defer close(done)
	duration := time.Until(lease.ExpiresAt)
	if duration <= 0 {
		select {
		case failures <- ErrImportConflict:
		default:
		}
		return
	}
	interval := duration / 3
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
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
			renewCtx, cancel := context.WithTimeout(context.Background(), minDuration(interval, 10*time.Second))
			err := s.Repository.Renew(renewCtx, importID, lease)
			cancel()
			if err != nil {
				select {
				case failures <- fmt.Errorf("renew validation lease: %w", err):
				default:
				}
				return
			}
		}
	}
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

// MemoryStagingRepository models resumable and exact import staging. It stores
// only protected MSISDN material and applies the same fencing semantics as the
// PostgreSQL implementation.
type MemoryStagingRepository struct {
	mu            sync.Mutex
	imports       map[string]map[string]ContactCandidate
	rows          map[string]map[int]string
	issues        map[string]map[string]RowIssue
	status        map[string]string
	results       map[string]PreviewResult
	failures      map[string]string
	leases        map[string]ValidationLease
	leaseVersions map[string]int64
	WorkerID      string
	LeaseDuration time.Duration
	Clock         func() time.Time
}

func NewMemoryStagingRepository() *MemoryStagingRepository {
	return &MemoryStagingRepository{
		imports: make(map[string]map[string]ContactCandidate), rows: make(map[string]map[int]string),
		issues: make(map[string]map[string]RowIssue), status: make(map[string]string),
		results: make(map[string]PreviewResult), failures: make(map[string]string),
		leases: make(map[string]ValidationLease), leaseVersions: make(map[string]int64),
	}
}

func (r *MemoryStagingRepository) now() time.Time {
	if r.Clock != nil {
		return r.Clock().UTC()
	}
	return time.Now().UTC()
}
func (r *MemoryStagingRepository) leaseDuration() time.Duration {
	if r.LeaseDuration <= 0 {
		return 2 * time.Minute
	}
	return r.LeaseDuration
}
func (r *MemoryStagingRepository) workerID() string {
	if strings.TrimSpace(r.WorkerID) == "" {
		return "memory-validation-worker"
	}
	return strings.TrimSpace(r.WorkerID)
}
func (r *MemoryStagingRepository) Begin(_ context.Context, importID string) (ValidationLease, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.imports[importID] == nil {
		r.imports[importID] = make(map[string]ContactCandidate)
		r.rows[importID] = make(map[int]string)
		r.issues[importID] = make(map[string]RowIssue)
	}
	r.leaseVersions[importID]++
	lease := ValidationLease{Owner: r.workerID(), Version: r.leaseVersions[importID], ExpiresAt: r.now().Add(r.leaseDuration())}
	r.leases[importID] = lease
	r.status[importID] = "VALIDATING"
	return lease, nil
}
func (r *MemoryStagingRepository) Renew(_ context.Context, importID string, lease ValidationLease) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.validateLease(importID, lease)
}

func (r *MemoryStagingRepository) validateLease(importID string, lease ValidationLease) error {
	current, ok := r.leases[importID]
	if !ok || current.Owner != lease.Owner || current.Version != lease.Version || !current.ExpiresAt.After(r.now()) {
		return ErrImportConflict
	}
	current.ExpiresAt = r.now().Add(r.leaseDuration())
	r.leases[importID] = current
	return nil
}
func (r *MemoryStagingRepository) StageBatch(_ context.Context, importID string, lease ValidationLease, candidates []ContactCandidate) (StageBatchResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.validateLease(importID, lease); err != nil {
		return StageBatchResult{}, err
	}
	items := r.imports[importID]
	if items == nil {
		return StageBatchResult{}, errors.New("import has not begun")
	}
	result := StageBatchResult{}
	for _, candidate := range candidates {
		if candidate.E164 != "" || len(candidate.EncryptedMSISDN) == 0 || len(candidate.LookupHMAC) == 0 {
			return StageBatchResult{}, errors.New("staging accepts protected candidates only")
		}
		lookup := hex.EncodeToString(candidate.LookupHMAC)
		if existingLookup, ok := r.rows[importID][candidate.RowNumber]; ok {
			if existingLookup != lookup {
				return StageBatchResult{}, fmt.Errorf("row %d changed during import replay", candidate.RowNumber)
			}
			result.Replayed++
			continue
		}
		if first, ok := items[lookup]; ok {
			key := issueKey(candidate.RowNumber, "DUPLICATE_MSISDN")
			if _, exists := r.issues[importID][key]; !exists {
				r.issues[importID][key] = RowIssue{RowNumber: candidate.RowNumber, Field: "msisdn", Code: "DUPLICATE_MSISDN", Message: fmt.Sprintf("duplicates row %d", first.RowNumber)}
				result.SourceDuplicates++
			}
			r.rows[importID][candidate.RowNumber] = lookup
			continue
		}
		stored := cloneCandidate(candidate)
		stored.E164 = ""
		items[lookup] = stored
		r.rows[importID][candidate.RowNumber] = lookup
		result.Inserted++
	}
	return result, nil
}
func (r *MemoryStagingRepository) SaveIssues(_ context.Context, importID string, lease ValidationLease, issues []RowIssue) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.validateLease(importID, lease); err != nil {
		return err
	}
	if r.issues[importID] == nil {
		return errors.New("import has not begun")
	}
	for _, issue := range issues {
		r.issues[importID][issueKey(issue.RowNumber, issue.Code)] = issue
	}
	return nil
}
func (r *MemoryStagingRepository) Summary(_ context.Context, importID string, lease ValidationLease) (ImportSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.validateLease(importID, lease); err != nil {
		return ImportSummary{}, err
	}
	if r.imports[importID] == nil {
		return ImportSummary{}, errors.New("import not found")
	}
	duplicates := 0
	for _, issue := range r.issues[importID] {
		if issue.Code == "DUPLICATE_MSISDN" {
			duplicates++
		}
	}
	return ImportSummary{StagedRows: len(r.imports[importID]), SourceDuplicates: duplicates}, nil
}
func (r *MemoryStagingRepository) CompleteValidation(_ context.Context, importID string, lease ValidationLease, result PreviewResult, _ ImportSummary) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.validateLease(importID, lease); err != nil {
		return err
	}
	r.status[importID] = "PREVIEW_READY"
	result.Candidates = nil
	r.results[importID] = result
	delete(r.leases, importID)
	return nil
}
func (r *MemoryStagingRepository) Fail(_ context.Context, importID string, lease ValidationLease, cause error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.validateLease(importID, lease); err != nil {
		return err
	}
	r.status[importID] = "FAILED"
	if cause != nil {
		r.failures[importID] = cause.Error()
	}
	delete(r.leases, importID)
	return nil
}
func (r *MemoryStagingRepository) Candidate(importID, lookupHex string) (ContactCandidate, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.imports[importID][lookupHex]
	return cloneCandidate(value), ok
}
func issueKey(row int, code string) string { return fmt.Sprintf("%d:%s", row, code) }
