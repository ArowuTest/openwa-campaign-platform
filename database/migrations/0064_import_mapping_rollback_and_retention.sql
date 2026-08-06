BEGIN;

CREATE TABLE audience_import_mapping_definitions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organisation_id uuid REFERENCES organisations(id),
  name text NOT NULL,
  source_system text,
  template_version text NOT NULL,
  worksheet text,
  mapping jsonb NOT NULL,
  status text NOT NULL CHECK (status IN ('DRAFT','PENDING_APPROVAL','ACTIVE','REJECTED','RETIRED')),
  effective_from timestamptz NOT NULL,
  effective_to timestamptz,
  created_by uuid NOT NULL REFERENCES internal_users(id),
  submitted_by uuid REFERENCES internal_users(id),
  approved_by uuid REFERENCES internal_users(id),
  reason text NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CHECK (effective_to IS NULL OR effective_to > effective_from)
);
CREATE INDEX audience_import_mapping_resolution_idx ON audience_import_mapping_definitions(organisation_id,upper(coalesce(source_system,'')),status,effective_from DESC,id DESC);
CREATE UNIQUE INDEX audience_import_mapping_active_idx ON audience_import_mapping_definitions(coalesce(organisation_id,'00000000-0000-0000-0000-000000000000'::uuid),upper(coalesce(source_system,'')),lower(name)) WHERE status='ACTIVE' AND effective_to IS NULL;
ALTER TABLE audience_import_mapping_definitions ADD CONSTRAINT audience_import_mapping_active_period_exclusion EXCLUDE USING gist (coalesce(organisation_id,'00000000-0000-0000-0000-000000000000'::uuid) WITH =, upper(coalesce(source_system,'')) WITH =, lower(name) WITH =, tstzrange(effective_from,coalesce(effective_to,'infinity'::timestamptz),'[)') WITH &&) WHERE (status='ACTIVE');

CREATE TABLE audience_import_mapping_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  mapping_id uuid NOT NULL REFERENCES audience_import_mapping_definitions(id) ON DELETE CASCADE,
  event_type text NOT NULL,
  actor_id uuid NOT NULL REFERENCES internal_users(id),
  reason text,
  mapping_version bigint NOT NULL,
  occurred_at timestamptz NOT NULL
);
CREATE INDEX audience_import_mapping_events_idx ON audience_import_mapping_events(mapping_id,occurred_at DESC,id DESC);
CREATE FUNCTION prevent_audience_import_mapping_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'audience import mapping events are append-only'; END $$;
CREATE TRIGGER audience_import_mapping_events_append_only BEFORE UPDATE OR DELETE ON audience_import_mapping_events FOR EACH ROW EXECUTE FUNCTION prevent_audience_import_mapping_event_mutation();

ALTER TABLE audience_imports
  ADD COLUMN mapping_definition_id uuid REFERENCES audience_import_mapping_definitions(id),
  ADD COLUMN source_expires_at timestamptz,
  ADD COLUMN source_deleted_at timestamptz,
  ADD COLUMN rolled_back_at timestamptz,
  ADD COLUMN rolled_back_by uuid REFERENCES internal_users(id),
  ADD COLUMN rollback_reason text,
  ADD COLUMN source_deletion_lease_owner text,
  ADD COLUMN source_deletion_lease_expires_at timestamptz,
  ADD COLUMN source_deletion_attempts integer NOT NULL DEFAULT 0,
  ADD COLUMN source_deletion_last_error text;
ALTER TABLE audience_imports DROP CONSTRAINT IF EXISTS audience_imports_status_check;
ALTER TABLE audience_imports ADD CONSTRAINT audience_imports_status_check CHECK (status IN (
  'UPLOADED','SCANNING','QUARANTINED','VALIDATING','PREVIEW_READY','APPROVED','IMPORTING',
  'COMPLETED','COMPLETED_WITH_EXCEPTIONS','ROLLED_BACK','REJECTED','FAILED','CANCELLED'
));
CREATE INDEX audience_import_source_retention_idx ON audience_imports(source_expires_at,id) WHERE source_deleted_at IS NULL AND source_expires_at IS NOT NULL AND status IN ('COMPLETED','COMPLETED_WITH_EXCEPTIONS','ROLLED_BACK','REJECTED','FAILED','CANCELLED');

