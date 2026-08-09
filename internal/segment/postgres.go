package segment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	pgretry "campaign-platform/internal/persistence/postgres"
)

type PostgreSQLStore struct{ DB *sql.DB }

func (s *PostgreSQLStore) Create(ctx context.Context, snapshot Snapshot) error {
	return s.CreateWithMembers(ctx, snapshot, nil)
}

func (s *PostgreSQLStore) CreateWithMembers(ctx context.Context, snapshot Snapshot, members []Member) error {
	_, _, err := s.EnsureWithMembers(ctx, snapshot, members)
	return err
}

// EnsureWithMembers creates one immutable snapshot or returns the previously
// committed snapshot for the same campaign/hash. A conflicting replay is
// rejected rather than silently reusing evidence with different policy metadata.
func (s *PostgreSQLStore) EnsureWithMembers(ctx context.Context, snapshot Snapshot, members []Member) (Snapshot, bool, error) {
	result, err := pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (snapshotResult, error) {
		stored, created, operationErr := s.ensureWithMembersOnce(ctx, snapshot, members)
		return snapshotResult{Snapshot: stored, Created: created}, operationErr
	})
	if err != nil {
		return Snapshot{}, false, err
	}
	return result.Snapshot, result.Created, nil
}

type snapshotResult struct {
	Snapshot Snapshot
	Created  bool
}

func (s *PostgreSQLStore) ensureWithMembersOnce(ctx context.Context, snapshot Snapshot, members []Member) (Snapshot, bool, error) {
	if s == nil || s.DB == nil {
		return Snapshot{}, false, errors.New("database is required")
	}
	if err := validateSnapshotIntegrity(snapshot, members); err != nil {
		return Snapshot{}, false, err
	}
	definition, err := json.Marshal(snapshot.Definition)
	if err != nil {
		return Snapshot{}, false, err
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Snapshot{}, false, err
	}
	defer tx.Rollback()

	var insertedID string
	err = tx.QueryRowContext(ctx, `
INSERT INTO audience_snapshots(id,campaign_id,segment_id,segment_definition,definition_version,consent_policy_version,configuration_version,snapshot_hash,eligible_count,created_by,created_at)
VALUES($1::uuid,$2::uuid,NULLIF($3,'')::uuid,$4::jsonb,$5,$6,$7,$8,$9,NULLIF($10,'')::uuid,$11)
ON CONFLICT(campaign_id,snapshot_hash) DO NOTHING
RETURNING id::text`, snapshot.ID, snapshot.CampaignID, snapshot.SegmentID, string(definition), snapshot.DefinitionVersion, snapshot.ConsentPolicyVersion, snapshot.ConfigurationVersion, snapshot.SnapshotHash, snapshot.EligibleCount, snapshot.CreatedBy, snapshot.CreatedAt).Scan(&insertedID)
	if errors.Is(err, sql.ErrNoRows) {
		existing, loadErr := getSnapshotTx(ctx, tx, snapshot.CampaignID, snapshot.SnapshotHash)
		if loadErr != nil {
			return Snapshot{}, false, loadErr
		}
		if !sameSnapshotEvidence(existing, snapshot) {
			return Snapshot{}, false, ErrSnapshotConflict
		}
		var memberCount int64
		if countErr := tx.QueryRowContext(ctx, `SELECT count(*) FROM audience_snapshot_members WHERE snapshot_id=$1::uuid`, existing.ID).Scan(&memberCount); countErr != nil {
			return Snapshot{}, false, countErr
		}
		if memberCount != int64(len(members)) {
			return Snapshot{}, false, ErrSnapshotConflict
		}
		if err := tx.Commit(); err != nil {
			return Snapshot{}, false, err
		}
		return existing, false, nil
	}
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("insert audience snapshot: %w", err)
	}

	const batchSize = 5_000
	for start := 0; start < len(members); start += batchSize {
		end := start + batchSize
		if end > len(members) {
			end = len(members)
		}
		payload, marshalErr := json.Marshal(members[start:end])
		if marshalErr != nil {
			return Snapshot{}, false, marshalErr
		}
		result, insertErr := tx.ExecContext(ctx, `
INSERT INTO audience_snapshot_members(snapshot_id,contact_id,eligibility_evidence)
SELECT $1::uuid,x."contactId"::uuid,jsonb_build_object('hash',x."eligibilityEvidenceHash")
FROM jsonb_to_recordset($2::jsonb) AS x("contactId" text,"eligibilityEvidenceHash" text)
ON CONFLICT(snapshot_id,contact_id) DO NOTHING`, snapshot.ID, string(payload))
		if insertErr != nil {
			return Snapshot{}, false, fmt.Errorf("insert snapshot member batch: %w", insertErr)
		}
		inserted, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return Snapshot{}, false, rowsErr
		}
		if inserted != int64(end-start) {
			return Snapshot{}, false, ErrSnapshotConflict
		}
	}
	if err := tx.Commit(); err != nil {
		return Snapshot{}, false, err
	}
	return snapshot, true, nil
}

