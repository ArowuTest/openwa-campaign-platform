package storage

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLTrustedAssetRightsPersistAcrossScanAndPermitRenewedAuthority(t *testing.T) {
	dsn := os.Getenv("POSTGRES_ASSET_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_ASSET_DATABASE_URL is not set")
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

	var actor, firstID, secondID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&actor, &firstID, &secondID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Asset rights actor','DISABLED',false)`, actor, "asset-rights-"+actor+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM trusted_asset_events WHERE asset_id IN ($1::uuid,$2::uuid)`, firstID, secondID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM trusted_assets WHERE id IN ($1::uuid,$2::uuid)`, firstID, secondID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actor)
	}()

	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	expires := now.Add(30 * 24 * time.Hour)
	repo := &PostgreSQLAssetRepository{DB: db}
	first, err := repo.Create(ctx, Asset{
		ID: firstID, Purpose: AssetPurposeMessageMedia, ObjectKey: "trusted-assets/message_media/" + firstID + "/creative.png",
		OriginalFilename: "creative.png", MediaType: "image/png", Size: 1234,
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Status: AssetStatusScanning, RightsBasis: RightsBasisLicensed,
		RightsReference: "licence/creative/v1", RightsExpiresAt: &expires,
		CreatedBy: actor, CreatedAt: now, UpdatedAt: now, Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err = repo.UpdateScan(ctx, first.ID, first.Version, AssetStatusClean, "", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := repo.Get(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.RightsBasis != RightsBasisLicensed || reloaded.RightsReference != "licence/creative/v1" ||
		reloaded.RightsExpiresAt == nil || !reloaded.RightsExpiresAt.Equal(expires) {
		t.Fatalf("rights evidence lost after scan transition: %+v", reloaded)
	}

	renewed := expires.Add(90 * 24 * time.Hour)
	second, err := repo.Create(ctx, Asset{
		ID: secondID, Purpose: AssetPurposeMessageMedia, ObjectKey: "trusted-assets/message_media/" + secondID + "/creative.png",
		OriginalFilename: "creative.png", MediaType: "image/png", Size: 1234,
		SHA256: first.SHA256, Status: AssetStatusScanning, RightsBasis: RightsBasisLicensed,
		RightsReference: "licence/creative/v2", RightsExpiresAt: &renewed,
		CreatedBy: actor, CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second), Version: 1,
	})
	if err != nil {
		t.Fatalf("same bytes under renewed immutable rights evidence should be accepted: %v", err)
	}
	if second.ID == first.ID || second.RightsReference == first.RightsReference {
		t.Fatalf("renewed authority did not produce distinct governed asset: first=%+v second=%+v", first, second)
	}
}
