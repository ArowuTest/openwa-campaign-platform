package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryAuthenticationSnapshotFreezesRedactedScopedRows(t *testing.T) {
	ctx := context.Background()
	recorder := NewMemoryEventRecorder()
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	user := "00000000-0000-4000-8000-000000000001"
	original := AuthenticationEvent{
		ID: "00000000-0000-4000-8000-000000000010", UserID: user,
		Type: EventLoginSucceeded, CorrelationID: "selected", OccurredAt: at,
		EmailHash: "secret-email", SourceIP: "192.0.2.1", UserAgent: "secret-agent",
		Metadata: map[string]string{"secret": "private"},
	}
	for _, event := range []AuthenticationEvent{
		original,
		{ID: "00000000-0000-4000-8000-000000000011", UserID: user, Type: EventLoginFailed, CorrelationID: "selected", OccurredAt: at},
		{ID: "00000000-0000-4000-8000-000000000012", Type: EventLoginSucceeded, CorrelationID: "selected", OccurredAt: at},
		{ID: "00000000-0000-4000-8000-000000000013", UserID: user, Type: EventLoginSucceeded, CorrelationID: "other", OccurredAt: at},
		{ID: "00000000-0000-4000-8000-000000000014", UserID: user, Type: EventLoginSucceeded, CorrelationID: "selected", OccurredAt: at.Add(time.Hour)},
	} {
		if err := recorder.Append(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	to := at.Add(time.Second)
	rows, err := recorder.SnapshotAuthenticationEvents(ctx, AuthenticationEventQuery{
		UserID: user, EventType: EventLoginSucceeded, CorrelationID: "selected", From: &at, To: &to,
	}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != original.ID {
		t.Fatalf("unexpected scoped snapshot: %+v", rows)
	}
	if rows[0].EmailHash != "" || rows[0].SourceIP != "" || rows[0].UserAgent != "" || rows[0].Metadata != nil {
		t.Fatalf("snapshot retained sensitive fields: %+v", rows[0])
	}
	if err := recorder.Append(ctx, AuthenticationEvent{ID: "00000000-0000-4000-8000-000000000009", UserID: user, Type: EventLoginSucceeded, CorrelationID: "selected", OccurredAt: at}); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("later append changed captured rows: %+v", rows)
	}
	rows[0].CorrelationID = "changed-copy"
	if recorder.Events()[0].CorrelationID != original.CorrelationID {
		t.Fatal("snapshot shares mutable event storage")
	}
}

func TestMemoryAuthenticationSnapshotRejectsOverflowAndPagination(t *testing.T) {
	recorder := NewMemoryEventRecorder()
	for i := 0; i < 3; i++ {
		if err := recorder.Append(context.Background(), AuthenticationEvent{ID: "00000000-0000-4000-8000-000000000010", OccurredAt: time.Now(), Type: EventLoginFailed}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := recorder.SnapshotAuthenticationEvents(context.Background(), AuthenticationEventQuery{}, 2); !errors.Is(err, ErrAuthenticationEventSnapshotTooLarge) {
		t.Fatalf("expected fail-closed overflow, got %v", err)
	}
	for _, maximum := range []int{0, MaxAuthenticationEventSnapshotRows + 1} {
		if _, err := recorder.SnapshotAuthenticationEvents(context.Background(), AuthenticationEventQuery{}, maximum); !errors.Is(err, ErrInvalidAuthenticationEventQuery) {
			t.Fatalf("accepted invalid maximum %d: %v", maximum, err)
		}
	}
	if _, err := recorder.SnapshotAuthenticationEvents(context.Background(), AuthenticationEventQuery{After: &AuthenticationEventCursor{}}, 10); !errors.Is(err, ErrInvalidAuthenticationEventQuery) {
		t.Fatalf("accepted partial snapshot cursor: %v", err)
	}
}
