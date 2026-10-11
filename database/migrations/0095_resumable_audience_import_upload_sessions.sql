BEGIN;

CREATE TABLE audience_import_upload_sessions (
  id uuid PRIMARY KEY,
  organisation_id uuid NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  consent_review_id uuid NOT NULL REFERENCES consent_reviews(id) ON DELETE RESTRICT,
  purpose_id uuid NOT NULL REFERENCES consent_purposes(id) ON DELETE RESTRICT,
  channel text NOT NULL,
  wording_version text NOT NULL,
  source_name text NOT NULL,
  source_system text,
  default_country_iso2 char(2),
  original_filename text NOT NULL,
  template_version text NOT NULL,
  mapping_definition_id uuid REFERENCES audience_import_mapping_definitions(id) ON DELETE RESTRICT,
  mapping jsonb NOT NULL,
  update_policy text NOT NULL CHECK (update_policy IN ('INSERT_ONLY','FILL_NULL','NEWEST_SOURCE','TRUSTED_SOURCE','MANUAL_CONFLICT')),
  uploaded_by uuid NOT NULL REFERENCES internal_users(id) ON DELETE RESTRICT,
  client_request_id text NOT NULL,
  expected_bytes bigint NOT NULL CHECK (expected_bytes > 0),
  uploaded_bytes bigint NOT NULL DEFAULT 0 CHECK (uploaded_bytes >= 0),
  part_size bigint NOT NULL CHECK (part_size BETWEEN 5242880 AND 33554432),
  part_count integer NOT NULL CHECK (part_count > 0 AND part_count <= 1024),
  uploaded_parts integer NOT NULL DEFAULT 0 CHECK (uploaded_parts >= 0),
  state text NOT NULL CHECK (state IN ('CREATED','UPLOADING','UPLOADED','FINALISING','IMPORT_CREATED','ABORTED','EXPIRED','FAILED')),
  failure_reason text,
  expires_at timestamptz NOT NULL,
  final_sha256 text,
  detected_media_type text,
  linked_import_id uuid UNIQUE REFERENCES audience_imports(id) ON DELETE RESTRICT,
  finaliser_lease_owner text,
  finaliser_lease_version bigint NOT NULL DEFAULT 0 CHECK (finaliser_lease_version >= 0),
  finaliser_lease_expires_at timestamptz,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  UNIQUE (organisation_id, client_request_id),
  CHECK (uploaded_bytes <= expected_bytes),
  CHECK (uploaded_parts <= part_count),
  CHECK ((state IN ('CREATED','UPLOADING') AND linked_import_id IS NULL) OR state NOT IN ('CREATED','UPLOADING')),
  CHECK ((state='IMPORT_CREATED' AND linked_import_id IS NOT NULL) OR state<>'IMPORT_CREATED'),
  CHECK ((final_sha256 IS NULL) OR final_sha256 ~ '^[0-9a-f]{64}$'),
  CHECK ((finaliser_lease_owner IS NULL) = (finaliser_lease_expires_at IS NULL))
);

CREATE INDEX audience_import_upload_sessions_active_idx
  ON audience_import_upload_sessions(state,expires_at,id)
  WHERE state IN ('CREATED','UPLOADING','UPLOADED','FINALISING');

CREATE INDEX audience_import_upload_sessions_finalise_idx
  ON audience_import_upload_sessions(state,finaliser_lease_expires_at,updated_at,id)
  WHERE state IN ('UPLOADED','FINALISING');

ALTER TABLE audience_imports
  ADD COLUMN upload_session_id uuid UNIQUE REFERENCES audience_import_upload_sessions(id) ON DELETE RESTRICT;

CREATE TABLE audience_import_upload_parts (
  session_id uuid NOT NULL REFERENCES audience_import_upload_sessions(id) ON DELETE CASCADE,
  part_number integer NOT NULL CHECK (part_number > 0 AND part_number <= 1024),
  offset_bytes bigint NOT NULL CHECK (offset_bytes >= 0),
  expected_bytes bigint NOT NULL CHECK (expected_bytes > 0 AND expected_bytes <= 33554432),
  object_key text NOT NULL UNIQUE,
  state text NOT NULL CHECK (state IN ('PENDING','UPLOADED')),
  sha256 text,
  uploaded_bytes bigint NOT NULL DEFAULT 0 CHECK (uploaded_bytes >= 0),
  uploaded_at timestamptz,
  PRIMARY KEY (session_id,part_number),
  CHECK (
    (state='PENDING' AND sha256 IS NULL AND uploaded_bytes=0 AND uploaded_at IS NULL)
    OR
    (state='UPLOADED' AND sha256 ~ '^[0-9a-f]{64}$' AND uploaded_bytes=expected_bytes AND uploaded_at IS NOT NULL)
  )
);

CREATE INDEX audience_import_upload_parts_session_state_idx
  ON audience_import_upload_parts(session_id,state,part_number);

CREATE FUNCTION prevent_audience_import_uploaded_part_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.state='UPLOADED' AND (
    NEW.state IS DISTINCT FROM OLD.state OR
    NEW.offset_bytes IS DISTINCT FROM OLD.offset_bytes OR
    NEW.expected_bytes IS DISTINCT FROM OLD.expected_bytes OR
    NEW.object_key IS DISTINCT FROM OLD.object_key OR
    NEW.sha256 IS DISTINCT FROM OLD.sha256 OR
    NEW.uploaded_bytes IS DISTINCT FROM OLD.uploaded_bytes OR
    NEW.uploaded_at IS DISTINCT FROM OLD.uploaded_at
  ) THEN
    RAISE EXCEPTION 'uploaded audience import parts are immutable';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER audience_import_upload_parts_immutable
BEFORE UPDATE ON audience_import_upload_parts
FOR EACH ROW EXECUTE FUNCTION prevent_audience_import_uploaded_part_rewrite();

COMMIT;