CREATE TABLE audience_import_source_deletion_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  audience_import_id uuid NOT NULL REFERENCES audience_imports(id) ON DELETE RESTRICT,
  event_type text NOT NULL CHECK (event_type IN ('SOURCE_DELETION_COMPLETED','SOURCE_DELETION_FAILED')),
  worker_id text NOT NULL,
  object_key_hash text NOT NULL,
  attempt integer NOT NULL CHECK (attempt > 0),
  failure_detail text,
  occurred_at timestamptz NOT NULL,
  CHECK ((event_type='SOURCE_DELETION_COMPLETED' AND failure_detail IS NULL) OR (event_type='SOURCE_DELETION_FAILED' AND failure_detail IS NOT NULL))
);
CREATE INDEX audience_import_source_deletion_events_idx ON audience_import_source_deletion_events(audience_import_id,occurred_at DESC,id DESC);
CREATE FUNCTION prevent_audience_import_source_deletion_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'audience import source deletion events are append-only'; END $$;
CREATE TRIGGER audience_import_source_deletion_events_append_only BEFORE UPDATE OR DELETE ON audience_import_source_deletion_events FOR EACH ROW EXECUTE FUNCTION prevent_audience_import_source_deletion_event_mutation();

CREATE TABLE audience_import_contact_mutations (
  audience_import_id uuid NOT NULL REFERENCES audience_imports(id) ON DELETE RESTRICT,
  contact_id uuid NOT NULL REFERENCES contacts(id) ON DELETE RESTRICT,
  was_inserted boolean NOT NULL,
  before_encrypted_msisdn bytea,
  before_masked_msisdn text,
  before_country_id uuid,
  before_state_id uuid,
  before_lga_id uuid,
  before_reported_age smallint,
  before_age_recorded_at date,
  before_age_source text,
  before_age_verified boolean,
  before_gender_code text,
  before_status text,
  before_source_system text,
  before_source_record_id text,
  before_profile_recorded_at timestamptz,
  recorded_at timestamptz NOT NULL,
  PRIMARY KEY (audience_import_id,contact_id),
  CHECK ((was_inserted AND before_encrypted_msisdn IS NULL) OR (NOT was_inserted AND before_encrypted_msisdn IS NOT NULL))
);
CREATE INDEX audience_import_contact_mutations_contact_idx ON audience_import_contact_mutations(contact_id,audience_import_id);

CREATE TABLE audience_import_rollback_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  audience_import_id uuid NOT NULL REFERENCES audience_imports(id) ON DELETE RESTRICT,
  actor_id uuid NOT NULL REFERENCES internal_users(id),
  reason text NOT NULL,
  restored_contacts bigint NOT NULL,
  deleted_contacts bigint NOT NULL,
  revoked_consents bigint NOT NULL,
  occurred_at timestamptz NOT NULL
);
CREATE INDEX audience_import_rollback_events_idx ON audience_import_rollback_events(audience_import_id,occurred_at DESC,id DESC);
CREATE FUNCTION prevent_audience_import_rollback_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'audience import rollback events are append-only'; END $$;
CREATE TRIGGER audience_import_rollback_events_append_only BEFORE UPDATE OR DELETE ON audience_import_rollback_events FOR EACH ROW EXECUTE FUNCTION prevent_audience_import_rollback_event_mutation();

UPDATE role_definitions SET permissions=(SELECT ARRAY(SELECT DISTINCT unnest(permissions || ARRAY['audience.import_mapping.write','audience.import_mapping.approve','audience.import.rollback']))) WHERE code='SUPER_ADMIN';
UPDATE role_definitions SET permissions=(SELECT ARRAY(SELECT DISTINCT unnest(permissions || ARRAY['audience.import_mapping.write']))) WHERE code='CAMPAIGN_OPERATOR';
UPDATE role_definitions SET permissions=(SELECT ARRAY(SELECT DISTINCT unnest(permissions || ARRAY['audience.import_mapping.approve','audience.import.rollback']))) WHERE code='COMPLIANCE_REVIEWER';

COMMIT;
