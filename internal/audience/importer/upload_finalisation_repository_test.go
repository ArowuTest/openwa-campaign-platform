package importer

import (
	"context"
	"strings"
	"testing"
	"time"
)

func completedMemoryUploadSession(t *testing.T, repo *MemoryUploadSessionRepository, requestKey string, now time.Time) UploadSession {
	t.Helper()
	input := validUploadSessionInput()
	input.ClientRequestID = requestKey
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
	if _, _, err := repo.Create(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	return session
}

func TestMemoryUploadFinalisationClaimsAreBoundedAndFenced(t *testing.T) {
	now := time.Date(2026, 10, 6, 10, 30, 0, 0, time.UTC)
	repo := NewMemoryUploadSessionRepository()
	first := completedMemoryUploadSession(t, repo, "finalisation-claim-request-0001", now)
	second := completedMemoryUploadSession(t, repo, "finalisation-claim-request-0002", now)

	claimed, err := repo.ClaimReadyFinalisations(context.Background(), "worker-a", 2*time.Minute, 1, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed=%d want 1", len(claimed))
	}
	if claimed[0].Session.State != UploadSessionFinalising || claimed[0].Lease.Owner != "worker-a" {
		t.Fatalf("unexpected claim: %+v", claimed[0])
	}

	other, err := repo.ClaimReadyFinalisations(context.Background(), "worker-b", 10*time.Minute, 5, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].Session.ID == claimed[0].Session.ID {
		t.Fatalf("second worker must claim only the unclaimed source: %+v", other)
	}

	takeover, err := repo.ClaimReadyFinalisations(context.Background(), "worker-c", 2*time.Minute, 5, claimed[0].Lease.ExpiresAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(takeover) != 1 || takeover[0].Session.ID != claimed[0].Session.ID || takeover[0].Lease.Version != claimed[0].Lease.Version+1 {
		t.Fatalf("expired lease was not fenced/taken over: %+v", takeover)
	}

	claimedID := takeover[0].Session.ID
	if _, err := repo.CompleteUploadFinalisation(context.Background(), claimedID, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", strings.Repeat("b", 64), "text/csv", claimed[0].Lease, now.Add(6*time.Minute)); err == nil {
		t.Fatal("stale worker completed finalisation")
	}
	finalised, err := repo.CompleteUploadFinalisation(context.Background(), claimedID, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", strings.Repeat("b", 64), "text/csv", takeover[0].Lease, takeover[0].Lease.ExpiresAt.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if finalised.State != UploadSessionImportCreated {
		t.Fatalf("finalised state=%s", finalised.State)
	}

	// Keep the second ID referenced so accidental map ordering never makes the
	// test pass without actually creating two distinct sessions.
	if first.ID == second.ID {
		t.Fatal("test setup produced duplicate session IDs")
	}
}
