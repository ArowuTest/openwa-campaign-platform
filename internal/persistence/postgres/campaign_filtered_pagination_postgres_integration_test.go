package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLCampaignFilteredPageScopesBeforePagination(t *testing.T) {
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

	ids := make([]string, 10)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	org1, review1, purpose1 := ids[0], ids[1], ids[2]
	org2, review2, purpose2 := ids[3], ids[4], ids[5]
	c1, c2, c3, c4 := ids[6], ids[7], ids[8], ids[9]
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id = ANY($1::uuid[])`, []string{c1, c2, c3, c4})
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id = ANY($1::uuid[])`, []string{purpose1, purpose2})
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_reviews WHERE id = ANY($1::uuid[])`, []string{review1, review2})
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id = ANY($1::uuid[])`, []string{org1, org2})
	}()

	for _, fixture := range []struct{ org, review, purpose, suffix string }{
		{org1, review1, purpose1, "one"},
		{org2, review2, purpose2, "two"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, fixture.org, "Campaign filter "+fixture.suffix); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,status) VALUES($1::uuid,$2::uuid,$3,'WHATSAPP','DIRECT','v1','DRAFT')`, fixture.review, fixture.org, "Review "+fixture.suffix); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,$4,'WHATSAPP','v1',$5::uuid)`, fixture.purpose, fixture.org, "FILTER_"+fixture.suffix+"_"+fixture.purpose, "Purpose "+fixture.suffix, fixture.review); err != nil {
			t.Fatal(err)
		}
	}

	repository := &CampaignRepository{DB: db}
	base := time.Date(2099, 3, 1, 12, 0, 0, 0, time.UTC)
	for _, item := range []campaign.Campaign{
		{ID: c1, OrganisationID: org1, Name: "Org1 build newest", PurposeID: purpose1, ConsentReviewID: review1, Status: campaign.StatusAudienceBuilding, Timezone: "UTC", MaximumUniqueRecipients: 100, MaximumMessagesPerRecipient: 1, CreatedAt: base.Add(4 * time.Minute), UpdatedAt: base.Add(4 * time.Minute), Version: 1},
		{ID: c2, OrganisationID: org1, Name: "Org1 build older", PurposeID: purpose1, ConsentReviewID: review1, Status: campaign.StatusAudienceBuilding, Timezone: "UTC", MaximumUniqueRecipients: 100, MaximumMessagesPerRecipient: 1, CreatedAt: base.Add(3 * time.Minute), UpdatedAt: base.Add(3 * time.Minute), Version: 1},
		{ID: c3, OrganisationID: org1, Name: "Org1 draft", PurposeID: purpose1, ConsentReviewID: review1, Status: campaign.StatusDraft, Timezone: "UTC", MaximumUniqueRecipients: 100, MaximumMessagesPerRecipient: 1, CreatedAt: base.Add(2 * time.Minute), UpdatedAt: base.Add(2 * time.Minute), Version: 1},
		{ID: c4, OrganisationID: org2, Name: "Org2 build", PurposeID: purpose2, ConsentReviewID: review2, Status: campaign.StatusAudienceBuilding, Timezone: "UTC", MaximumUniqueRecipients: 100, MaximumMessagesPerRecipient: 1, CreatedAt: base.Add(time.Minute), UpdatedAt: base.Add(time.Minute), Version: 1},
	} {
		if err := repository.Create(ctx, item); err != nil {
			t.Fatal(err)
		}
	}

	service := campaign.NewService(repository)
	filter := campaign.ListFilter{OrganisationID: org1, Status: campaign.StatusAudienceBuilding}
	first, err := service.ListFiltered(ctx, filter, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.Items[0].ID != c1 || first.NextCursor == "" {
		t.Fatalf("first page=%+v", first)
	}
	second, err := service.ListFiltered(ctx, filter, 1, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != c2 || second.NextCursor != "" {
		t.Fatalf("second page=%+v", second)
	}
	for _, item := range append(append([]campaign.Campaign{}, first.Items...), second.Items...) {
		if item.OrganisationID != org1 || item.Status != campaign.StatusAudienceBuilding {
			t.Fatalf("filtered page leaked campaign: %+v", item)
		}
	}
}
