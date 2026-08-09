package orchestration

import (
	"testing"
)

func TestPostgreSQLCampaignEvidenceRejectsSnapshotAndApprovedMessageMutation(t *testing.T) {
	db, ctx, cancel := openPostgresReleaseDB(t)
	defer cancel()
	defer db.Close()
	fixture := seedPostgresReleaseFixture(t, ctx, db, 1, 1)

	if _, err := db.ExecContext(ctx, `UPDATE audience_snapshots SET eligible_count=eligible_count+1 WHERE id=$1::uuid`, fixture.SnapshotID); err == nil {
		t.Fatal("immutable audience snapshot accepted an update")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM audience_snapshots WHERE id=$1::uuid`, fixture.SnapshotID); err == nil {
		t.Fatal("immutable audience snapshot accepted a delete")
	}
	if _, err := db.ExecContext(ctx, `UPDATE message_versions SET body='mutated after approval' WHERE id=$1::uuid`, fixture.MessageID); err == nil {
		t.Fatal("approved message version accepted a material mutation")
	}
}
