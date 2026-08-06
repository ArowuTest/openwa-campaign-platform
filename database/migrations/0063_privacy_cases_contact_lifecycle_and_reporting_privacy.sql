BEGIN;

ALTER TABLE contacts DROP CONSTRAINT IF EXISTS contacts_status_check;
ALTER TABLE contacts ADD CONSTRAINT contacts_status_check CHECK (
  status IN ('ACTIVE','INACTIVE','SUPPRESSED','INVALID','RECYCLED','DECEASED','ANONYMISED','DELETED')
);
ALTER TABLE contacts
  ADD COLUMN IF NOT EXISTS status_reason text,
  ADD COLUMN IF NOT EXISTS status_updated_at timestamptz,
  ADD COLUMN IF NOT EXISTS status_updated_by uuid REFERENCES internal_users(id),
  ADD COLUMN IF NOT EXISTS processing_restricted boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS anonymised_at timestamptz,
  ADD COLUMN IF NOT EXISTS deleted_at timestamptz,
  ADD COLUMN IF NOT EXISTS lifecycle_version bigint NOT NULL DEFAULT 1;
CREATE INDEX IF NOT EXISTS contacts_status_lifecycle_idx ON contacts(status,updated_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS contacts_processing_restricted_idx ON contacts(processing_restricted,id) WHERE processing_restricted=true;

CREATE TABLE contact_lifecycle_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  contact_id uuid NOT NULL REFERENCES contacts(id) ON DELETE RESTRICT,
  previous_status text NOT NULL,
  new_status text NOT NULL,
  previous_processing_restricted boolean NOT NULL,
  new_processing_restricted boolean NOT NULL,
  actor_id uuid NOT NULL REFERENCES internal_users(id),
  reason text NOT NULL,
  contact_version bigint NOT NULL CHECK (contact_version > 1),
  occurred_at timestamptz NOT NULL
);
CREATE INDEX contact_lifecycle_events_contact_idx ON contact_lifecycle_events(contact_id,occurred_at DESC,id DESC);
CREATE FUNCTION prevent_contact_lifecycle_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'contact lifecycle events are append-only'; END;
$$;
CREATE TRIGGER contact_lifecycle_events_append_only BEFORE UPDATE OR DELETE ON contact_lifecycle_events FOR EACH ROW EXECUTE FUNCTION prevent_contact_lifecycle_event_mutation();

ALTER TABLE contact_profile_history
  ALTER COLUMN audience_import_id DROP NOT NULL,
  ADD COLUMN IF NOT EXISTS privacy_case_id uuid,
  ADD COLUMN IF NOT EXISTS actor_id uuid REFERENCES internal_users(id),
  ADD COLUMN IF NOT EXISTS change_reason text,
  ADD COLUMN IF NOT EXISTS source_system text,
  ADD COLUMN IF NOT EXISTS source_record_id text,
  ADD COLUMN IF NOT EXISTS age_source text,
  ADD COLUMN IF NOT EXISTS age_verified boolean,
  ADD COLUMN IF NOT EXISTS preferred_language_code text,
  ADD COLUMN IF NOT EXISTS contact_status text;

CREATE TABLE privacy_cases (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  case_type text NOT NULL CHECK (case_type IN ('ACCESS','RECTIFICATION','ERASURE','PORTABILITY','OBJECTION','RESTRICTION')),
  status text NOT NULL CHECK (status IN ('OPEN','ASSIGNED','PENDING_APPROVAL','APPROVED','REJECTED','IN_PROGRESS','COMPLETED','CANCELLED')),
  subject_lookup_hmac bytea NOT NULL,
  subject_masked text NOT NULL,
  contact_id uuid REFERENCES contacts(id),
  organisation_id uuid REFERENCES organisations(id),
  requested_at timestamptz NOT NULL,
  due_at timestamptz NOT NULL,
  assigned_to uuid REFERENCES internal_users(id),
  created_by uuid NOT NULL REFERENCES internal_users(id),
  submitted_by uuid REFERENCES internal_users(id),
  decided_by uuid REFERENCES internal_users(id),
  executed_by uuid REFERENCES internal_users(id),
  request_reason text NOT NULL,
  decision_reason text,
  execution_reason text,
  requested_changes jsonb NOT NULL DEFAULT '{}'::jsonb,
  result_ciphertext bytea,
  result_key_version text,
  result_sha256 text,
  completed_at timestamptz,
  rejected_at timestamptz,
  cancelled_at timestamptz,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (due_at > requested_at),
  CHECK (decided_by IS NULL OR decided_by <> created_by),
  CHECK (status <> 'COMPLETED' OR (completed_at IS NOT NULL AND executed_by IS NOT NULL)),
  CHECK (status <> 'REJECTED' OR (rejected_at IS NOT NULL AND decision_reason IS NOT NULL)),
  CHECK (result_sha256 IS NULL OR result_sha256 ~ '^[0-9a-f]{64}$')
);
CREATE INDEX privacy_cases_subject_time_idx ON privacy_cases(subject_lookup_hmac,created_at DESC,id DESC);
CREATE INDEX privacy_cases_status_due_idx ON privacy_cases(status,due_at,id) WHERE status NOT IN ('COMPLETED','REJECTED','CANCELLED');
CREATE INDEX privacy_cases_assignee_idx ON privacy_cases(assigned_to,status,due_at) WHERE assigned_to IS NOT NULL;

