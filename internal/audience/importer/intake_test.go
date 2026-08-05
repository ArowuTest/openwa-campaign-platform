package importer

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"campaign-platform/internal/security/malware"
	"campaign-platform/internal/storage"
)

type fakeMalwareScanner struct {
	mu     sync.Mutex
	result malware.Result
	err    error
	calls  int
}

func (s *fakeMalwareScanner) Scan(_ context.Context, reader io.Reader) (malware.Result, error) {
	_, _ = io.Copy(io.Discard, reader)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.result, s.err
}

func validIntakeInput() IntakeInput {
	return IntakeInput{
		OrganisationID: "org-1", ConsentReviewID: "review-1", PurposeID: "purpose-1",
		Channel: "WHATSAPP", WordingVersion: "v1", SourceName: "event registrations",
		OriginalFilename: "audience.csv", TemplateVersion: "audience-v1",
		Mapping: ColumnMapping{MSISDN: "msisdn", Country: "country"}, UpdatePolicy: UpdateNewestSource,
		UploadedBy: "maker-1", ClientRequestID: "intake-request-000001",
	}
}

func TestIntakeStoresScansAndReplaysWithoutDuplicateImport(t *testing.T) {
	store, err := storage.NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repository := NewMemoryImportRepository()
	scanner := &fakeMalwareScanner{result: malware.Result{Clean: true}}
	service := IntakeService{Store: store, Scanner: scanner, Imports: &ImportService{Repository: repository}, MaxFileSize: 1024}
	payload := "msisdn,country\n08012345678,NG\n"
	first, created, err := service.Intake(context.Background(), validIntakeInput(), strings.NewReader(payload))
	if err != nil || !created || first.Status != ImportValidating || first.MalwareStatus != MalwareClean {
		t.Fatalf("unexpected intake: batch=%+v created=%t err=%v", first, created, err)
	}
	replay, created, err := service.Intake(context.Background(), validIntakeInput(), strings.NewReader(payload))
	if err != nil || created || replay.ID != first.ID || replay.Version != first.Version {
		t.Fatalf("intake replay changed evidence: first=%+v replay=%+v created=%t err=%v", first, replay, created, err)
	}
	if len(repository.items) != 1 {
		t.Fatalf("expected one import, got %d", len(repository.items))
	}
}

func TestIntakeQuarantinesDisguisedOrInfectedFile(t *testing.T) {
	store, _ := storage.NewFileSystemStore(t.TempDir())
	repository := NewMemoryImportRepository()
	scanner := &fakeMalwareScanner{result: malware.Result{Clean: true}}
	service := IntakeService{Store: store, Scanner: scanner, Imports: &ImportService{Repository: repository}}
	batch, _, err := service.Intake(context.Background(), validIntakeInput(), strings.NewReader("\x00binary"))
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != ImportQuarantined || batch.ContentSignatureValid {
		t.Fatalf("disguised file was not quarantined: %+v", batch)
	}

	input := validIntakeInput()
	input.ClientRequestID = "intake-request-000002"
	input.OriginalFilename = "infected.csv"
	scanner.result = malware.Result{Infected: true, Signature: "Eicar-Signature"}
	batch, _, err = service.Intake(context.Background(), input, strings.NewReader("msisdn\n08012345678\n"))
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != ImportQuarantined || batch.MalwareStatus != MalwareInfected || batch.FailureReason != "Eicar-Signature" {
		t.Fatalf("infected file was not quarantined: %+v", batch)
	}
}

func TestIntakeRejectsDifferentPayloadForSameRequest(t *testing.T) {
	store, _ := storage.NewFileSystemStore(t.TempDir())
	service := IntakeService{Store: store, Scanner: &fakeMalwareScanner{result: malware.Result{Clean: true}}, Imports: &ImportService{Repository: NewMemoryImportRepository()}}
	input := validIntakeInput()
	if _, _, err := service.Intake(context.Background(), input, strings.NewReader("msisdn\n08012345678\n")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Intake(context.Background(), input, strings.NewReader("msisdn\n08099999999\n")); !errors.Is(err, storage.ErrKeyConflict) {
		t.Fatalf("expected object key conflict, got %v", err)
	}
}
