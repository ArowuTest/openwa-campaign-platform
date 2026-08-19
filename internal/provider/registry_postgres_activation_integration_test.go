package provider

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLProviderCapabilityActivationUsesGovernedTransaction(t *testing.T) {
	dsn := os.Getenv("POSTGRES_PAGINATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PAGINATION_DATABASE_URL is not set")
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
	var creatorID, approverID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&creatorID, &approverID); err != nil {
		t.Fatal(err)
	}
	for _, user := range []struct{ id, email string }{
		{creatorID, "provider-creator-" + creatorID + "@internal.invalid"},
		{approverID, "provider-approver-" + approverID + "@internal.invalid"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 provider capability','DISABLED',false)`, user.id, user.email); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2099, 3, 2, 13, 0, 0, 0, time.UTC)
	service := &Service{
		Store: &PostgreSQLStore{DB: db},
		Clock: func() time.Time { return now },
	}
	created, err := service.CreateDraft(ctx, Definition{
		Provider: "OPENWA", Channel: ChannelWhatsApp, Engine: "BAILEYS",
		AdapterVersion: "0.13.0+task6", Capabilities: []Capability{CapabilitySendText},
	}, creatorID, "task six provider capability draft")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	submitted, err := service.Submit(ctx, created.ID, created.Version, creatorID, "task six provider capability submit")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	activated, err := service.Decide(ctx, submitted.ID, submitted.Version, true, approverID, "task six provider capability approval")
	if err != nil {
		t.Fatal(err)
	}
	if activated.Status != StatusActive || activated.ApprovedBy != approverID {
		t.Fatalf("unexpected activated definition: %#v", activated)
	}
	resolved, err := service.Store.Active(ctx, "OPENWA", ChannelWhatsApp, "BAILEYS", now)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != activated.ID {
		t.Fatalf("resolved provider definition=%s want=%s", resolved.ID, activated.ID)
	}
	events, err := service.ListEvents(ctx, activated.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Action != string(StatusActive) {
		t.Fatalf("unexpected provider events: %#v", events)
	}
	now = now.Add(time.Minute)
	rejectDraft, err := service.CreateDraft(ctx, Definition{
		Provider: "OPENWA", Channel: ChannelWhatsApp, Engine: "WHATSAPP_WEB_JS",
		AdapterVersion: "0.13.0+task6-reject", Capabilities: []Capability{CapabilitySendText},
		SubmittedBy: "forged", ApprovedBy: "forged",
	}, creatorID, "task six provider rejection draft")
	if err != nil {
		t.Fatal(err)
	}
	if rejectDraft.SubmittedBy != "" || rejectDraft.ApprovedBy != "" {
		t.Fatalf("draft persisted caller audit fields: %#v", rejectDraft)
	}
	now = now.Add(time.Minute)
	rejectSubmitted, err := service.Submit(ctx, rejectDraft.ID, rejectDraft.Version, creatorID, "task six provider rejection submit")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	rejected, err := service.Decide(ctx, rejectSubmitted.ID, rejectSubmitted.Version, false, approverID, "task six provider rejection decision")
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Status != StatusRejected || rejected.ApprovedBy != approverID {
		t.Fatalf("unexpected rejected definition: %#v", rejected)
	}
	rejectEvents, err := service.ListEvents(ctx, rejected.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rejectEvents) != 3 || rejectEvents[0].Action != string(StatusRejected) || rejectEvents[0].ActorID != approverID {
		t.Fatalf("unexpected reject events: %#v", rejectEvents)
	}
}