ALTER TABLE contact_profile_history
  ADD CONSTRAINT contact_profile_history_privacy_case_fk FOREIGN KEY (privacy_case_id) REFERENCES privacy_cases(id);

CREATE TABLE privacy_case_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  privacy_case_id uuid NOT NULL REFERENCES privacy_cases(id) ON DELETE CASCADE,
  event_type text NOT NULL CHECK (event_type IN ('CREATED','ASSIGNED','SUBMITTED','APPROVED','REJECTED','EXECUTION_STARTED','COMPLETED','CANCELLED','EXPORT_REQUESTED')),
  actor_id uuid NOT NULL REFERENCES internal_users(id),
  reason text,
  case_version bigint NOT NULL,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX privacy_case_events_case_time_idx ON privacy_case_events(privacy_case_id,occurred_at DESC,id DESC);
CREATE FUNCTION prevent_privacy_case_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'privacy case events are append-only'; END;
$$;
CREATE TRIGGER privacy_case_events_append_only BEFORE UPDATE OR DELETE ON privacy_case_events FOR EACH ROW EXECUTE FUNCTION prevent_privacy_case_event_mutation();

CREATE TABLE privacy_legal_holds (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  subject_lookup_hmac bytea NOT NULL,
  contact_id uuid REFERENCES contacts(id),
  organisation_id uuid REFERENCES organisations(id),
  scope text NOT NULL CHECK (scope IN ('ALL','CONTACT','CONSENT','CAMPAIGN','AUDIT','COMMERCIAL')),
  status text NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT','PENDING_APPROVAL','ACTIVE','REJECTED','RELEASED')),
  reason text NOT NULL,
  created_by uuid NOT NULL REFERENCES internal_users(id),
  submitted_by uuid REFERENCES internal_users(id),
  decided_by uuid REFERENCES internal_users(id),
  decision_reason text,
  created_at timestamptz NOT NULL DEFAULT now(),
  activated_at timestamptz,
  rejected_at timestamptz,
  expires_at timestamptz,
  released_at timestamptz,
  released_by uuid REFERENCES internal_users(id),
  release_reason text,
  version bigint NOT NULL DEFAULT 1,
  CHECK (expires_at IS NULL OR expires_at > created_at),
  CHECK (decided_by IS NULL OR decided_by <> created_by),
  CHECK (status <> 'ACTIVE' OR (activated_at IS NOT NULL AND decided_by IS NOT NULL AND length(coalesce(decision_reason,'')) >= 8)),
  CHECK (status <> 'REJECTED' OR (rejected_at IS NOT NULL AND decided_by IS NOT NULL AND length(coalesce(decision_reason,'')) >= 8)),
  CHECK (status <> 'RELEASED' OR (released_at IS NOT NULL AND released_by IS NOT NULL AND length(coalesce(release_reason,'')) >= 8))
);
CREATE INDEX privacy_legal_holds_active_subject_idx ON privacy_legal_holds(subject_lookup_hmac,scope,created_at DESC) WHERE status='ACTIVE';

CREATE TABLE privacy_legal_hold_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  legal_hold_id uuid NOT NULL REFERENCES privacy_legal_holds(id) ON DELETE CASCADE,
  event_type text NOT NULL CHECK (event_type IN ('CREATED','SUBMITTED','APPROVED','REJECTED','RELEASED')),
  actor_id uuid NOT NULL REFERENCES internal_users(id),
  reason text,
  hold_version bigint NOT NULL,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX privacy_legal_hold_events_hold_time_idx ON privacy_legal_hold_events(legal_hold_id,occurred_at DESC,id DESC);
