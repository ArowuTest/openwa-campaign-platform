package importer

import (
	"strings"
	"testing"
	"time"
)

func validUploadSessionInput() UploadSessionInput {
	return UploadSessionInput{
		OrganisationID:     "11111111-1111-4111-8111-111111111111",
		ConsentReviewID:    "22222222-2222-4222-8222-222222222222",
		PurposeID:          "33333333-3333-4333-8333-333333333333",
		Channel:            "WHATSAPP",
		WordingVersion:     "v1",
		SourceName:         "two-million-recipient-source",
		SourceSystem:       "client-secure-export",
		DefaultCountryISO2: "NG",
		OriginalFilename:   "audience.csv",
		TemplateVersion:    "v1",
		Mapping:            ColumnMapping{MSISDN: "msisdn", Country: "country", State: "state", LGA: "lga", Age: "age", Gender: "gender"},
		UpdatePolicy:       UpdateNewestSource,
		UploadedBy:         "44444444-4444-4444-8444-444444444444",
		ClientRequestID:    "large-import-request-0001",
		ExpectedBytes:      (160 << 20) + 123,
	}
}

func TestNewUploadSessionPartitionsLargeFileIntoBoundedParts(t *testing.T) {
	now := time.Date(2026, 10, 6, 8, 30, 0, 0, time.UTC)
	input := validUploadSessionInput()
	session, err := NewUploadSession(input, DefaultUploadPartSize, 2<<30, now.Add(24*time.Hour), now)
	if err != nil {
		t.Fatalf("new upload session: %v", err)
	}
	if session.ID == "" {
		t.Fatal("session ID is required")
	}
	if session.State != UploadSessionCreated || session.Version != 1 {
		t.Fatalf("unexpected initial state/version: %s v%d", session.State, session.Version)
	}
	if session.PartSize != 8<<20 {
		t.Fatalf("unexpected part size: %d", session.PartSize)
	}
	if session.PartCount != 21 || len(session.Parts) != 21 {
		t.Fatalf("expected 21 parts, got count=%d len=%d", session.PartCount, len(session.Parts))
	}
	for index, part := range session.Parts {
		expectedNumber := index + 1
		if part.Number != expectedNumber {
			t.Fatalf("part %d number=%d", index, part.Number)
		}
		if part.Offset != int64(index)*(8<<20) {
			t.Fatalf("part %d offset=%d", part.Number, part.Offset)
		}
		expectedSize := int64(8 << 20)
		if expectedNumber == 21 {
			expectedSize = 123
		}
		if part.ExpectedBytes != expectedSize {
			t.Fatalf("part %d expected bytes=%d want=%d", part.Number, part.ExpectedBytes, expectedSize)
		}
		if !strings.Contains(part.ObjectKey, session.ID) || strings.Contains(part.ObjectKey, input.OriginalFilename) {
			t.Fatalf("part object key must be opaque and session-scoped: %q", part.ObjectKey)
		}
	}
}

func TestNewUploadSessionRejectsUnsafeGeometry(t *testing.T) {
	now := time.Now().UTC()
	input := validUploadSessionInput()
	input.ExpectedBytes = 0
	if _, err := NewUploadSession(input, DefaultUploadPartSize, 2<<30, now.Add(time.Hour), now); err == nil {
		t.Fatal("expected zero-size source rejection")
	}

	input = validUploadSessionInput()
	input.ExpectedBytes = (2 << 30) + 1
	if _, err := NewUploadSession(input, DefaultUploadPartSize, 2<<30, now.Add(time.Hour), now); err == nil {
		t.Fatal("expected configured maximum rejection")
	}

	input = validUploadSessionInput()
	if _, err := NewUploadSession(input, MinUploadPartSize-1, 2<<30, now.Add(time.Hour), now); err == nil {
		t.Fatal("expected undersized part rejection")
	}
	if _, err := NewUploadSession(input, MaxUploadPartSize+1, 2<<30, now.Add(time.Hour), now); err == nil {
		t.Fatal("expected oversized part rejection")
	}
	if _, err := NewUploadSession(input, DefaultUploadPartSize, 2<<30, now, now); err == nil {
		t.Fatal("expected non-future expiry rejection")
	}
}

