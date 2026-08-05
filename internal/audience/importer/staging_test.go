package importer

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	sharedcrypto "campaign-platform/internal/shared/crypto"
)

func importProtector(t *testing.T) *sharedcrypto.MSISDNProtector {
	t.Helper()
	protector, err := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{7}, 32), bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return protector
}

func TestIngestServiceStagesProtectedRowsAndReplaysIdempotently(t *testing.T) {
	csvData := "msisdn,country,state,lga,age,gender\n08012345678,NG,Lagos,Ikeja,27,Female\n+2348012345678,NG,Lagos,Ikeja,27,Female\n08022222222,NG,Lagos,Surulere,35,Male\n"
	repository := NewMemoryStagingRepository()
	service := IngestService{Repository: repository, BatchSize: 1}
	options := PreviewOptions{DefaultCountryISO2: "NG", Protector: importProtector(t), Mapping: ColumnMapping{MSISDN: "msisdn", Country: "country", State: "state", LGA: "lga", Age: "age", Gender: "gender"}}
	first, err := service.Process(context.Background(), "import-1", strings.NewReader(csvData), options)
	if err != nil {
		t.Fatal(err)
	}
	if first.UploadedRows != 3 || first.ValidRows != 2 || first.DuplicateRows != 1 {
		t.Fatalf("unexpected first result: %+v", first)
	}
	second, err := service.Process(context.Background(), "import-1", strings.NewReader(csvData), options)
	if err != nil {
		t.Fatal(err)
	}
	if second.ValidRows != 2 || second.DuplicateRows != 1 {
		t.Fatalf("replay changed durable counts: %+v", second)
	}
	lookup := options.Protector.LookupHMAC("+2348012345678")
	candidate, ok := repository.Candidate("import-1", hex.EncodeToString(lookup))
	if !ok {
		t.Fatal("staged candidate not found")
	}
	if candidate.E164 != "" || len(candidate.EncryptedMSISDN) == 0 || candidate.MaskedMSISDN == "+2348012345678" {
		t.Fatalf("unsafe staged candidate: %+v", candidate)
	}
}

func TestIngestFailureMarksImportFailed(t *testing.T) {
	repository := NewMemoryStagingRepository()
	service := IngestService{Repository: repository}
	_, err := service.Process(context.Background(), "import-1", strings.NewReader("wrong\nvalue\n"), PreviewOptions{Protector: importProtector(t), Mapping: ColumnMapping{MSISDN: "msisdn"}})
	if err == nil {
		t.Fatal("expected missing column error")
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.status["import-1"] != "FAILED" {
		t.Fatalf("status=%s", repository.status["import-1"])
	}
}

func TestValidationLeaseFencesEarlierRun(t *testing.T) {
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryStagingRepository()
	repository.Clock = func() time.Time { return now }
	repository.WorkerID = "validator-1"
	first, err := repository.Begin(context.Background(), "import-lease")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.Begin(context.Background(), "import-lease")
	if err != nil {
		t.Fatal(err)
	}
	candidate := protectedMergeCandidate(7, now)
	if _, err := repository.StageBatch(context.Background(), "import-lease", first, []ContactCandidate{candidate}); !errors.Is(err, ErrImportConflict) {
		t.Fatalf("expected stale validation lease conflict, got %v", err)
	}
	if _, err := repository.StageBatch(context.Background(), "import-lease", second, []ContactCandidate{candidate}); err != nil {
		t.Fatalf("current validation lease was rejected: %v", err)
	}
}

func TestValidationLeaseExpiryBlocksMutation(t *testing.T) {
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryStagingRepository()
	repository.Clock = func() time.Time { return now }
	repository.LeaseDuration = time.Second
	lease, err := repository.Begin(context.Background(), "import-expiry")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if _, err := repository.StageBatch(context.Background(), "import-expiry", lease, []ContactCandidate{protectedMergeCandidate(8, now)}); !errors.Is(err, ErrImportConflict) {
		t.Fatalf("expected expired validation lease conflict, got %v", err)
	}
}

type slowChunkReader struct {
	data  []byte
	chunk int
	delay time.Duration
}

func (r *slowChunkReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	time.Sleep(r.delay)
	limit := r.chunk
	if limit <= 0 || limit > len(r.data) {
		limit = len(r.data)
	}
	if limit > len(p) {
		limit = len(p)
	}
	copy(p, r.data[:limit])
	r.data = r.data[limit:]
	return limit, nil
}

func TestProcessClaimedRenewsLeaseWhileParserProducesNoValidBatch(t *testing.T) {
	repository := NewMemoryStagingRepository()
	repository.LeaseDuration = 250 * time.Millisecond
	lease, err := repository.Begin(context.Background(), "import-heartbeat")
	if err != nil {
		t.Fatal(err)
	}
	protector, err := sharedcrypto.NewMSISDNProtector(make([]byte, 32), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	reader := &slowChunkReader{
		data:  []byte("msisdn,age\ninvalid,not-an-age\nalso-invalid,200\n"),
		chunk: 20,
		delay: 120 * time.Millisecond,
	}
	service := IngestService{Repository: repository, BatchSize: 100}
	result, err := service.ProcessClaimed(context.Background(), "import-heartbeat", lease, reader, PreviewOptions{
		DefaultCountryISO2: "NG", Mapping: ColumnMapping{MSISDN: "msisdn", Age: "age"}, Protector: protector,
	})
	if err != nil {
		t.Fatalf("long invalid-only parse lost its lease: %v", err)
	}
	if result.UploadedRows != 2 || result.InvalidRows != 2 || result.ValidRows != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
}
