package execution

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLRoutingCreateClassifiesMissingCampaignAsInvalid(t *testing.T) {
	dsn := os.Getenv("POSTGRES_EXECUTION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_EXECUTION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	missingCampaignID := newDatabaseUUID(t, db, ctx)
	_, err = (&PostgreSQLRoutingPlanStore{DB: db}).Create(ctx, RoutingPlan{
		CampaignID: missingCampaignID,
	}, nil)
	if !errors.Is(err, ErrRoutingPlanInvalid) {
		t.Fatalf("missing campaign classified as %v, want ErrRoutingPlanInvalid", err)
	}
}