CREATE FUNCTION prevent_privacy_legal_hold_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'privacy legal hold events are append-only'; END;
$$;
CREATE TRIGGER privacy_legal_hold_events_append_only BEFORE UPDATE OR DELETE ON privacy_legal_hold_events FOR EACH ROW EXECUTE FUNCTION prevent_privacy_legal_hold_event_mutation();

CREATE TABLE reporting_privacy_policies (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organisation_id uuid REFERENCES organisations(id),
  status text NOT NULL CHECK (status IN ('DRAFT','PENDING_APPROVAL','ACTIVE','REJECTED','RETIRED')),
  minimum_cohort_size integer NOT NULL CHECK (minimum_cohort_size BETWEEN 2 AND 10000),
  suppression_label text NOT NULL DEFAULT 'SUPPRESSED_SMALL_COHORT',
  apply_geography boolean NOT NULL DEFAULT true,
  apply_demographics boolean NOT NULL DEFAULT true,
  apply_attributes boolean NOT NULL DEFAULT true,
  effective_from timestamptz NOT NULL,
  effective_to timestamptz,
  created_by uuid NOT NULL REFERENCES internal_users(id),
  submitted_by uuid REFERENCES internal_users(id),
  approved_by uuid REFERENCES internal_users(id),
  reason text NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (effective_to IS NULL OR effective_to > effective_from),
  CHECK (approved_by IS NULL OR approved_by <> created_by)
);
CREATE UNIQUE INDEX reporting_privacy_active_platform_idx ON reporting_privacy_policies((organisation_id IS NULL)) WHERE organisation_id IS NULL AND status='ACTIVE' AND effective_to IS NULL;
CREATE UNIQUE INDEX reporting_privacy_active_org_idx ON reporting_privacy_policies(organisation_id) WHERE organisation_id IS NOT NULL AND status='ACTIVE' AND effective_to IS NULL;
CREATE INDEX reporting_privacy_resolution_idx ON reporting_privacy_policies(organisation_id,status,effective_from DESC,id DESC);
ALTER TABLE reporting_privacy_policies ADD CONSTRAINT reporting_privacy_active_period_exclusion EXCLUDE USING gist (coalesce(organisation_id,'00000000-0000-0000-0000-000000000000'::uuid) WITH =, tstzrange(effective_from,coalesce(effective_to,'infinity'::timestamptz),'[)') WITH &&) WHERE (status='ACTIVE');

CREATE TABLE reporting_privacy_policy_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  policy_id uuid NOT NULL REFERENCES reporting_privacy_policies(id) ON DELETE CASCADE,
  event_type text NOT NULL CHECK (event_type IN ('CREATED','SUBMITTED','APPROVED','REJECTED','ACTIVATED','SUPERSEDED','RETIRED')),
  actor_id uuid NOT NULL REFERENCES internal_users(id),
  reason text,
  policy_version bigint NOT NULL,
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION prevent_reporting_privacy_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'reporting privacy policy events are append-only'; END;
$$;
CREATE TRIGGER reporting_privacy_policy_events_append_only BEFORE UPDATE OR DELETE ON reporting_privacy_policy_events FOR EACH ROW EXECUTE FUNCTION prevent_reporting_privacy_event_mutation();

UPDATE role_definitions SET permissions=(SELECT ARRAY(SELECT DISTINCT unnest(permissions || ARRAY['privacy.read','privacy.write','privacy.approve','privacy.execute','privacy.hold','reporting_privacy.write','reporting_privacy.approve']))) WHERE code='SUPER_ADMIN';
UPDATE role_definitions SET permissions=(SELECT ARRAY(SELECT DISTINCT unnest(permissions || ARRAY['privacy.read','privacy.write','privacy.approve','privacy.hold','reporting_privacy.write','reporting_privacy.approve']))) WHERE code='COMPLIANCE_REVIEWER';
UPDATE role_definitions SET permissions=(SELECT ARRAY(SELECT DISTINCT unnest(permissions || ARRAY['privacy.read','privacy.execute']))) WHERE code='TECHNICAL_ADMIN';
UPDATE role_definitions SET permissions=(SELECT ARRAY(SELECT DISTINCT unnest(permissions || ARRAY['audience.contact_status.write']))) WHERE code IN ('SUPER_ADMIN','COMPLIANCE_REVIEWER');

COMMIT;