func TestUploadSessionRecordsExactPartReplayButRejectsConflict(t *testing.T) {
	now := time.Date(2026, 10, 6, 8, 30, 0, 0, time.UTC)
	input := validUploadSessionInput()
	input.ExpectedBytes = 10 << 20
	session, err := NewUploadSession(input, 5<<20, 2<<30, now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	hashA := strings.Repeat("a", 64)
	next, changed, err := session.RecordPart(1, 5<<20, hashA, session.Version, now.Add(time.Minute))
	if err != nil || !changed {
		t.Fatalf("record part: changed=%v err=%v", changed, err)
	}
	if next.State != UploadSessionUploading || next.Version != session.Version+1 {
		t.Fatalf("unexpected post-part state: %s v%d", next.State, next.Version)
	}
	replayed, changed, err := next.RecordPart(1, 5<<20, hashA, next.Version, now.Add(2*time.Minute))
	if err != nil || changed {
		t.Fatalf("exact replay must converge without mutation: changed=%v err=%v", changed, err)
	}
	if replayed.Version != next.Version {
		t.Fatalf("exact replay advanced version: %d -> %d", next.Version, replayed.Version)
	}
	if _, _, err := next.RecordPart(1, 5<<20, strings.Repeat("b", 64), next.Version, now.Add(2*time.Minute)); err == nil {
		t.Fatal("conflicting checksum replay must fail")
	}
	if _, _, err := next.RecordPart(2, (5<<20)-1, hashA, next.Version, now.Add(2*time.Minute)); err == nil {
		t.Fatal("wrong part size must fail")
	}
	if _, _, err := next.RecordPart(0, 5<<20, hashA, next.Version, now.Add(2*time.Minute)); err == nil {
		t.Fatal("out-of-range part must fail")
	}
}

func TestUploadSessionCompletionRequiresEveryVerifiedPartAndExactAccounting(t *testing.T) {
	now := time.Date(2026, 10, 6, 8, 30, 0, 0, time.UTC)
	input := validUploadSessionInput()
	input.ExpectedBytes = (10 << 20) + 7
	session, err := NewUploadSession(input, 5<<20, 2<<30, now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Complete(session.Version, now.Add(time.Minute)); err == nil {
		t.Fatal("incomplete upload must not complete")
	}
	for number, hash := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)} {
		size := int64(5 << 20)
		if number == 2 {
			size = 7
		}
		session, _, err = session.RecordPart(number+1, size, hash, session.Version, now.Add(time.Duration(number+1)*time.Minute))
		if err != nil {
			t.Fatalf("record part %d: %v", number+1, err)
		}
	}
	completed, err := session.Complete(session.Version, now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if completed.State != UploadSessionUploaded {
		t.Fatalf("state=%s", completed.State)
	}
	if completed.UploadedBytes != input.ExpectedBytes || completed.UploadedParts != 3 {
		t.Fatalf("accounting bytes=%d parts=%d", completed.UploadedBytes, completed.UploadedParts)
	}
	if _, _, err := completed.RecordPart(1, 5<<20, strings.Repeat("a", 64), completed.Version, now.Add(6*time.Minute)); err == nil {
		t.Fatal("completed upload must reject further writes")
	}
}

func TestUploadSessionOptimisticVersionAbortAndExpiry(t *testing.T) {
	now := time.Date(2026, 10, 6, 8, 30, 0, 0, time.UTC)
	session, err := NewUploadSession(validUploadSessionInput(), DefaultUploadPartSize, 2<<30, now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.RecordPart(1, session.Parts[0].ExpectedBytes, strings.Repeat("a", 64), session.Version+1, now.Add(time.Minute)); err == nil {
		t.Fatal("stale/wrong expected version must fail")
	}
	aborted, err := session.Abort("operator selected wrong source file", session.Version, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("abort: %v", err)
	}
	if aborted.State != UploadSessionAborted || aborted.FailureReason == "" {
		t.Fatalf("unexpected abort state: %+v", aborted)
	}
	if _, err := aborted.Abort("again", aborted.Version, now.Add(3*time.Minute)); err == nil {
		t.Fatal("terminal abort must not be mutable")
	}

	expired, err := NewUploadSession(validUploadSessionInput(), DefaultUploadPartSize, 2<<30, now.Add(time.Minute), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := expired.RecordPart(1, expired.Parts[0].ExpectedBytes, strings.Repeat("a", 64), expired.Version, now.Add(2*time.Minute)); err == nil {
		t.Fatal("expired session must reject upload")
	}
}

func TestUploadSessionFinalisationLeaseFencesStaleWorkers(t *testing.T) {
	now := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	input := validUploadSessionInput()
	input.ExpectedBytes = 9
	session, err := NewUploadSession(input, DefaultUploadPartSize, 2<<30, now.Add(24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	session, _, err = session.RecordPart(1, 9, strings.Repeat("a", 64), session.Version, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	session, err = session.Complete(session.Version, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	claimed, lease, err := session.ClaimFinalisation("worker-a", 2*time.Minute, now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed.State != UploadSessionFinalising || lease.Owner != "worker-a" || lease.Version != 1 {
		t.Fatalf("unexpected claim: state=%s lease=%+v", claimed.State, lease)
	}
	if _, _, err := claimed.ClaimFinalisation("worker-b", 2*time.Minute, now.Add(4*time.Minute)); err == nil {
		t.Fatal("live finalisation lease allowed takeover")
	}

	taken, replacement, err := claimed.ClaimFinalisation("worker-b", 2*time.Minute, lease.ExpiresAt.Add(time.Second))
	if err != nil {
		t.Fatalf("expired takeover: %v", err)
	}
	if replacement.Owner != "worker-b" || replacement.Version != lease.Version+1 {
		t.Fatalf("replacement lease=%+v", replacement)
	}
	if _, err := taken.CompleteFinalisation("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", strings.Repeat("b", 64), "text/csv", lease, now.Add(6*time.Minute)); err == nil {
		t.Fatal("stale finaliser lease completed session")
	}
	completed, err := taken.CompleteFinalisation("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", strings.Repeat("b", 64), "text/csv", replacement, now.Add(6*time.Minute))
	if err != nil {
		t.Fatalf("complete finalisation: %v", err)
	}
	if completed.State != UploadSessionImportCreated || completed.LinkedImportID == "" || completed.FinalSHA256 == "" || completed.FinaliserLeaseOwner != "" {
		t.Fatalf("unexpected completed finalisation: %+v", completed)
	}
}

func TestUploadSessionFinalisationFailureIsFencedAndTerminal(t *testing.T) {
	now := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	input := validUploadSessionInput()
	input.ExpectedBytes = 9
	session, _ := NewUploadSession(input, DefaultUploadPartSize, 2<<30, now.Add(24*time.Hour), now)
	session, _, _ = session.RecordPart(1, 9, strings.Repeat("a", 64), session.Version, now.Add(time.Minute))
	session, _ = session.Complete(session.Version, now.Add(2*time.Minute))
	claimed, lease, err := session.ClaimFinalisation("worker-a", 2*time.Minute, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	failed, err := claimed.FailFinalisation("malware scanner rejected source", lease, now.Add(4*time.Minute))
	if err != nil {
		t.Fatalf("fail finalisation: %v", err)
	}
	if failed.State != UploadSessionFailed || failed.FailureReason == "" || failed.FinaliserLeaseOwner != "" {
		t.Fatalf("unexpected failed finalisation: %+v", failed)
	}
	if _, err := failed.FailFinalisation("retry", lease, now.Add(5*time.Minute)); err == nil {
		t.Fatal("terminal failed session accepted stale mutation")
	}
}
