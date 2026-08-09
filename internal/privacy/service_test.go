package privacy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	sharedcrypto "campaign-platform/internal/shared/crypto"
)

func testService(t *testing.T) (*Service, *MemoryRepository, time.Time) {
	t.Helper()
	protector, err := sharedcrypto.NewMSISDNProtector(make([]byte, 32), []byte("lookup-key-that-is-at-least-thirty-two-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := sharedcrypto.NewSecretKeyring("v1", map[string][]byte{"v1": make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	return &Service{Repository: repo, Protector: protector, Evidence: ring, Clock: func() time.Time { return now }}, repo, now
}

func seedSubject(t *testing.T, service *Service, repo *MemoryRepository, msisdn string) []byte {
	t.Helper()
	normalized, err := sharedcrypto.NormalizeE164(msisdn)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := service.Protector.Encrypt(normalized)
	if err != nil {
		t.Fatal(err)
	}
	lookup := service.Protector.LookupHMAC(normalized)
	repo.SeedSubject(lookup, SubjectPackage{
		ContactID:       "contact-1",
		EncryptedMSISDN: cipher,
		MaskedMSISDN:    sharedcrypto.Mask(normalized),
		Status:          "ACTIVE",
		Profile:         map[string]any{"genderCode": "NOT_STATED"},
		Consents:        []map[string]any{{"purpose": "EVENTS", "status": "ACTIVE"}},
	})
	return lookup
}

func approveCase(t *testing.T, service *Service, caseType CaseType, changes any) Case {
	t.Helper()
	created, err := service.Create(context.Background(), CreateInput{Type: caseType, MSISDN: "+2348012345678", Reason: "valid subject request", RequestedChanges: changes, CreatedBy: "maker"}, "corr-1")
	if err != nil {
		t.Fatal(err)
	}
	assigned, err := service.Assign(context.Background(), created.ID, "case-worker", "assign for investigation", "supervisor", "corr-2", created.Version)
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := service.Submit(context.Background(), assigned.ID, "evidence review completed", "case-worker", "corr-3", assigned.Version)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.Decide(context.Background(), submitted.ID, true, "request is lawful and evidenced", "checker", "corr-4", submitted.Version)
	if err != nil {
		t.Fatal(err)
	}
	return approved
}

func TestAccessPackageIsEncryptedAndIntegrityChecked(t *testing.T) {
	service, repo, _ := testService(t)
	lookup := seedSubject(t, service, repo, "+2348012345678")
	subject := repo.subjects[stringKey(lookup)]
	subject.Suppressions = []map[string]any{{"scope": "GLOBAL", "reason": "STOP", "active": true}}
	repo.SeedSubject(lookup, subject)
	approved := approveCase(t, service, CaseAccess, map[string]any{})

	completed, err := service.Execute(context.Background(), approved.ID, "produce controlled access package", "executor", "corr-5", approved.Version)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != StatusCompleted || len(completed.ResultCiphertext) == 0 || completed.ResultKeyVersion != "v1" {
		t.Fatalf("unexpected completed case: %+v", completed)
	}
	if string(completed.ResultCiphertext) == "+2348012345678" {
		t.Fatal("plaintext MSISDN was stored as result ciphertext")
	}
	envelope, err := service.ExportEnvelope(context.Background(), completed.ID, "exporter", "corr-6")
	if err != nil {
		t.Fatal(err)
	}
	events, err := service.Events(context.Background(), completed.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[len(events)-1].Type != "EXPORT_REQUESTED" {
		t.Fatalf("missing immutable export request event: %+v", events)
	}
	payload, err := service.ResolveExportEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["msisdn"] != "+2348012345678" {
		t.Fatalf("unexpected package msisdn: %v", decoded["msisdn"])
	}
	subjectPayload, ok := decoded["subject"].(map[string]any)
	if !ok {
		t.Fatalf("subject package missing from export: %#v", decoded["subject"])
	}
	consents, ok := subjectPayload["consents"].([]any)
	if !ok || len(consents) != 1 {
		t.Fatalf("consent evidence missing from export: %#v", subjectPayload["consents"])
	}
	suppressions, ok := subjectPayload["suppressions"].([]any)
	if !ok || len(suppressions) != 1 {
		t.Fatalf("suppression evidence missing from export: %#v", subjectPayload["suppressions"])
	}
}

func TestErasureBlockedByContactLegalHold(t *testing.T) {
	service, repo, _ := testService(t)
	lookup := seedSubject(t, service, repo, "+2348012345678")
	approved := approveCase(t, service, CaseErasure, map[string]any{})
	hold, err := service.CreateLegalHold(context.Background(), "+2348012345678", "", "CONTACT", "preserve evidence for dispute", "legal-maker", "corr-hold", nil)
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := repo.HasActiveLegalHold(context.Background(), lookup, "CONTACT", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if blocked {
		t.Fatal("draft legal hold was treated as active")
	}
	hold, err = service.SubmitLegalHold(context.Background(), hold.ID, "submit hold for independent review", "legal-maker", "corr-hold-submit", hold.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecideLegalHold(context.Background(), hold.ID, true, "approve evidence preservation hold", "legal-maker", "corr-hold-decision", hold.Version); !errors.Is(err, ErrInvalid) {
		t.Fatalf("maker was allowed to approve legal hold: %v", err)
	}
	hold, err = service.DecideLegalHold(context.Background(), hold.ID, true, "approve evidence preservation hold", "legal-checker", "corr-hold-decision", hold.Version)
	if err != nil {
		t.Fatal(err)
	}
	if hold.Status != HoldActive {
		t.Fatalf("expected active legal hold, got %+v", hold)
	}
	events, err := service.LegalHoldEvents(context.Background(), hold.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Type != "APPROVED" || events[1].Type != "SUBMITTED" || events[2].Type != "CREATED" {
		t.Fatalf("unexpected legal hold event history: %+v", events)
	}
	_, err = service.Execute(context.Background(), approved.ID, "execute lawful erasure request", "executor", "corr-exec", approved.Version)
	if !errors.Is(err, ErrLegalHold) {
		t.Fatalf("expected legal hold error, got %v", err)
	}
}

func TestRectificationInitialisesMissingProfileAndPreservesOtherData(t *testing.T) {
	service, repo, _ := testService(t)
	lookup := seedSubject(t, service, repo, "+2348012345678")
	subject := repo.subjects[stringKey(lookup)]
	subject.Profile = nil
	repo.SeedSubject(lookup, subject)
	approved := approveCase(t, service, CaseRectification, map[string]any{"genderCode": "FEMALE", "reportedAge": 31})
	if _, err := service.Execute(context.Background(), approved.ID, "apply verified corrections", "executor", "corr-exec", approved.Version); err != nil {
		t.Fatal(err)
	}
	updated, err := repo.LoadSubjectPackage(context.Background(), lookup, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Profile["genderCode"] != "FEMALE" || updated.Profile["reportedAge"] != float64(31) {
		t.Fatalf("rectification not applied: %#v", updated.Profile)
	}
}

func TestMakerCheckerAndExecutorSeparation(t *testing.T) {
	service, repo, _ := testService(t)
	seedSubject(t, service, repo, "+2348012345678")
	created, err := service.Create(context.Background(), CreateInput{Type: CaseAccess, MSISDN: "+2348012345678", Reason: "valid subject request", CreatedBy: "maker"}, "corr")
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := service.Submit(context.Background(), created.ID, "submit evidence for review", "maker", "corr", created.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Decide(context.Background(), submitted.ID, true, "approve request after review", "maker", "corr", submitted.Version); !errors.Is(err, ErrInvalid) {
		t.Fatalf("maker was allowed to approve: %v", err)
	}
	approved, err := service.Decide(context.Background(), submitted.ID, true, "approve request after review", "checker", "corr", submitted.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(context.Background(), approved.ID, "produce the approved package", "checker", "corr", approved.Version); !errors.Is(err, ErrConflict) {
		t.Fatalf("checker was allowed to execute own approval: %v", err)
	}
}

func stringKey(value []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(value)*2)
	for i, b := range value {
		out[i*2] = hex[b>>4]
		out[i*2+1] = hex[b&0x0f]
	}
	return string(out)
}
