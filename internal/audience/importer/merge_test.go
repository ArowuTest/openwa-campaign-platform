package importer

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func protectedMergeCandidate(seed byte, observedAt time.Time) ContactCandidate {
	age := 27
	return ContactCandidate{
		RowNumber:         int(seed) + 2,
		EncryptedMSISDN:   []byte{seed, seed + 1, seed + 2},
		LookupHMAC:        []byte{seed, seed, seed, seed},
		MaskedMSISDN:      "+234******5678",
		Country:           "NG",
		State:             "Lagos",
		LGA:               "Ikeja",
		ReportedAge:       &age,
		AgeRecordedAt:     observedAt,
		ProfileRecordedAt: observedAt,
		Gender:            "FEMALE",
		SourceHash:        "source-hash-" + string(rune(seed)),
	}
}

func approvedMemoryImport(now time.Time) MemoryMergeImport {
	return MemoryMergeImport{
		ID: "import-1", OrganisationID: "org-1", PurposeID: "purpose-1",
		Channel: "WHATSAPP", WordingVersion: "v1", UploadedBy: "maker-1", ApprovedBy: "checker-1",
		Status: "APPROVED", ReviewApproved: true, ReviewExpiresAt: now.Add(24 * time.Hour), EvidenceClean: true,
		Candidates: []ContactCandidate{
			protectedMergeCandidate(1, now.Add(-2*time.Hour)),
			protectedMergeCandidate(2, now.Add(-time.Hour)),
		},
	}
}

func TestMergeIsIdempotentAndReturnsOriginalResult(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	repository := NewMemoryMergeRepository()
	repository.Seed(approvedMemoryImport(now))
	service := MergeService{Repository: repository, Clock: func() time.Time { return now }}

	first, err := service.Merge(context.Background(), "import-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Merge(context.Background(), "import-1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("replay returned a different result: first=%+v second=%+v", first, second)
	}
	if first.InsertedContacts != 2 || first.ConsentGrants != 2 || first.SourceLinks != 2 || first.ProfileHistory != 2 {
		t.Fatalf("unexpected merge result: %+v", first)
	}
}

func TestConcurrentMergeCommitsOnceAndReplaysSameOutcome(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	repository := NewMemoryMergeRepository()
	repository.Seed(approvedMemoryImport(now))
	service := MergeService{Repository: repository, Clock: func() time.Time { return now }}

	const callers = 12
	results := make(chan MergeResult, callers)
	errorsSeen := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := service.Merge(context.Background(), "import-1")
			results <- result
			errorsSeen <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatalf("concurrent merge failed: %v", err)
		}
	}
	var expected *MergeResult
	for result := range results {
		if expected == nil {
			copy := result
			expected = &copy
			continue
		}
		if !reflect.DeepEqual(*expected, result) {
			t.Fatalf("concurrent replay mismatch: expected=%+v actual=%+v", *expected, result)
		}
	}
	if len(repository.contacts) != 2 {
		t.Fatalf("expected two canonical contacts, got %d", len(repository.contacts))
	}
}

func TestMergeEnforcesMakerCheckerAndConsentEvidence(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	input := approvedMemoryImport(now)
	input.ApprovedBy = input.UploadedBy
	repository := NewMemoryMergeRepository()
	repository.Seed(input)
	_, err := (&MergeService{Repository: repository, Clock: func() time.Time { return now }}).Merge(context.Background(), input.ID)
	if !errors.Is(err, ErrMakerChecker) {
		t.Fatalf("expected maker-checker error, got %v", err)
	}

	input = approvedMemoryImport(now)
	input.ID = "import-2"
	input.EvidenceClean = false
	repository.Seed(input)
	_, err = (&MergeService{Repository: repository, Clock: func() time.Time { return now }}).Merge(context.Background(), input.ID)
	if !errors.Is(err, ErrConsentBasis) {
		t.Fatalf("expected consent-basis error, got %v", err)
	}
}

func TestMergeRejectsUnsupportedUpdatePolicy(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	input := approvedMemoryImport(now)
	input.UpdatePolicy = UpdateTrustedSource
	repository := NewMemoryMergeRepository()
	repository.Seed(input)
	_, err := (&MergeService{Repository: repository, Clock: func() time.Time { return now }}).Merge(context.Background(), input.ID)
	if !errors.Is(err, ErrUnsupportedUpdatePolicy) {
		t.Fatalf("expected unsupported update policy error, got %v", err)
	}
}

func TestFillNullPolicyDoesNotOverwriteExistingProfile(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	repository := NewMemoryMergeRepository()
	first := approvedMemoryImport(now)
	first.ID = "import-first"
	first.UpdatePolicy = UpdateNewestSource
	first.Candidates = []ContactCandidate{protectedMergeCandidate(9, now.Add(-time.Hour))}
	repository.Seed(first)
	service := MergeService{Repository: repository, Clock: func() time.Time { return now }}
	if _, err := service.Merge(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	second := approvedMemoryImport(now)
	second.ID = "import-second"
	second.UpdatePolicy = UpdateFillNull
	candidate := protectedMergeCandidate(9, now)
	candidate.State = "Abuja"
	candidate.LGA = "Municipal"
	age := 41
	candidate.ReportedAge = &age
	second.Candidates = []ContactCandidate{candidate}
	repository.Seed(second)
	if _, err := service.Merge(context.Background(), second.ID); err != nil {
		t.Fatal(err)
	}
	stored := repository.contacts[string(candidate.LookupHMAC)].Candidate
	if stored.State != "Lagos" || stored.LGA != "Ikeja" || stored.ReportedAge == nil || *stored.ReportedAge != 27 {
		t.Fatalf("fill-null policy overwrote existing profile: %+v", stored)
	}
}
