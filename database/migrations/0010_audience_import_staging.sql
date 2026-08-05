BEGIN;

ALTER TABLE audience_imports
  ADD COLUMN IF NOT EXISTS purpose_id uuid REFERENCES consent_purposes(id),
  ADD COLUMN IF NOT EXISTS channel text,
  ADD COLUMN IF NOT EXISTS wording_version text,
  ADD COLUMN IF NOT EXISTS granted_at timestamptz,
  ADD COLUMN IF NOT EXISTS expires_at timestamptz,
  ADD COLUMN IF NOT EXISTS last_processed_row bigint NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS audience_import_staging (
  audience_import_id uuid NOT NULL REFERENCES audience_imports(id) ON DELETE CASCADE,
  row_number bigint NOT NULL CHECK (row_number >= 2),
  encrypted_msisdn bytea NOT NULL,
  msisdn_lookup_hmac bytea NOT NULL,
  masked_msisdn text NOT NULL,
  country_iso2 char(2) NOT NULL,
  state_name text,
  lga_name text,
  reported_age smallint CHECK (reported_age BETWEEN 0 AND 130),
  age_recorded_at date,
  gender_code text,
  source_record_hash text NOT NULL,
  staged_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (audience_import_id, row_number),
  UNIQUE (audience_import_id, msisdn_lookup_hmac),
  CHECK ((reported_age IS NULL AND age_recorded_at IS NULL) OR
         (reported_age IS NOT NULL AND age_recorded_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS idx_audience_import_staging_lookup
  ON audience_import_staging(audience_import_id, msisdn_lookup_hmac);

CREATE UNIQUE INDEX IF NOT EXISTS uq_audience_import_issue_row_code
  ON audience_import_issues(audience_import_id, row_number, issue_code);

COMMIT;
