BEGIN;

CREATE TABLE internal_users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email citext NOT NULL UNIQUE,
  display_name text NOT NULL,
  status text NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('INVITED','ACTIVE','SUSPENDED','DISABLED')),
  mfa_required boolean NOT NULL DEFAULT true,
  last_login_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE roles (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  code text NOT NULL UNIQUE,
  name text NOT NULL,
  description text,
  system_role boolean NOT NULL DEFAULT false,
  permissions text[] NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_roles (
  user_id uuid NOT NULL REFERENCES internal_users(id) ON DELETE CASCADE,
  role_id uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  assigned_by uuid REFERENCES internal_users(id),
  assigned_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, role_id)
);

INSERT INTO roles(code, name, description, system_role, permissions) VALUES
  ('SUPER_ADMIN', 'Super administrator', 'Full platform administration.', true, ARRAY['*']),
  ('CAMPAIGN_OPERATOR', 'Campaign operator', 'Creates audiences and campaigns but cannot self-approve.', true, ARRAY['organisation.read','consent.read','audience.read','audience.write','campaign.read','campaign.write']),
  ('COMPLIANCE_REVIEWER', 'Compliance reviewer', 'Reviews consent evidence and grants or rejects consent-use approvals.', true, ARRAY['organisation.read','consent.read','consent.review','audit.read']),
  ('CAMPAIGN_APPROVER', 'Campaign approver', 'Approves messages, audiences and final campaign release.', true, ARRAY['campaign.read','campaign.approve','audience.read','consent.read']),
  ('ANALYST', 'Analyst', 'Views aggregated reports without unrestricted raw identifier access.', true, ARRAY['report.read','campaign.read']),
  ('TECHNICAL_ADMIN', 'Technical administrator', 'Operates sender nodes, sessions and queues.', true, ARRAY['sender.read','sender.write','queue.read','queue.write'])
ON CONFLICT (code) DO NOTHING;

CREATE TABLE consent_reviews (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organisation_id uuid NOT NULL REFERENCES organisations(id),
  name text NOT NULL,
  purpose_description text,
  channel text NOT NULL CHECK (channel IN ('WHATSAPP')),
  consent_source text NOT NULL,
  wording_version text NOT NULL,
  privacy_notice_reviewed boolean NOT NULL DEFAULT false,
  opt_out_process_reviewed boolean NOT NULL DEFAULT false,
  sample_records_reviewed boolean NOT NULL DEFAULT false,
  permitted_country_iso2 char(2)[] NOT NULL DEFAULT '{}',
  permitted_message_category text,
  restrictions text,
  status text NOT NULL CHECK (status IN ('DRAFT','PENDING_REVIEW','APPROVED','REJECTED','EXPIRED','SUSPENDED')),
  submitted_by uuid REFERENCES internal_users(id),
  reviewed_by uuid REFERENCES internal_users(id),
  reviewed_at timestamptz,
  expires_at timestamptz,
  decision_reason text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (status <> 'APPROVED' OR (
    reviewed_by IS NOT NULL
    AND reviewed_at IS NOT NULL
    AND expires_at IS NOT NULL
    AND privacy_notice_reviewed
    AND opt_out_process_reviewed
    AND sample_records_reviewed
  ))
);

CREATE INDEX idx_consent_reviews_org_status ON consent_reviews(organisation_id, status, expires_at);
CREATE INDEX idx_consent_reviews_expiry ON consent_reviews(expires_at) WHERE status = 'APPROVED';

CREATE TABLE consent_review_evidence (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  consent_review_id uuid NOT NULL REFERENCES consent_reviews(id) ON DELETE CASCADE,
  object_key text NOT NULL,
  original_filename text,
  media_type text,
  byte_size bigint CHECK (byte_size IS NULL OR byte_size >= 0),
  sha256_checksum text NOT NULL,
  malware_scan_status text NOT NULL DEFAULT 'PENDING' CHECK (malware_scan_status IN ('PENDING','CLEAN','INFECTED','FAILED')),
  uploaded_by uuid REFERENCES internal_users(id),
  uploaded_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (consent_review_id, sha256_checksum)
);

ALTER TABLE consent_purposes
  ADD COLUMN IF NOT EXISTS consent_review_id uuid REFERENCES consent_reviews(id),
  ADD COLUMN IF NOT EXISTS permitted_message_category text;

