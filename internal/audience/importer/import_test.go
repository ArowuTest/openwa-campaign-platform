package importer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func validCreateImportInput() CreateImportInput {
	return CreateImportInput{
		OrganisationID: "org-1", ConsentReviewID: "review-1", PurposeID: "purpose-1", Channel: "WHATSAPP", WordingVersion: "v1",
		SourceName: "event registrations", SourceSystem: "registration-portal", DefaultCountryISO2: "NG",
		ObjectKey: "imports/2026/08/object.csv", OriginalFilename: "audience.csv", DetectedMediaType: "text/csv",
		FileSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ByteSize: 1024,
		TemplateVersion: "audience-v1", Mapping: ColumnMapping{MSISDN: "phone", Country: "country"},
		UpdatePolicy: UpdateNewestSource, UploadedBy: "maker-1", ClientRequestID: "import-request-0000001", ContentSignatureValid: true,
	}
}

func TestImportCreationReplayIsExactAndConcurrent(t *testing.T) {
	repository := NewMemoryImportRepository()
	service := ImportService{Repository: repository, Clock: func() time.Time { return time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC) }}
	input := validCreateImportInput()
	const callers = 12
	results := make(chan ImportBatch, callers)
	errorsSeen := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			batch, _, err := service.Create(context.Background(), input)
			results <- batch
			errorsSeen <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	var id string
	for value := range results {
		if id == "" {
			id = value.ID
		}
		if value.ID != id {
			t.Fatalf("replay created multiple imports: %q and %q", id, value.ID)
		}
	}
	if len(repository.items) != 1 {
		t.Fatalf("expected one import, got %d", len(repository.items))
	}
}

func TestImportRejectsRequestKeyPayloadMismatchAndDuplicateFile(t *testing.T) {
	repository := NewMemoryImportRepository()
	service := ImportService{Repository: repository}
	input := validCreateImportInput()
	first, _, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.SourceName = "different source"
	if _, _, err := service.Create(context.Background(), input); !errors.Is(err, ErrImportReplayConflict) {
		t.Fatalf("expected replay conflict, got %v", err)
	}
	input = validCreateImportInput()
	input.ClientRequestID = "import-request-0000002"
	duplicate, _, err := service.Create(context.Background(), input)
	if !errors.Is(err, ErrDuplicateImportFile) || duplicate.ID != first.ID {
		t.Fatalf("expected duplicate file reference, batch=%+v err=%v", duplicate, err)
	}
}

func TestImportScanAndMakerCheckerApproval(t *testing.T) {
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryImportRepository()
	service := ImportService{Repository: repository, Clock: func() time.Time { return now }}
	batch, _, err := service.Create(context.Background(), validCreateImportInput())
	if err != nil {
		t.Fatal(err)
	}
	batch, err = service.RecordScan(context.Background(), batch.ID, MalwareClean, true, "", batch.Version)
	if err != nil {
		t.Fatal(err)
	}
	batch, err = repository.SetPreviewReady(batch.ID, PreviewResult{UploadedRows: 10, ValidRows: 8, InvalidRows: 1, DuplicateRows: 1}, batch.Version, now)
	if err != nil {
		t.Fatal(err)
	}
	repository.SetConsentEligible(batch.ConsentReviewID, true)
	if _, err := service.Approve(context.Background(), batch.ID, batch.UploadedBy, batch.Version); !errors.Is(err, ErrMakerChecker) {
		t.Fatalf("expected maker checker, got %v", err)
	}
	approved, err := service.Approve(context.Background(), batch.ID, "checker-1", batch.Version)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != ImportApproved || approved.ApprovedBy != "checker-1" || approved.Version != batch.Version+1 {
		t.Fatalf("unexpected approval: %+v", approved)
	}
	// An ambiguous network retry returns the original committed approval.
	replayed, err := service.Approve(context.Background(), batch.ID, "checker-1", batch.Version)
	if err != nil || replayed.Version != approved.Version || replayed.ApprovedBy != approved.ApprovedBy {
		t.Fatalf("approval replay changed evidence: replay=%+v err=%v", replayed, err)
	}
	if _, err := service.Approve(context.Background(), batch.ID, "different-checker", batch.Version); !errors.Is(err, ErrImportConflict) {
		t.Fatalf("expected conflicting approval replay, got %v", err)
	}
}

func TestInfectedOrDisguisedImportIsQuarantined(t *testing.T) {
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	batch, err := NewImportBatch(validCreateImportInput(), now)
	if err != nil {
		t.Fatal(err)
	}
	quarantined, err := batch.Scan(MalwareInfected, true, "EICAR signature", batch.Version, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if quarantined.Status != ImportQuarantined || quarantined.FailureReason == "" {
		t.Fatalf("infected import not quarantined: %+v", quarantined)
	}
	batch, _ = NewImportBatch(validCreateImportInput(), now)
	quarantined, err = batch.Scan(MalwareClean, false, "extension/signature mismatch", batch.Version, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if quarantined.Status != ImportQuarantined {
		t.Fatalf("disguised file not quarantined: %+v", quarantined)
	}
}

func TestImportScanReplayIsExact(t *testing.T) {
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryImportRepository()
	service := ImportService{Repository: repository, Clock: func() time.Time { return now }}
	batch, _, err := service.Create(context.Background(), validCreateImportInput())
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.RecordScan(context.Background(), batch.ID, MalwareClean, true, "", batch.Version)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.RecordScan(context.Background(), batch.ID, MalwareClean, true, "", batch.Version)
	if err != nil || replay.Version != first.Version || replay.Status != first.Status {
		t.Fatalf("scan replay changed evidence: first=%+v replay=%+v err=%v", first, replay, err)
	}
	if _, err := service.RecordScan(context.Background(), batch.ID, MalwareFailed, false, "scanner unavailable", batch.Version); !errors.Is(err, ErrImportConflict) {
		t.Fatalf("expected scan replay conflict, got %v", err)
	}
}
