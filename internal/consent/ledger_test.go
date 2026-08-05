package consent

import (
	"context"
	"sync"
	"testing"
)

func grantInput() GrantInput {
	return GrantInput{ContactID: "contact-1", OrganisationID: "org-1", PurposeID: "purpose-1", Channel: "WHATSAPP", ConsentReviewID: "review-1", WordingVersion: "v1", SourceType: "WEB_FORM", EvidenceChecksum: "abc123", CreatedBy: "actor-1", ClientRequestID: "req-1"}
}
func TestGrantReplayIsIdempotentAndMismatchRejected(t *testing.T) {
	s := NewLedgerService(NewMemoryLedgerRepository())
	a, created, err := s.CreateGrant(context.Background(), grantInput())
	if err != nil || !created {
		t.Fatalf("grant=%+v created=%v err=%v", a, created, err)
	}
	b, created, err := s.CreateGrant(context.Background(), grantInput())
	if err != nil || created || a.ID != b.ID {
		t.Fatalf("replay=%+v created=%v err=%v", b, created, err)
	}
	changed := grantInput()
	changed.PurposeID = "purpose-2"
	if _, _, err = s.CreateGrant(context.Background(), changed); err != ErrLedgerReplayConflict {
		t.Fatalf("expected replay conflict, got %v", err)
	}
}
func TestConcurrentWithdrawOnlyOneVersionCommits(t *testing.T) {
	s := NewLedgerService(NewMemoryLedgerRepository())
	g, _, err := s.CreateGrant(context.Background(), grantInput())
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(actor string) {
			defer wg.Done()
			<-start
			_, err := s.Withdraw(context.Background(), g.ID, WithdrawInput{ActorID: actor, Reason: "STOP", ExpectedVersion: g.Version})
			results <- err
		}(map[bool]string{true: "actor-1", false: "actor-2"}[i == 0])
	}
	close(start)
	wg.Wait()
	close(results)
	ok, fail := 0, 0
	for err := range results {
		if err == nil {
			ok++
		} else {
			fail++
		}
	}
	if ok != 1 || fail != 1 {
		t.Fatalf("ok=%d fail=%d", ok, fail)
	}
}
func TestSuppressionScopeAndMinimisedIdentity(t *testing.T) {
	s := NewLedgerService(NewMemoryLedgerRepository())
	if _, _, err := s.CreateSuppression(context.Background(), SuppressionInput{MSISDNLookupHMAC: []byte("hmac"), Scope: SuppressionOrganisation, Reason: "opt out", CreatedBy: "actor", ClientRequestID: "s1"}); err == nil {
		t.Fatal("expected missing organisation")
	}
	v, created, err := s.CreateSuppression(context.Background(), SuppressionInput{MSISDNLookupHMAC: []byte("hmac"), Scope: SuppressionGlobal, Reason: "STOP", CreatedBy: "actor", ClientRequestID: "s2"})
	if err != nil || !created || !v.Active {
		t.Fatalf("suppression=%+v created=%v err=%v", v, created, err)
	}
}
func TestEventsAreAppendOnlyAndContactScoped(t *testing.T) {
	s := NewLedgerService(NewMemoryLedgerRepository())
	g, _, _ := s.CreateGrant(context.Background(), grantInput())
	_, _ = s.Withdraw(context.Background(), g.ID, WithdrawInput{ActorID: "actor-2", Reason: "STOP", ExpectedVersion: g.Version})
	events, err := s.Events(context.Background(), "contact-1", 10)
	if err != nil || len(events) != 2 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if events[0].EventType != "GRANT_WITHDRAWN" || events[1].EventType != "GRANT_CREATED" {
		t.Fatalf("unexpected order: %+v", events)
	}
}
