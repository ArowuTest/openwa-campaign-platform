package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	sharedcrypto "campaign-platform/internal/shared/crypto"
	"campaign-platform/internal/storage"
)

func TestValidationWorkerStreamsResumableUploadComposite(t *testing.T) {
	now := time.Date(2099, 10, 6, 12, 0, 0, 0, time.UTC)
	store, err := storage.NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	uploads := NewMemoryUploadSessionRepository()
	payload := []byte("msisdn,country\n08012345678,NG\n08087654321,NG\n")
	session := buildUploadedSessionForFinaliser(t, store, uploads, now, payload, "validation-composite-request-0001")
	claimed, err := uploads.ClaimReadyFinalisations(context.Background(), "finaliser", 5*time.Minute, 1, now.Add(3*time.Minute))
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v items=%d", err, len(claimed))
	}
	sum := sha256.Sum256(payload)
	wholeSHA := hex.EncodeToString(sum[:])
	importID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	session, err = uploads.CompleteUploadFinalisation(context.Background(), session.ID, importID, wholeSHA, "text/csv", claimed[0].Lease, now.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	staging := NewMemoryStagingRepository()
	staging.Clock = func() time.Time { return now.Add(5 * time.Minute) }
	staging.LeaseDuration = 5 * time.Minute
	lease, err := staging.Begin(context.Background(), importID)
	if err != nil {
		t.Fatal(err)
	}
	protector, err := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{41}, 32), bytes.Repeat([]byte{42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	mapping, _ := json.Marshal(ColumnMapping{MSISDN: "msisdn", Country: "country"})
	work := ValidationWork{
		ImportID:           importID,
		ObjectKey:          uploadSessionSourceReference(session.ID),
		UploadSessionID:    session.ID,
		OriginalFilename:   "audience.csv",
		DetectedMediaType:  "text/csv",
		FileSHA256:         wholeSHA,
		ByteSize:           int64(len(payload)),
		DefaultCountryISO2: "NG",
		Mapping:            mapping,
		Lease:              lease,
	}
	worker := &ValidationWorker{
		Staging:        staging,
		Store:          store,
		UploadSessions: uploads,
		Ingest:         &IngestService{Repository: staging, BatchSize: 1},
		Options: PreviewOptions{
			DefaultCountryISO2: "NG",
			MaxRows:            2_000_000,
			Protector:          protector,
			MaxCandidateSample: 1,
		},
	}
	if err := worker.process(context.Background(), work); err != nil {
		t.Fatalf("process composite: %v", err)
	}
	if got := len(staging.imports[importID]); got != 2 {
		t.Fatalf("durable staged rows=%d want=2", got)
	}
	if got := len(staging.issues[importID]); got != 0 {
		t.Fatalf("unexpected durable issues=%d", got)
	}
}

func TestValidationWorkerRejectsUploadSessionNotTerminallyLinked(t *testing.T) {
	now := time.Date(2099, 10, 6, 12, 0, 0, 0, time.UTC)
	store, _ := storage.NewFileSystemStore(t.TempDir())
	uploads := NewMemoryUploadSessionRepository()
	payload := []byte("msisdn\n08012345678\n")
	session := buildUploadedSessionForFinaliser(t, store, uploads, now, payload, "validation-composite-request-0002")
	staging := NewMemoryStagingRepository()
	staging.Clock = func() time.Time { return now.Add(5 * time.Minute) }
	lease, _ := staging.Begin(context.Background(), "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	work := ValidationWork{
		ImportID:          "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		ObjectKey:         uploadSessionSourceReference(session.ID),
		UploadSessionID:   session.ID,
		OriginalFilename:  "audience.csv",
		DetectedMediaType: "text/csv",
		FileSHA256:        hex.EncodeToString(sha256.New().Sum(nil)),
		ByteSize:          int64(len(payload)),
		Mapping:           json.RawMessage(`{"msisdn":"msisdn"}`),
		Lease:             lease,
	}
	worker := &ValidationWorker{Staging: staging, Store: store, UploadSessions: uploads, Ingest: &IngestService{Repository: staging}}
	if err := worker.process(context.Background(), work); err == nil {
		t.Fatal("uploaded-but-not-import-created session was accepted for validation")
	}
}
