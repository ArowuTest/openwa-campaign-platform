BEGIN;

ALTER TABLE consent_grants
  ADD COLUMN IF NOT EXISTS source_import_id uuid REFERENCES audience_imports(id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_consent_grant_import_basis
  ON consent_grants(contact_id,purpose_id,channel,source_import_id)
  WHERE source_import_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS contact_profile_history (
  id bigserial PRIMARY KEY,
  contact_id uuid NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
  audience_import_id uuid NOT NULL REFERENCES audience_imports(id) ON DELETE CASCADE,
  reported_age smallint CHECK (reported_age BETWEEN 0 AND 130),
  age_recorded_at date,
  gender_code text,
  country_id uuid REFERENCES countries(id),
  state_id uuid REFERENCES administrative_areas(id),
  lga_id uuid REFERENCES administrative_areas(id),
  source_record_hash text NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (contact_id,audience_import_id,source_record_hash),
  CHECK ((reported_age IS NULL AND age_recorded_at IS NULL) OR
         (reported_age IS NOT NULL AND age_recorded_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS idx_contact_profile_history_contact_time
  ON contact_profile_history(contact_id,recorded_at DESC);

COMMIT;
