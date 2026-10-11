package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/storage"
)

func TestMappingAdministrationMakerCheckerAndResolution(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	admin := &MappingAdministration{Store: NewMemoryMappingStore(), Clock: func() time.Time { return now }}
	created, err := admin.Create(ctx, MappingDefinition{
		OrganisationID: "org-1", Name: "registration", SourceSystem: "web", TemplateVersion: CurrentTemplateVersion,
		Mapping: ColumnMapping{MSISDN: "phone", Country: "country"},
	}, "maker", "create reusable mapping", "req-1")
	if err != nil || created.Status != MappingDraft {
		t.Fatalf("create: %+v err=%v", created, err)
	}
	submitted, err := admin.Submit(ctx, created.ID, created.Version, "maker", "submit reusable mapping", "req-2")
	if err != nil || submitted.Status != MappingPending {
		t.Fatalf("submit: %+v err=%v", submitted, err)
	}
	if _, err = admin.Decide(ctx, submitted.ID, submitted.Version, true, "maker", "self approval forbidden", "req-3"); !errors.Is(err, ErrImportTransition) {
		t.Fatalf("expected maker-checker rejection, got %v", err)
	}
	active, err := admin.Decide(ctx, submitted.ID, submitted.Version, true, "checker", "approve reusable mapping", "req-4")
	if err != nil || active.Status != MappingActive {
		t.Fatalf("approve: %+v err=%v", active, err)
	}
	resolved, err := admin.Resolve(ctx, "org-1", "WEB", "registration", now.Add(time.Minute))
	if err != nil || resolved.ID != active.ID {
		t.Fatalf("resolve: %+v err=%v", resolved, err)
	}
	events, err := admin.Events(ctx, active.ID, 20)
	if err != nil || len(events) != 3 || events[0].EventType == "" || events[0].ID == "" {
		t.Fatalf("events: %+v err=%v", events, err)
	}
}

func TestAudienceTemplatesAreVersionedAndDeterministic(t *testing.T) {
	csvPayload, contentType, name, err := RenderTemplate(CurrentTemplateVersion, "csv")
	if err != nil || contentType != "text/csv; charset=utf-8" || name != CurrentTemplateVersion+".csv" {
		t.Fatalf("csv template metadata: %s %s err=%v", contentType, name, err)
	}
	if string(csvPayload) != "msisdn,country,state,lga,age,gender\n" {
		t.Fatalf("unexpected csv template: %q", csvPayload)
	}
	first, _, _, err := RenderTemplate(CurrentTemplateVersion, "xlsx")
	if err != nil {
		t.Fatal(err)
	}
	second, _, _, err := RenderTemplate(CurrentTemplateVersion, "xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("xlsx template must be deterministic")
	}
	archive, err := zip.NewReader(bytes.NewReader(first), int64(len(first)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range archive.File {
		if file.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		reader, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		payload, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read template: %v %v", readErr, closeErr)
		}
		found = strings.Contains(string(payload), "msisdn") && strings.Contains(string(payload), "gender")
	}
	if !found {
		t.Fatal("xlsx template worksheet missing expected columns")
	}
	if _, _, _, err := RenderTemplate("unknown", "csv"); err == nil {
		t.Fatal("unknown template version should fail")
	}
}

type retentionRepositoryStub struct {
	items       []SourceDeletionWork
	completed   []string
	failed      []string
	completeErr error
	failErr     error
}

func (r *retentionRepositoryStub) ClaimSourcesForDeletion(context.Context, string, time.Time, time.Duration, int) ([]SourceDeletionWork, error) {
	return append([]SourceDeletionWork(nil), r.items...), nil
}
func (r *retentionRepositoryStub) CompleteSourceDeletion(_ context.Context, item SourceDeletionWork, _ time.Time) error {
	r.completed = append(r.completed, item.ImportID)
	return r.completeErr
}
func (r *retentionRepositoryStub) FailSourceDeletion(_ context.Context, item SourceDeletionWork, _ string, _ time.Time) error {
	r.failed = append(r.failed, item.ImportID)
	return r.failErr
}

type retentionObjectStore struct {
	deleteErrors map[string]error
	deleted      []string
}

func (s *retentionObjectStore) Put(context.Context, string, io.Reader, int64) (storage.Metadata, error) {
	return storage.Metadata{}, errors.New("not implemented")
}
func (s *retentionObjectStore) Open(context.Context, string) (storage.ReadSeekCloser, storage.Metadata, error) {
	return nil, storage.Metadata{}, errors.New("not implemented")
}
func (s *retentionObjectStore) Stat(context.Context, string) (storage.Metadata, error) {
	return storage.Metadata{}, errors.New("not implemented")
}
func (s *retentionObjectStore) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return s.deleteErrors[key]
}

