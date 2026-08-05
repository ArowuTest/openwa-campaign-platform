BEGIN;

ALTER TABLE audience_import_merge_results
  ADD COLUMN IF NOT EXISTS conflicts bigint NOT NULL DEFAULT 0 CHECK (conflicts >= 0);


CREATE TABLE IF NOT EXISTS audience_source_trust_policies (
  organisation_id uuid NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
  source_system text NOT NULL,
  trust_level smallint NOT NULL CHECK (trust_level BETWEEN 0 AND 100),
  reason text NOT NULL CHECK (length(trim(reason)) >= 8),
  updated_by uuid NOT NULL REFERENCES internal_users(id),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (organisation_id,source_system),
  CHECK (source_system=upper(trim(source_system)) AND length(source_system) BETWEEN 2 AND 80)
);

CREATE TABLE IF NOT EXISTS audience_source_trust_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organisation_id uuid NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
  source_system text NOT NULL,
  trust_level smallint NOT NULL CHECK (trust_level BETWEEN 0 AND 100),
  reason text NOT NULL,
  actor_id uuid NOT NULL REFERENCES internal_users(id),
  policy_version bigint NOT NULL CHECK (policy_version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (organisation_id,source_system,policy_version)
);

CREATE OR REPLACE FUNCTION record_audience_source_trust_event()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO audience_source_trust_events(organisation_id,source_system,trust_level,reason,actor_id,policy_version,created_at)
  VALUES(NEW.organisation_id,NEW.source_system,NEW.trust_level,NEW.reason,NEW.updated_by,NEW.version,NEW.updated_at);
  RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS trg_record_audience_source_trust_event ON audience_source_trust_policies;
CREATE TRIGGER trg_record_audience_source_trust_event
AFTER INSERT OR UPDATE ON audience_source_trust_policies
FOR EACH ROW EXECUTE FUNCTION record_audience_source_trust_event();

CREATE TABLE IF NOT EXISTS audience_profile_conflicts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  audience_import_id uuid NOT NULL REFERENCES audience_imports(id) ON DELETE CASCADE,
  contact_id uuid NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
  masked_msisdn text NOT NULL,
  field_name text NOT NULL CHECK (field_name IN ('country_id','state_id','lga_id','reported_age','gender_code')),
  existing_value text,
  incoming_value text,
  status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','RESOLVED','REJECTED')),
  resolution text CHECK (resolution IS NULL OR resolution IN ('KEEP_EXISTING','USE_INCOMING')),
  reason text,
  resolved_by uuid REFERENCES internal_users(id),
  resolved_at timestamptz,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (audience_import_id,contact_id,field_name),
  CHECK (
    (status='PENDING' AND resolution IS NULL AND resolved_by IS NULL AND resolved_at IS NULL)
    OR
    (status IN ('RESOLVED','REJECTED') AND resolution IS NOT NULL AND resolved_by IS NOT NULL AND resolved_at IS NOT NULL AND length(trim(reason)) >= 8)
  )
);

CREATE INDEX IF NOT EXISTS idx_audience_profile_conflicts_import_status
  ON audience_profile_conflicts(audience_import_id,status,created_at,id);
CREATE INDEX IF NOT EXISTS idx_audience_profile_conflicts_contact_pending
  ON audience_profile_conflicts(contact_id,created_at,id)
  WHERE status='PENDING';

-- Conflict evidence is append-only except for the controlled pending-to-terminal
-- resolution transition. Deletion would destroy import lineage and is forbidden.
CREATE OR REPLACE FUNCTION prevent_audience_profile_conflict_delete()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audience profile conflict evidence cannot be deleted';
END;
$$;

DROP TRIGGER IF EXISTS trg_prevent_audience_profile_conflict_delete ON audience_profile_conflicts;
CREATE TRIGGER trg_prevent_audience_profile_conflict_delete
BEFORE DELETE ON audience_profile_conflicts
FOR EACH ROW EXECUTE FUNCTION prevent_audience_profile_conflict_delete();

COMMIT;
