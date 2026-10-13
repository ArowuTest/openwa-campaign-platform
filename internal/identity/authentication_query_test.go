package identity

import (
	"context"
	"testing"
	"time"
)

func TestMemoryAuthenticationEventQueryOrdersAndPaginates(t *testing.T) {
	recorder := NewMemoryEventRecorder()
	base := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	events := []AuthenticationEvent{
		{ID: "00000000-0000-4000-8000-000000000001", UserID: "00000000-0000-4000-8000-000000000101", Type: EventLoginFailed, CorrelationID: "corr-1", OccurredAt: base.Add(time.Minute), Metadata: map[string]string{"reason": "invalid credentials"}},
		{ID: "00000000-0000-4000-8000-000000000002", UserID: "00000000-0000-4000-8000-000000000101", Type: EventMFASucceeded, CorrelationID: "corr-2", OccurredAt: base.Add(2 * time.Minute)},
	}
	for _, event := range events {
		if err := recorder.Append(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	first, err := recorder.SearchAuthenticationEvents(context.Background(), AuthenticationEventQuery{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.Items[0].ID != events[1].ID || first.NextCursor == "" {
		t.Fatalf("unexpected first page: %+v", first)
	}
	cursor, err := DecodeAuthenticationEventCursor(first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	second, err := recorder.SearchAuthenticationEvents(context.Background(), AuthenticationEventQuery{After: &cursor, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != events[0].ID || second.NextCursor != "" {
		t.Fatalf("unexpected second page: %+v", second)
	}
}

func TestValidateAuthenticationEventQueryFailsClosed(t *testing.T) {
	cases := []AuthenticationEventQuery{
		{UserID: "not-a-uuid"},
		{EventType: AuthenticationEventType("UNKNOWN")},
		{Limit: 501},
	}
	for _, query := range cases {
		if err := ValidateAuthenticationEventQuery(query); err == nil {
			t.Fatalf("expected query to fail validation: %+v", query)
		}
	}
	if _, err := DecodeAuthenticationEventCursor("too-large-or-invalid"); err == nil {
		t.Fatal("expected malformed cursor to fail")
	}
}
