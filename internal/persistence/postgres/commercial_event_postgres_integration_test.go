package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/commercial"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLCommercialEventsAreAtomicOrderedAndAppendOnly(t *testing.T) {
	dsn := os.Getenv("POSTGRES_COMMERCIAL_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_COMMERCIAL_DATABASE_URL is not set")
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

	var maker, checker, orgID, purposeID, campaignID string
	if err := db.QueryRowContext(ctx, "SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text").
		Scan(&maker, &checker, &orgID, &purposeID, &campaignID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Commercial Maker','DISABLED',false),($3::uuid,$4,'Commercial Checker','DISABLED',false)",
		maker, "commercial-maker-"+maker+"@internal.invalid", checker, "commercial-checker-"+checker+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')", orgID, "Commercial Event Organisation "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,'COMMERCIAL_EVENT','Commercial Event','WHATSAPP','v1')", purposeID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,maximum_messages_per_recipient) VALUES($1::uuid,$2::uuid,$3,$4::uuid,'MESSAGE_APPROVED',100,1)", campaignID, orgID, "Commercial Event Campaign "+campaignID, purposeID); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 11, 9, 0, 0, 123456000, time.UTC)
	paid := now.Add(-time.Hour)
	service := &commercial.Service{Store: &CommercialRepository{DB: db}, Clock: func() time.Time { return now }}
	draft, err := service.CreateDraft(ctx, commercial.Record{
		CampaignID: campaignID, OrganisationID: orgID,
		QuotationReference: "Q-EVENT", InvoiceReference: "I-EVENT", Currency: "NGN",
		ApprovedRecipients: 100, UnitPriceMinor: 50, ManagementFeeMinor: 1000, TotalAmountMinor: 6000,
		PaymentReference: "PAY-EVENT", PaymentReceivedAt: &paid,
	}, maker, "create governed commercial evidence")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.Submit(ctx, draft.ID, draft.Version, maker, "submit commercial evidence")
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.Decide(ctx, pending.ID, pending.Version, true, checker, "independent payment verification")
	if err != nil {
		t.Fatal(err)
	}

	events, err := service.Events(ctx, approved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].RecordVersion != 1 || events[1].RecordVersion != 2 || events[2].RecordVersion != 3 {
		t.Fatalf("commercial events not version ordered: %+v", events)
	}
	amount, ok := events[2].Evidence["totalAmountMinor"].(json.Number)
	if !ok || amount.String() != "6000" {
		t.Fatalf("exact monetary evidence lost: %#v", events[2].Evidence)
	}
	if events[2].Evidence["status"] != string(commercial.StatusApproved) {
		t.Fatalf("approved status evidence missing: %#v", events[2].Evidence)
	}

	if _, err := db.ExecContext(ctx, "UPDATE campaign_commercial_events SET reason='tamper' WHERE commercial_record_id=$1::uuid", approved.ID); err == nil {
		t.Fatal("commercial event history allowed UPDATE")
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM campaign_commercial_events WHERE commercial_record_id=$1::uuid", approved.ID); err == nil {
		t.Fatal("commercial event history allowed DELETE")
	}
	if _, err := db.ExecContext(ctx, "TRUNCATE TABLE campaign_commercial_events"); err == nil {
		t.Fatal("commercial event history allowed TRUNCATE")
	}

	var versions int
	if err := db.QueryRowContext(ctx, "SELECT count(DISTINCT record_version) FROM campaign_commercial_events WHERE commercial_record_id=$1::uuid", approved.ID).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 3 {
		t.Fatalf("record-version uniqueness evidence count=%d want 3", versions)
	}
}
