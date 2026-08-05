BEGIN;

CREATE TABLE organisations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  legal_name text NOT NULL,
  trading_name text,
  country_id uuid REFERENCES countries(id),
  status text NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','SUSPENDED','CLOSED')),
  primary_contact_name text,
  primary_contact_email citext,
  internal_notes text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE contacts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  encrypted_msisdn bytea NOT NULL,
  msisdn_lookup_hmac bytea NOT NULL UNIQUE,
  masked_msisdn text NOT NULL,
  country_id uuid REFERENCES countries(id),
  state_id uuid REFERENCES administrative_areas(id),
  lga_id uuid REFERENCES administrative_areas(id),
  reported_age smallint CHECK (reported_age BETWEEN 0 AND 130),
  age_recorded_at date,
  age_source text,
  age_verified boolean NOT NULL DEFAULT false,
  gender_code text REFERENCES gender_options(code),
  preferred_language_code text,
  status text NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','SUPPRESSED','INVALID','DELETED')),
  source_system text,
  source_record_id text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((reported_age IS NULL AND age_recorded_at IS NULL) OR (reported_age IS NOT NULL AND age_recorded_at IS NOT NULL))
);

CREATE INDEX idx_contacts_country_state_lga ON contacts(country_id, state_id, lga_id) WHERE status = 'ACTIVE';
CREATE INDEX idx_contacts_reported_age ON contacts(reported_age, age_recorded_at) WHERE status = 'ACTIVE' AND reported_age IS NOT NULL;
CREATE INDEX idx_contacts_gender ON contacts(gender_code) WHERE status = 'ACTIVE' AND gender_code IS NOT NULL;

CREATE TABLE attribute_definitions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  code text NOT NULL UNIQUE CHECK (code = upper(code)),
  display_name text NOT NULL,
  description text,
  data_type text NOT NULL CHECK (data_type IN ('text','integer','decimal','boolean','date','single_select','multi_select','geography')),
  allowed_operators text[] NOT NULL,
  filterable boolean NOT NULL DEFAULT false,
  reportable boolean NOT NULL DEFAULT false,
  sensitive boolean NOT NULL DEFAULT false,
  required_permission text,
  index_strategy text NOT NULL DEFAULT 'none',
  display_order integer NOT NULL DEFAULT 100,
  active boolean NOT NULL DEFAULT true,
  configuration jsonb NOT NULL DEFAULT '{}'::jsonb,
  version integer NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE contact_attribute_values (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  contact_id uuid NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
  attribute_definition_id uuid NOT NULL REFERENCES attribute_definitions(id),
  value_text text,
  value_integer bigint,
  value_decimal numeric,
  value_boolean boolean,
  value_date date,
  value_json jsonb,
  source_system text,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (num_nonnulls(value_text, value_integer, value_decimal, value_boolean, value_date, value_json) = 1)
);

CREATE INDEX idx_contact_attribute_values_contact ON contact_attribute_values(contact_id, attribute_definition_id);
CREATE INDEX idx_contact_attribute_values_definition_text ON contact_attribute_values(attribute_definition_id, value_text) WHERE value_text IS NOT NULL;
CREATE INDEX idx_contact_attribute_values_definition_integer ON contact_attribute_values(attribute_definition_id, value_integer) WHERE value_integer IS NOT NULL;
CREATE INDEX idx_contact_attribute_values_definition_date ON contact_attribute_values(attribute_definition_id, value_date) WHERE value_date IS NOT NULL;
CREATE INDEX idx_contact_attribute_values_json ON contact_attribute_values USING gin(value_json) WHERE value_json IS NOT NULL;

CREATE TABLE consent_purposes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organisation_id uuid REFERENCES organisations(id),
  code text NOT NULL,
  name text NOT NULL,
  description text,
  channel text NOT NULL,
  wording_version text NOT NULL,
  expires_after_days integer,
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (organisation_id, code, wording_version)
);

CREATE TABLE consent_grants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  contact_id uuid NOT NULL REFERENCES contacts(id),
  organisation_id uuid REFERENCES organisations(id),
  purpose_id uuid NOT NULL REFERENCES consent_purposes(id),
  channel text NOT NULL,
  wording_version text NOT NULL,
  source_type text NOT NULL,
  source_reference text,
  evidence_object_key text,
  evidence_checksum text,
  granted_at timestamptz NOT NULL,
  expires_at timestamptz,
  status text NOT NULL CHECK (status IN ('ACTIVE','EXPIRED','WITHDRAWN','REVOKED','DISPUTED')),
  reviewed_by uuid,
  reviewed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_consent_grants_eligibility ON consent_grants(organisation_id, purpose_id, channel, expires_at, contact_id) WHERE status = 'ACTIVE';
CREATE INDEX idx_consent_grants_contact ON consent_grants(contact_id, status);

CREATE TABLE suppressions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  contact_id uuid NOT NULL REFERENCES contacts(id),
  organisation_id uuid REFERENCES organisations(id),
  purpose_id uuid REFERENCES consent_purposes(id),
  channel text,
  scope text NOT NULL CHECK (scope IN ('GLOBAL','ORGANISATION','PURPOSE','CHANNEL','TEMPORARY')),
  reason text NOT NULL,
  effective_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz,
  active boolean NOT NULL DEFAULT true,
  source_reference text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_suppressions_contact_active ON suppressions(contact_id, organisation_id, purpose_id, channel) WHERE active = true;

COMMIT;
