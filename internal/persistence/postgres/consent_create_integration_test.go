package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/consent"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLConsentReviewCreateAcceptsOptionalTextFields(t *testing.T) {
	dsn := os.Getenv("POSTGRES_PAGINATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PAGINATION_DATABASE_URL is not set")
	}
	db, e := sql.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if e = db.PingContext(ctx); e != nil {
		t.Fatal(e)
	}
	actor, org := complianceUUID(t, ctx, db), complianceUUID(t, ctx, db)
	if _, e = db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Consent Create Regression','DISABLED',false)`, actor, "consent-create-"+actor+"@internal.invalid"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'UNDER_REVIEW')`, org, "Consent Create Organisation "+org); e != nil {
		t.Fatal(e)
	}
	review, e := consent.NewReview(consent.CreateInput{OrganisationID: org, Name: "Consent Create Regression", Channel: "WHATSAPP", CreatedBy: actor}, time.Date(2099, 5, 6, 12, 0, 0, 0, time.UTC))
	if e != nil {
		t.Fatal(e)
	}
	repo := &ConsentRepository{DB: db}
	if e = repo.Create(ctx, review); e != nil {
		t.Fatal(e)
	}
	loaded, e := repo.Get(ctx, review.ID)
	if e != nil {
		t.Fatal(e)
	}
	if loaded.ID != review.ID || loaded.Name != review.Name {
		t.Fatalf("unexpected stored review: %#v", loaded)
	}
}