func TestSourceRetentionWorkerRecordsDeleteFailuresAndCompletesSuccesses(t *testing.T) {
	repository := &retentionRepositoryStub{items: []SourceDeletionWork{
		{ImportID: "i1", ObjectKey: "ok", LeaseOwner: "w", LeaseVersion: 2},
		{ImportID: "i2", ObjectKey: "bad", LeaseOwner: "w", LeaseVersion: 2},
	}}
	worker := &SourceRetentionWorker{
		Repository: repository,
		Objects:    &retentionObjectStore{deleteErrors: map[string]error{"bad": errors.New("storage unavailable")}},
		WorkerID:   "retention-worker",
	}
	processed, err := worker.Process(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 || len(repository.completed) != 1 || repository.completed[0] != "i1" || len(repository.failed) != 1 || repository.failed[0] != "i2" {
		t.Fatalf("processed=%d completed=%v failed=%v", processed, repository.completed, repository.failed)
	}
}

func TestSourceRetentionWorkerDeletesEveryResumableUploadPartBeforeCompletion(t *testing.T) {
	repository := &retentionRepositoryStub{items: []SourceDeletionWork{{
		ImportID: "i-resumable", ObjectKey: "imports/upload-sessions/session/manifest",
		ObjectKeys: []string{"imports/uploads/session/part-000001.bin", "imports/uploads/session/part-000002.bin"},
		LeaseOwner: "w", LeaseVersion: 2,
	}}}
	store := &retentionObjectStore{deleteErrors: map[string]error{}}
	worker := &SourceRetentionWorker{Repository: repository, Objects: store, WorkerID: "retention-worker"}
	processed, err := worker.Process(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 || len(repository.completed) != 1 || len(repository.failed) != 0 {
		t.Fatalf("processed=%d completed=%v failed=%v", processed, repository.completed, repository.failed)
	}
	if len(store.deleted) != 2 || store.deleted[0] != "imports/uploads/session/part-000001.bin" || store.deleted[1] != "imports/uploads/session/part-000002.bin" {
		t.Fatalf("deleted=%v", store.deleted)
	}
}

func TestMemoryRollbackIsIdempotentAndVersioned(t *testing.T) {
	repository := NewMemoryImportRepository()
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	batch := ImportBatch{ID: "import-1", Status: ImportCompleted, Version: 4, UpdatedAt: now}
	repository.items[batch.ID] = batch
	service := &RollbackService{Repository: &MemoryRollbackRepository{Imports: repository}, Clock: func() time.Time { return now }}
	result, err := service.Rollback(context.Background(), batch.ID, 4, "actor", "rollback unconsumed source data", "req")
	if err != nil || result.ImportID != batch.ID {
		t.Fatalf("rollback: %+v err=%v", result, err)
	}
	stored, err := repository.Get(context.Background(), batch.ID)
	if err != nil || stored.Status != ImportRolledBack || stored.Version != 5 {
		t.Fatalf("stored: %+v err=%v", stored, err)
	}
	replayed, err := service.Rollback(context.Background(), batch.ID, 5, "actor", "rollback unconsumed source data", "req")
	if err != nil || replayed.RolledBackAt != result.RolledBackAt {
		t.Fatalf("replay: %+v err=%v", replayed, err)
	}
}

func TestFutureMappingKeepsCurrentMappingEffectiveUntilBoundary(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	admin := &MappingAdministration{Store: NewMemoryMappingStore(), Clock: func() time.Time { return now }}
	activate := func(name string, effective time.Time, maker, checker string) MappingDefinition {
		t.Helper()
		created, err := admin.Create(ctx, MappingDefinition{OrganisationID: "org-1", Name: name, SourceSystem: "web", TemplateVersion: CurrentTemplateVersion, EffectiveFrom: effective, Mapping: ColumnMapping{MSISDN: "phone"}}, maker, "create effective mapping", "req")
		if err != nil {
			t.Fatal(err)
		}
		submitted, err := admin.Submit(ctx, created.ID, created.Version, maker, "submit effective mapping", "req")
		if err != nil {
			t.Fatal(err)
		}
		active, err := admin.Decide(ctx, submitted.ID, submitted.Version, true, checker, "approve effective mapping", "req")
		if err != nil {
			t.Fatal(err)
		}
		return active
	}
	current := activate("registration", now.Add(-time.Hour), "maker-1", "checker-1")
	futureAt := now.Add(24 * time.Hour)
	future := activate("registration", futureAt, "maker-2", "checker-2")
	resolvedNow, err := admin.Resolve(ctx, "org-1", "web", "registration", now)
	if err != nil || resolvedNow.ID != current.ID {
		t.Fatalf("current mapping should remain effective: %+v err=%v", resolvedNow, err)
	}
	resolvedFuture, err := admin.Resolve(ctx, "org-1", "web", "registration", futureAt.Add(time.Second))
	if err != nil || resolvedFuture.ID != future.ID {
		t.Fatalf("future mapping should take effect at boundary: %+v err=%v", resolvedFuture, err)
	}
	currentStored, err := admin.Get(ctx, current.ID)
	if err != nil || currentStored.EffectiveTo == nil || !currentStored.EffectiveTo.Equal(futureAt) || currentStored.Status != MappingActive {
		t.Fatalf("current mapping period not closed correctly: %+v err=%v", currentStored, err)
	}
}
