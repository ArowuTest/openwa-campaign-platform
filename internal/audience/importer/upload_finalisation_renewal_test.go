package importer

import (
	"strings"
	"testing"
	"time"
)

func TestUploadSessionFinalisationLeaseRenewalInvalidatesOldExpiry(t *testing.T) {
	now := time.Date(2026, 10, 6, 11, 0, 0, 0, time.UTC)
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
	claimed, original, err := session.ClaimFinalisation("worker-a", 2*time.Minute, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	renewed, current, err := claimed.RenewFinalisation(original, 2*time.Minute, now.Add(4*time.Minute))
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if current.Owner != original.Owner || current.Version != original.Version || !current.ExpiresAt.After(original.ExpiresAt) {
		t.Fatalf("unexpected renewed lease: original=%+v current=%+v", original, current)
	}
	if _, err := renewed.CompleteFinalisation("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", strings.Repeat("b", 64), "text/csv", original, now.Add(4*time.Minute+30*time.Second)); err == nil {
		t.Fatal("old lease expiry remained valid after renewal")
	}
	if _, err := renewed.CompleteFinalisation("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", strings.Repeat("b", 64), "text/csv", current, now.Add(4*time.Minute+30*time.Second)); err != nil {
		t.Fatalf("renewed lease rejected: %v", err)
	}
}
