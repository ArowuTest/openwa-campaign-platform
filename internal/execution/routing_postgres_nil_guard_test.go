package execution

import (
	"context"
	"testing"
)

func TestPostgreSQLRoutingPlanReadMethodsRejectMissingDatabase(t *testing.T) {
	store := &PostgreSQLRoutingPlanStore{}
	tests := []struct {
		name string
		run  func() error
	}{
		{"get", func() error { _, err := store.Get(context.Background(), "plan"); return err }},
		{"list campaign", func() error { _, err := store.ListByCampaign(context.Background(), "campaign"); return err }},
		{"list page", func() error { _, err := store.ListRoutingPlanPage(context.Background(), "campaign", 10, 0); return err }},
		{"reservations", func() error { _, err := store.Reservations(context.Background(), "plan"); return err }},
		{"pool report", func() error { _, err := store.PoolExecutionReport(context.Background(), "plan"); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("missing database panicked: %v", recovered)
				}
			}()
			if err := tt.run(); err == nil {
				t.Fatal("missing database was accepted")
			}
		})
	}
}
