package metacloud

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLTemplateCatalogueAndImmutableBinding(t *testing.T) {
	dsn := os.Getenv("POSTGRES_META_CLOUD_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_META_CLOUD_DATABASE_URL is not set")
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

	var orgID, actorID, purposeID, campaignID, messageID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&orgID, &actorID, &purposeID, &campaignID, &messageID); err != nil {
		t.Fatal(err)
	}
	suffix := orgID[:8]
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Meta Template Actor','ACTIVE',false)`, actorID, "meta-template-"+suffix+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Meta template "+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Meta template','WHATSAPP','v1')`, purposeID, orgID, "META_"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Meta template',$3::uuid,'DRAFT',10)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 12, 0, 30, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,destination_links,variables,content_hash,client_request_id,status,created_by,approved_by,created_at,approved_at) VALUES($1::uuid,$2::uuid,1,'TEXT','Hello {{first_name}}','[]'::jsonb,'[{"name":"first_name","dataType":"TEXT","fallback":""}]'::jsonb,$3,$4,'APPROVED',$5::uuid,$5::uuid,$6,$6)`, messageID, campaignID, strings.Repeat("a", 64), "meta-template-"+suffix+"-0001", actorID, now); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		_, _ = db.ExecContext(cleanup, `DELETE FROM meta_cloud_message_bindings WHERE message_version_id=$1::uuid`, messageID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM meta_cloud_templates WHERE organisation_id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM message_versions WHERE id=$1::uuid`, messageID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()

	store := &PostgreSQLTemplateStore{DB: db}
	hash := hashEmptyComponents(t)
	values := []Template{{OrganisationID: orgID, WABAID: "waba-" + suffix, MetaTemplateID: "meta-1", Name: "hello", Language: "en_US", Category: "MARKETING", Status: "APPROVED", Components: []byte(`[]`), ComponentHash: hash, LastSyncedAt: now}, {OrganisationID: orgID, WABAID: "waba-" + suffix, MetaTemplateID: "meta-2", Name: "pending", Language: "en_US", Category: "MARKETING", Status: "PENDING", Components: []byte(`[]`), ComponentHash: hash, LastSyncedAt: now}}
	if err := store.ReplaceWABATemplates(ctx, orgID, "waba-"+suffix, values, now); err != nil {
		t.Fatal(err)
	}
	approved, err := store.ListApproved(ctx, orgID, "waba-"+suffix)
	if err != nil || len(approved) != 1 || approved[0].Name != "hello" {
		t.Fatalf("approved=%#v err=%v", approved, err)
	}

	binding := Binding{MessageVersionID: messageID, TemplateName: "hello", Language: "en_US", BodyVariableNames: []string{"first_name"},
		ComponentBindings:     []ComponentBinding{{Type: "BODY", SubType: "TEXT", ParameterNames: []string{"first_name"}}},
		TemplateComponentHash: hash, CreatedBy: actorID, CreatedAt: now}
	created, err := store.CreateBinding(ctx, binding)
	if err != nil || created.MessageVersionID != messageID || len(created.ComponentBindings) != 1 {
		t.Fatalf("binding=%#v err=%v", created, err)
	}
	changed := binding
	changed.TemplateName = "other"
	if _, err := store.CreateBinding(ctx, changed); !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("changed binding err=%v", err)
	}
	loaded, err := store.GetBinding(ctx, messageID)
	if err != nil || loaded.TemplateName != "hello" || len(loaded.BodyVariableNames) != 1 || len(loaded.ComponentBindings) != 1 {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
}
