BEGIN;

ALTER TABLE segments
  ADD COLUMN IF NOT EXISTS updated_by uuid REFERENCES internal_users(id),
  ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1 CHECK (version > 0);

UPDATE segments SET version=greatest(version,definition_version);

CREATE UNIQUE INDEX IF NOT EXISTS uq_segments_active_name_per_org
  ON segments(organisation_id,lower(name)) WHERE status <> 'ARCHIVED';

CREATE TABLE IF NOT EXISTS segment_definition_versions (
  segment_id uuid NOT NULL REFERENCES segments(id),
  version bigint NOT NULL CHECK (version > 0),
  name text NOT NULL,
  description text,
  definition jsonb NOT NULL,
  status text NOT NULL CHECK (status IN ('DRAFT','ACTIVE','ARCHIVED')),
  changed_by uuid NOT NULL REFERENCES internal_users(id),
  reason text NOT NULL CHECK (length(trim(reason)) > 0),
  created_at timestamptz NOT NULL,
  PRIMARY KEY(segment_id,version)
);

CREATE INDEX IF NOT EXISTS idx_segment_definition_versions_history
  ON segment_definition_versions(segment_id,version DESC);

CREATE OR REPLACE FUNCTION prevent_segment_definition_version_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'segment definition version evidence is append-only';
END $$;

DROP TRIGGER IF EXISTS trg_prevent_segment_definition_version_update ON segment_definition_versions;
CREATE TRIGGER trg_prevent_segment_definition_version_update
BEFORE UPDATE OR DELETE ON segment_definition_versions
FOR EACH ROW EXECUTE FUNCTION prevent_segment_definition_version_mutation();

COMMIT;
