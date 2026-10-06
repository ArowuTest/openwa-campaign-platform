package importer

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

func TestLargeAudiencePostgreSQLStagingProfile(t *testing.T) {
	if os.Getenv("RUN_LARGE_AUDIENCE_PG_BENCHMARK") != "1" {
		t.Skip("set RUN_LARGE_AUDIENCE_PG_BENCHMARK=1 to run the PostgreSQL staging profile")
	}
	dsn := os.Getenv("POSTGRES_IMPORT_GOVERNANCE_DATABASE_URL")
	if dsn == "" {
		t.Fatal("POSTGRES_IMPORT_GOVERNANCE_DATABASE_URL is required")
	}
	rows := benchmarkIntEnv(t, "LARGE_AUDIENCE_ROWS", 100_000, 1, 2_000_000)
	batchSize := benchmarkIntEnv(t, "LARGE_AUDIENCE_BATCH_SIZE", 1_000, 1, 10_000)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var databaseBytesBefore int64
	if err := db.QueryRowContext(ctx, `SELECT pg_database_size(current_database())`).Scan(&databaseBytesBefore); err != nil {
		t.Fatal(err)
	}

	ids := make([]string, 4)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	makerID, orgID, reviewID, purposeID := ids[0], ids[1], ids[2], ids[3]
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Load Maker','DISABLED',false)`, makerID, "load-"+makerID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Load "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,reviewed_by,reviewed_at,expires_at,outcome) VALUES($1::uuid,$2::uuid,'Load review','WHATSAPP','DIRECT','v1',true,true,true,'APPROVED',$3::uuid,$4,$5,'APPROVED')`, reviewID, orgID, makerID, now.Add(-time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Load purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "LOAD_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}

	importRepo := &PostgreSQLImportRepository{DB: db}
	importService := &ImportService{Repository: importRepo, Clock: func() time.Time { return now }}
	batch, created, err := importService.Create(ctx, CreateImportInput{
		OrganisationID: orgID, ConsentReviewID: reviewID, PurposeID: purposeID,
		Channel: "WHATSAPP", WordingVersion: "v1", SourceName: "synthetic load profile",
		SourceSystem: "benchmark", DefaultCountryISO2: "NG",
		ObjectKey: "benchmark/synthetic.csv", OriginalFilename: "synthetic.csv",
		DetectedMediaType: "text/csv", FileSHA256: fmt.Sprintf("%064x", rows),
		ByteSize: int64(rows) * 15, TemplateVersion: "v1",
		Mapping:      ColumnMapping{MSISDN: "msisdn", Country: "country"},
		UpdatePolicy: UpdateNewestSource, UploadedBy: makerID,
		ClientRequestID:       fmt.Sprintf("large-pg-benchmark-%d-%d-%d", rows, batchSize, time.Now().UnixNano()),
		ContentSignatureValid: true,
	})
	if err != nil || !created {
		t.Fatalf("create import: created=%v err=%v", created, err)
	}
	batch, err = importService.RecordScan(ctx, batch.ID, MalwareClean, true, "", batch.Version)
	if err != nil {
		t.Fatal(err)
	}

	staging := &PostgreSQLStagingRepository{
		DB: db, WorkerID: "large-pg-benchmark", LeaseDuration: 5 * time.Minute,
	}
	lease, err := staging.Begin(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	protector, err := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{71}, 32), bytes.Repeat([]byte{72}, 32))
	if err != nil {
		t.Fatal(err)
	}
	reader, generatedBytes, generatorDone := syntheticAudienceCSV(rows)

	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	var peakHeap atomic.Uint64
	peakHeap.Store(baseline.HeapAlloc)
	sampleStop := make(chan struct{})
	sampleDone := make(chan struct{})
	go sampleHeap(&peakHeap, sampleStop, sampleDone)

	started := time.Now()
	result, err := (&IngestService{Repository: staging, BatchSize: batchSize}).ProcessClaimed(ctx, batch.ID, lease, reader, PreviewOptions{
		DefaultCountryISO2: "NG", MaxRows: rows, MaxIssues: 1_000,
		MaxCandidateSample: 1, Protector: protector,
		Mapping: ColumnMapping{MSISDN: "msisdn", Country: "country"},
	})
	elapsed := time.Since(started)
	close(sampleStop)
	<-sampleDone
	if generatorErr := <-generatorDone; generatorErr != nil {
		t.Fatalf("generate source: %v", generatorErr)
	}
	if err != nil {
		t.Fatalf("stage source: %v", err)
	}
	if result.UploadedRows != rows || result.ValidRows != rows || result.InvalidRows != 0 || result.DuplicateRows != 0 {
		t.Fatalf("accounting uploaded=%d valid=%d invalid=%d duplicate=%d", result.UploadedRows, result.ValidRows, result.InvalidRows, result.DuplicateRows)
	}
	var stagedRows int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audience_import_staging WHERE audience_import_id=$1::uuid`, batch.ID).Scan(&stagedRows); err != nil {
		t.Fatal(err)
	}
	if stagedRows != int64(rows) {
		t.Fatalf("staged rows=%d want=%d", stagedRows, rows)
	}
	var databaseBytesAfter int64
	if err := db.QueryRowContext(ctx, `SELECT pg_database_size(current_database())`).Scan(&databaseBytesAfter); err != nil {
		t.Fatal(err)
	}
	peak := peakHeap.Load()
	var peakDelta uint64
	if peak > baseline.HeapAlloc {
		peakDelta = peak - baseline.HeapAlloc
	}
	const maxHeapDelta = 256 << 20
	if peakDelta > maxHeapDelta {
		t.Fatalf("PostgreSQL staging peak heap delta=%d exceeds threshold=%d", peakDelta, maxHeapDelta)
	}
	t.Logf("PostgreSQL staging result: rows=%d batch=%d bytes=%d elapsed=%s rows_per_second=%.0f baseline_heap=%d peak_heap=%d peak_delta=%d database_growth=%d",
		rows, batchSize, generatedBytes.Load(), elapsed, float64(rows)/elapsed.Seconds(), baseline.HeapAlloc, peak, peakDelta, databaseBytesAfter-databaseBytesBefore)
}

func syntheticAudienceCSV(rows int) (io.Reader, *atomic.Int64, <-chan error) {
	reader, writer := io.Pipe()
	var generatedBytes atomic.Int64
	done := make(chan error, 1)
	go func() {
		buffered := bufio.NewWriterSize(writer, 256<<10)
		counter := &countingBenchmarkWriter{Writer: buffered, Count: &generatedBytes}
		if _, err := io.WriteString(counter, "msisdn,country\n"); err != nil {
			_ = writer.CloseWithError(err)
			done <- err
			return
		}
		for i := 0; i < rows; i++ {
			if _, err := fmt.Fprintf(counter, "0%d,NG\n", int64(8_000_000_000)+int64(i)); err != nil {
				_ = writer.CloseWithError(err)
				done <- err
				return
			}
		}
		if err := buffered.Flush(); err != nil {
			_ = writer.CloseWithError(err)
			done <- err
			return
		}
		done <- writer.Close()
	}()
	return reader, &generatedBytes, done
}

func sampleHeap(peak *atomic.Uint64, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			for {
				current := peak.Load()
				if stats.HeapAlloc <= current || peak.CompareAndSwap(current, stats.HeapAlloc) {
					break
				}
			}
		}
	}
}

func benchmarkIntEnv(t *testing.T, name string, fallback, minimum, maximum int) int {
	t.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		t.Fatalf("%s must be an integer between %d and %d", name, minimum, maximum)
	}
	return value
}
