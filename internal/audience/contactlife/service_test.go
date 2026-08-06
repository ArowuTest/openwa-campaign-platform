package contactlife

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTransitionRecordsImmutableLifecycleEvidence(t *testing.T) {
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository(Record{ContactID: "contact-1", MaskedMSISDN: "+234 *** 5678", Status: StatusActive, Version: 1, UpdatedAt: now.Add(-time.Hour)})
	svc := &Service{Repository: repo, Clock: func() time.Time { return now }}
	out, err := svc.Transition(context.Background(), "contact-1", StatusSuppressed, true, 1, "operator", "customer objected to marketing", "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusSuppressed || !out.ProcessingRestricted || out.Version != 2 {
		t.Fatalf("unexpected result: %+v", out)
	}
	events, err := svc.Events(context.Background(), "contact-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].PreviousStatus != StatusActive || events[0].NewStatus != StatusSuppressed {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestAnonymisedContactCannotBeReactivated(t *testing.T) {
	repo := NewMemoryRepository(Record{ContactID: "contact-1", Status: StatusAnonymised, Version: 3})
	svc := &Service{Repository: repo}
	_, err := svc.Transition(context.Background(), "contact-1", StatusActive, false, 3, "operator", "attempt to reactivate record", "req-1")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}
