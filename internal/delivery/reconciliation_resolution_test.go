package delivery

import (
	"context"
	"testing"
	"time"
)

func TestMemoryReconciliationResolutionRejectsIneligibleOrInvalidTransitions(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name      string
		recipient Recipient
		resolved  Status
		action    string
	}{
		{
			name:      "not reconciliation required",
			recipient: Recipient{ID: "no-reconciliation", Status: StatusUnknown, ReconciliationRequired: false, UpdatedAt: now},
			resolved:  StatusFailedRetryable,
			action:    "CONFIRM_NOT_SUBMITTED",
		},
		{
			name:      "invalid resolved status",
			recipient: Recipient{ID: "invalid-status", Status: StatusUnknown, ReconciliationRequired: true, UpdatedAt: now},
			resolved:  Status("BOGUS"),
			action:    "CONFIRM_NOT_SUBMITTED",
		},
		{
			name:      "action status mismatch",
			recipient: Recipient{ID: "action-mismatch", Status: StatusUnknown, ReconciliationRequired: true, UpdatedAt: now},
			resolved:  StatusFailedRetryable,
			action:    "CONFIRM_DELIVERED",
		},
		{
			name:      "safe retry must originate from unknown",
			recipient: Recipient{ID: "retry-from-submitting", Status: StatusSubmitting, ReconciliationRequired: true, UpdatedAt: now},
			resolved:  StatusFailedRetryable,
			action:    "CONFIRM_NOT_SUBMITTED",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := NewMemoryRepository(tc.recipient)
			_, err := repo.ResolveReconciliation(context.Background(), tc.recipient.ID, tc.recipient.Status, tc.resolved, "actor", tc.action, "evidence://resolution", "reviewed provider evidence", now.Add(time.Second))
			if err == nil {
				t.Fatalf("unsafe reconciliation transition was accepted: current=%s resolved=%s action=%s", tc.recipient.Status, tc.resolved, tc.action)
			}
		})
	}
}

func TestMemoryReconciliationResolutionAllowsGovernedUnknownRetry(t *testing.T) {
	now := time.Now().UTC()
	recipient := Recipient{ID: "governed-retry", Status: StatusUnknown, ReconciliationRequired: true, UpdatedAt: now}
	repo := NewMemoryRepository(recipient)
	got, err := repo.ResolveReconciliation(context.Background(), recipient.ID, StatusUnknown, StatusFailedRetryable, "actor", "CONFIRM_NOT_SUBMITTED", "evidence://resolution", "reviewed provider evidence", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailedRetryable || got.ReconciliationRequired {
		t.Fatalf("governed retry resolution=%+v", got)
	}
}
