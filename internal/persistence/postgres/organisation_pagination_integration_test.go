package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/organisation"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLOrganisationPaginationContinuesWithoutDuplicates(t *testing.T) {
	dsn := os.Getenv("POSTGRES_PAGINATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PAGINATION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	repo := &OrganisationRepository{DB: db}
	base := time.Date(2099, 8, 7, 12, 0, 0, 0, time.UTC)
	ids := make([]string, 3)
	for i := range ids {
		if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM organisations WHERE id IN ($1::uuid,$2::uuid,$3::uuid)`, ids[0], ids[1], ids[2])
	}()
	for i, id := range ids {
		value := organisation.Organisation{ID: id, LegalName: "Pagination Org " + id, Status: organisation.StatusUnderReview, CreatedAt: base.Add(time.Duration(i) * time.Minute), UpdatedAt: base.Add(time.Duration(i) * time.Minute), Version: 1}
		if err := repo.Create(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	service := organisation.NewService(repo)
	seen := map[string]bool{}
	cursor := ""
	want := []string{ids[2], ids[1], ids[0]}
	for pageIndex, wantID := range want {
		page, pageErr := service.ListPage(ctx, 1, cursor)
		if pageErr != nil {
			t.Fatal(pageErr)
		}
		if len(page.Items) != 1 || page.Items[0].ID != wantID {
			t.Fatalf("page %d: got %#v want %s", pageIndex+1, page, wantID)
		}
		if seen[page.Items[0].ID] {
			t.Fatalf("duplicate organisation %s", page.Items[0].ID)
		}
		seen[page.Items[0].ID] = true
		if page.NextCursor == "" {
			t.Fatalf("page %d missing continuation cursor", pageIndex+1)
		}
		cursor = page.NextCursor
	}
}