CREATE TABLE audience_imports (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organisation_id uuid NOT NULL REFERENCES organisations(id),
  consent_review_id uuid NOT NULL REFERENCES consent_reviews(id),
  source_name text NOT NULL,
  source_system text,
  default_country_iso2 char(2),
  object_key text NOT NULL,
  original_filename text NOT NULL,
  file_sha256 text NOT NULL,
  byte_size bigint NOT NULL CHECK (byte_size >= 0),
  mapping jsonb NOT NULL,
  status text NOT NULL CHECK (status IN ('UPLOADED','SCANNING','VALIDATING','PREVIEW_READY','APPROVED','IMPORTING','COMPLETED','COMPLETED_WITH_EXCEPTIONS','REJECTED','FAILED','CANCELLED')),
  uploaded_rows bigint NOT NULL DEFAULT 0,
  valid_rows bigint NOT NULL DEFAULT 0,
  invalid_rows bigint NOT NULL DEFAULT 0,
  duplicate_rows bigint NOT NULL DEFAULT 0,
  suppressed_rows bigint NOT NULL DEFAULT 0,
  inserted_contacts bigint NOT NULL DEFAULT 0,
  updated_contacts bigint NOT NULL DEFAULT 0,
  uploaded_by uuid REFERENCES internal_users(id),
  approved_by uuid REFERENCES internal_users(id),
  approved_at timestamptz,
  started_at timestamptz,
  completed_at timestamptz,
  failure_reason text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (organisation_id, file_sha256)
);

CREATE INDEX idx_audience_imports_org_status ON audience_imports(organisation_id, status, created_at DESC);
CREATE INDEX idx_audience_imports_consent_review ON audience_imports(consent_review_id, created_at DESC);

CREATE TABLE audience_import_issues (
  id bigserial PRIMARY KEY,
  audience_import_id uuid NOT NULL REFERENCES audience_imports(id) ON DELETE CASCADE,
  row_number bigint NOT NULL,
  field_name text,
  issue_code text NOT NULL,
  issue_message text NOT NULL,
  masked_value text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_audience_import_issues_import_row ON audience_import_issues(audience_import_id, row_number);

CREATE TABLE contact_sources (
  contact_id uuid NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
  organisation_id uuid NOT NULL REFERENCES organisations(id),
  audience_import_id uuid REFERENCES audience_imports(id),
  source_system text,
  source_record_id text,
  source_record_hash text NOT NULL,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (contact_id, organisation_id, source_record_hash)
);

CREATE INDEX idx_contact_sources_import ON contact_sources(audience_import_id);
CREATE INDEX idx_contact_sources_organisation ON contact_sources(organisation_id, contact_id);

ALTER TABLE audience_snapshots
  ADD COLUMN IF NOT EXISTS final_suppression_checked_at timestamptz,
  ADD COLUMN IF NOT EXISTS final_consent_checked_at timestamptz,
  ADD COLUMN IF NOT EXISTS eligibility_query_hash text;

ALTER TABLE delivery_events
  ADD COLUMN IF NOT EXISTS event_deduplication_key text;

CREATE UNIQUE INDEX IF NOT EXISTS uq_delivery_events_deduplication
  ON delivery_events(event_deduplication_key)
  WHERE event_deduplication_key IS NOT NULL;

CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$;

DO $$
DECLARE
  table_name text;
BEGIN
  FOREACH table_name IN ARRAY ARRAY[
    'countries','administrative_areas','organisations','contacts','attribute_definitions',
    'consent_purposes','consent_grants','segments','campaigns','sender_nodes','sender_sessions',
    'campaign_recipients','internal_users','roles','consent_reviews','audience_imports'
  ]
  LOOP
    EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', 'trg_' || table_name || '_updated_at', table_name);
    EXECUTE format(
      'CREATE TRIGGER %I BEFORE UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION set_updated_at()',
      'trg_' || table_name || '_updated_at', table_name
    );
  END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION prevent_audit_event_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit_events are append-only';
END;
$$;

DROP TRIGGER IF EXISTS trg_audit_events_no_update ON audit_events;
CREATE TRIGGER trg_audit_events_no_update
BEFORE UPDATE OR DELETE ON audit_events
FOR EACH ROW EXECUTE FUNCTION prevent_audit_event_mutation();

COMMIT;
