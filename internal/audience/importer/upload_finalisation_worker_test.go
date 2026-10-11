package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"campaign-platform/internal/security/malware"
	"campaign-platform/internal/storage"
)

func buildUploadedSessionForFinaliser(t *testing.T, store storage.ObjectStore, repo *MemoryUploadSessionRepository, now time.Time, payload []byte, requestKey string) UploadSession {
	t.Helper()
	input := validUploadSessionInput()
	input.ClientRequestID = requestKey
	input.ExpectedBytes = int64(len(payload))
	session, err := NewUploadSession(input, DefaultUploadPartSize, 2<<30, now.Add(24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := store.Put(context.Background(), session.Parts[0].ObjectKey, bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	session, _, err = session.RecordPart(1, meta.Size, meta.SHA256, session.Version, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	session, err = session.Complete(session.Version, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.Create(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	return session
}

func TestUploadFinalisationWorkerCreatesScannedImportFromCompositeSource(t *testing.T) {
	now := time.Date(2026, 10, 6, 11, 30, 0, 0, time.UTC)
	store, err := storage.NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	uploads := NewMemoryUploadSessionRepository()
	payload := []byte("msisdn,country\n08012345678,NG\n08087654321,NG\n")
	session := buildUploadedSessionForFinaliser(t, store, uploads, now, payload, "finaliser-worker-request-0001")
	imports := NewMemoryImportRepository()
	scanner := &fakeMalwareScanner{result: malware.Result{Clean: true}}
	worker := &UploadFinalisationWorker{
		Repository:      uploads,
		Source:          &UploadCompositeSource{Store: store},
		Scanner:         scanner,
		Imports:         &ImportService{Repository: imports},
		WorkerID:        "upload-finaliser-test",
		LeaseDuration:   5 * time.Minute,
		ClaimBatch:      1,
		SourceRetention: 30 * 24 * time.Hour,
		Clock:           func() time.Time { return now.Add(3 * time.Minute) },
	}
	processed, err := worker.Process(context.Background())
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed=%d want=1", processed)
	}
	final, err := uploads.Get(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.State != UploadSessionImportCreated || final.LinkedImportID == "" {
		t.Fatalf("unexpected final session: %+v", final)
	}
	sum := sha256.Sum256(payload)
	wantSHA := hex.EncodeToString(sum[:])
	if final.FinalSHA256 != wantSHA || final.DetectedMediaType != "text/csv" {
		t.Fatalf("final evidence sha=%q media=%q", final.FinalSHA256, final.DetectedMediaType)
	}
	batch, err := imports.Get(context.Background(), final.LinkedImportID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.UploadSessionID != session.ID {
		t.Fatalf("upload lineage missing: %+v", batch)
	}
	if batch.FileSHA256 != wantSHA || batch.ByteSize != int64(len(payload)) {
		t.Fatalf("import file evidence mismatch: %+v", batch)
	}
	if batch.Status != ImportValidating || batch.MalwareStatus != MalwareClean || !batch.ContentSignatureValid {
		t.Fatalf("import not clean/validating: %+v", batch)
	}
	if scanner.calls != 1 {
		t.Fatalf("scanner calls=%d want=1", scanner.calls)
	}
}

func TestUploadFinalisationWorkerInfectedSourceFailsWithoutImport(t *testing.T) {
	now := time.Date(2026, 10, 6, 11, 30, 0, 0, time.UTC)
	store, _ := storage.NewFileSystemStore(t.TempDir())
	uploads := NewMemoryUploadSessionRepository()
	session := buildUploadedSessionForFinaliser(t, store, uploads, now, []byte("msisdn\n08012345678\n"), "finaliser-worker-request-0002")
	imports := NewMemoryImportRepository()
	worker := &UploadFinalisationWorker{
		Repository:    uploads,
		Source:        &UploadCompositeSource{Store: store},
		Scanner:       &fakeMalwareScanner{result: malware.Result{Infected: true, Signature: "EICAR-Test-Signature"}},
		Imports:       &ImportService{Repository: imports},
		WorkerID:      "upload-finaliser-test",
		LeaseDuration: 5 * time.Minute,
		ClaimBatch:    1,
		Clock:         func() time.Time { return now.Add(3 * time.Minute) },
	}
	processed, err := worker.Process(context.Background())
	if err == nil {
		t.Fatal("infected source should return a processing error")
	}
	if processed != 0 {
		t.Fatalf("processed=%d want=0", processed)
	}
	final, getErr := uploads.Get(context.Background(), session.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if final.State != UploadSessionFailed || final.FailureReason == "" {
		t.Fatalf("infected upload not terminal failed: %+v", final)
	}
	if len(imports.items) != 0 {
		t.Fatalf("infected source created imports: %d", len(imports.items))
	}
}