func getSnapshotTx(ctx context.Context, tx *sql.Tx, campaignID, snapshotHash string) (Snapshot, error) {
	var value Snapshot
	var segmentID sql.NullString
	var definition []byte
	err := tx.QueryRowContext(ctx, `SELECT id::text,campaign_id::text,segment_id::text,segment_definition,definition_version,consent_policy_version,configuration_version,eligible_count,snapshot_hash,coalesce(created_by::text,''),created_at FROM audience_snapshots WHERE campaign_id=$1::uuid AND snapshot_hash=$2 FOR SHARE`, campaignID, snapshotHash).Scan(&value.ID, &value.CampaignID, &segmentID, &definition, &value.DefinitionVersion, &value.ConsentPolicyVersion, &value.ConfigurationVersion, &value.EligibleCount, &value.SnapshotHash, &value.CreatedBy, &value.CreatedAt)
	if err != nil {
		return Snapshot{}, err
	}
	if segmentID.Valid {
		value.SegmentID = segmentID.String
	}
	if err := json.Unmarshal(definition, &value.Definition); err != nil {
		return Snapshot{}, err
	}
	return value, nil
}

func (s *PostgreSQLStore) Get(ctx context.Context, identifier string) (Snapshot, error) {
	if s == nil || s.DB == nil {
		return Snapshot{}, errors.New("database is required")
	}
	var value Snapshot
	var segmentID sql.NullString
	var definition []byte
	err := s.DB.QueryRowContext(ctx, `SELECT id::text,campaign_id::text,segment_id::text,segment_definition,definition_version,consent_policy_version,configuration_version,eligible_count,snapshot_hash,coalesce(created_by::text,''),created_at FROM audience_snapshots WHERE id=$1::uuid`, identifier).Scan(&value.ID, &value.CampaignID, &segmentID, &definition, &value.DefinitionVersion, &value.ConsentPolicyVersion, &value.ConfigurationVersion, &value.EligibleCount, &value.SnapshotHash, &value.CreatedBy, &value.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, ErrSnapshotNotFound
	}
	if err != nil {
		return Snapshot{}, err
	}
	if segmentID.Valid {
		value.SegmentID = segmentID.String
	}
	if err := json.Unmarshal(definition, &value.Definition); err != nil {
		return Snapshot{}, err
	}
	return value, nil
}

func (s *PostgreSQLStore) Members(ctx context.Context, snapshotID, afterContactID string, limit int) ([]Member, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT contact_id::text,coalesce(eligibility_evidence->>'hash','') FROM audience_snapshot_members WHERE snapshot_id=$1::uuid AND ($2='' OR contact_id>NULLIF($2,'')::uuid) ORDER BY contact_id LIMIT $3`, snapshotID, afterContactID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Member{}
	for rows.Next() {
		var value Member
		if err := rows.Scan(&value.ContactID, &value.EligibilityEvidenceHash); err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, rows.Err()
}

func (s *PostgreSQLStore) Overlap(ctx context.Context, leftID, rightID string) (SnapshotOverlap, error) {
	if s == nil || s.DB == nil {
		return SnapshotOverlap{}, errors.New("database is required")
	}
	var result SnapshotOverlap
	result.LeftSnapshotID, result.RightSnapshotID = leftID, rightID
	var exists int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM audience_snapshots WHERE id IN ($1::uuid,$2::uuid)`, leftID, rightID).Scan(&exists); err != nil {
		return SnapshotOverlap{}, err
	}
	if exists != 2 {
		return SnapshotOverlap{}, ErrSnapshotNotFound
	}
	err := s.DB.QueryRowContext(ctx, `WITH l AS (SELECT contact_id FROM audience_snapshot_members WHERE snapshot_id=$1::uuid), r AS (SELECT contact_id FROM audience_snapshot_members WHERE snapshot_id=$2::uuid), counts AS (SELECT (SELECT count(*) FROM l) left_count,(SELECT count(*) FROM r) right_count,(SELECT count(*) FROM l JOIN r USING(contact_id)) intersection) SELECT left_count,right_count,intersection,left_count-intersection,right_count-intersection,left_count+right_count-intersection FROM counts`, leftID, rightID).Scan(&result.LeftCount, &result.RightCount, &result.Intersection, &result.OnlyLeft, &result.OnlyRight, &result.Union)
	if err != nil {
		return SnapshotOverlap{}, fmt.Errorf("calculate snapshot overlap: %w", err)
	}
	if result.Union > 0 {
		result.OverlapPercent = float64(result.Intersection) * 100 / float64(result.Union)
	}
	return result, nil
}
