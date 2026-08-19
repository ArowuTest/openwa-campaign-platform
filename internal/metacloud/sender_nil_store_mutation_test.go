package metacloud

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSenderMutationMethodsFailClosedWithoutStore(t *testing.T) {
	svc := &Service{}
	ctx := context.Background()
	tests := []struct {
		name string
		call func() error
	}{
		{name: "submit", call: func() error { _, err := svc.Submit(ctx, "sender", 1, "actor", "valid reason"); return err }},
		{name: "decide", call: func() error {
			_, err := svc.Decide(ctx, "sender", 1, true, "actor", "valid reason", time.Now())
			return err
		}},
		{name: "observe health", call: func() error {
			_, err := svc.ObserveHealth(ctx, "sender", 1, HealthHealthy, time.Now(), "actor", "valid reason")
			return err
		}},
		{name: "retire", call: func() error { _, err := svc.Retire(ctx, "sender", 1, "actor", "valid reason"); return err }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("mutation panicked without store: %v", recovered)
				}
			}()
			err := tc.call()
			if err == nil || !strings.Contains(err.Error(), "sender store is required") {
				t.Fatalf("mutation error=%v want fail-closed store error", err)
			}
		})
	}
}
