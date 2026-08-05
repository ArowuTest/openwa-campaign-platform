package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sharedcrypto "campaign-platform/internal/shared/crypto"
	"campaign-platform/internal/storage"
)

func TestValidationWorkerProcessesImmutableCSVUnderClaimedLease(t *testing.T) {
	store, err := storage.NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	csv := "msisdn,country,state,lga,age,gender\n08012345678,NG,Lagos,Ikeja,25,F\n"
	metadata, err := store.Put(context.Background(), "imports/quarantine/test.csv", strings.NewReader(csv), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewMemoryStagingRepository()
	lease, err := repository.Begin(context.Background(), "import-1")
	if err != nil {
		t.Fatal(err)
	}
	protector, err := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	worker := ValidationWorker{
		Staging: repository, Store: store,
		Ingest: &IngestService{Repository: repository, BatchSize: 10},
		Options: PreviewOptions{Protector: protector, MaxRows: 1000, GeographyValidator: func(country, state, lga string) error {
			if country != "NG" || state != "Lagos" || lga != "Ikeja" {
				return errors.New("unexpected geography")
			}
			return nil
		}},
	}
	work := ValidationWork{
		ImportID: "import-1", ObjectKey: metadata.Key, OriginalFilename: "audience.csv",
		DetectedMediaType: "text/csv", FileSHA256: metadata.SHA256, ByteSize: metadata.Size,
		DefaultCountryISO2: "NG", Mapping: json.RawMessage(`{"msisdn":"msisdn","country":"country","state":"state","lga":"lga","age":"age","gender":"gender"}`),
		Lease: lease,
	}
	if err := worker.process(context.Background(), work); err != nil {
		t.Fatal(err)
	}
	if repository.status["import-1"] != "PREVIEW_READY" {
		t.Fatalf("unexpected import status %q", repository.status["import-1"])
	}
	if repository.results["import-1"].ValidRows != 1 {
		t.Fatalf("unexpected result: %#v", repository.results["import-1"])
	}
	for _, candidate := range repository.imports["import-1"] {
		if candidate.E164 != "" || len(candidate.EncryptedMSISDN) == 0 || len(candidate.LookupHMAC) == 0 {
			t.Fatalf("raw or unprotected identifier crossed staging boundary: %#v", candidate)
		}
	}
}

func TestValidationWorkerQuarantinedEvidenceMismatchFailsClosed(t *testing.T) {
	store, err := storage.NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := store.Put(context.Background(), "imports/quarantine/test.csv", strings.NewReader("msisdn\n08012345678\n"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewMemoryStagingRepository()
	lease, _ := repository.Begin(context.Background(), "import-2")
	worker := ValidationWorker{Staging: repository, Store: store, Ingest: &IngestService{Repository: repository}}
	err = worker.process(context.Background(), ValidationWork{
		ImportID: "import-2", ObjectKey: metadata.Key, DetectedMediaType: "text/csv",
		FileSHA256: strings.Repeat("0", 64), ByteSize: metadata.Size, Mapping: json.RawMessage(`{"msisdn":"msisdn"}`), Lease: lease,
	})
	if err == nil || !strings.Contains(err.Error(), "immutable file evidence") {
		t.Fatalf("expected evidence mismatch, got %v", err)
	}
	if repository.status["import-2"] != "FAILED" {
		t.Fatalf("evidence mismatch was not persisted as failure: %q", repository.status["import-2"])
	}
}

func TestDecodeColumnMappingRejectsUnknownAndTrailingFields(t *testing.T) {
	if _, err := decodeColumnMapping(json.RawMessage(`{"msisdn":"phone","unexpected":true}`)); err == nil {
		t.Fatal("unknown mapping field accepted")
	}
	if _, err := decodeColumnMapping(json.RawMessage(`{"msisdn":"phone"} {"msisdn":"other"}`)); err == nil {
		t.Fatal("trailing mapping object accepted")
	}
}

type oneShotValidationClaimer struct {
	items []ValidationWork
}

func (c *oneShotValidationClaimer) ClaimReady(ctx context.Context, limit int) ([]ValidationWork, error) {
	if len(c.items) == 0 {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	items := c.items
	if len(items) > limit {
		items = items[:limit]
	}
	c.items = c.items[len(items):]
	return items, nil
}

func TestValidationWorkerRunUsesBoundedOwnership(t *testing.T) {
	store, _ := storage.NewFileSystemStore(t.TempDir())
	metadata, _ := store.Put(context.Background(), "imports/quarantine/test.csv", strings.NewReader("msisdn\ninvalid\n"), 1<<20)
	repository := NewMemoryStagingRepository()
	lease, _ := repository.Begin(context.Background(), "import-3")
	protector, _ := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32))
	ctx, cancel := context.WithCancel(context.Background())
	worker := ValidationWorker{
		Claimer: &oneShotValidationClaimer{items: []ValidationWork{{
			ImportID: "import-3", ObjectKey: metadata.Key, DetectedMediaType: "text/csv", FileSHA256: metadata.SHA256,
			ByteSize: metadata.Size, DefaultCountryISO2: "NG", Mapping: json.RawMessage(`{"msisdn":"msisdn"}`), Lease: lease,
		}}},
		Staging: repository, Store: store, Ingest: &IngestService{Repository: repository},
		Options: PreviewOptions{Protector: protector}, Concurrency: 1, ClaimBatch: 1, PollInterval: time.Millisecond,
		OnError: func(work ValidationWork, err error) {
			if work.ImportID != "" {
				cancel()
			}
		},
	}
	// The invalid row is a valid completed validation, so cancel after observing status.
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	deadline := time.After(2 * time.Second)
	for {
		repository.mu.Lock()
		status := repository.status["import-3"]
		repository.mu.Unlock()
		if status == "PREVIEW_READY" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("worker did not finish claimed import")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if worker.Active() != 0 {
		t.Fatalf("worker leaked active tasks: %d", worker.Active())
	}
}
