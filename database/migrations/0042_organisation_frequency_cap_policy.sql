BEGIN;

ALTER TABLE organisation_policy_versions
  ADD COLUMN frequency_caps jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE organisation_policy_versions
  ADD CONSTRAINT organisation_policy_frequency_caps_array
  CHECK (jsonb_typeof(frequency_caps) = 'array');

CREATE INDEX idx_campaign_recipients_contact_authorised
  ON campaign_recipients(contact_id, authorised_at DESC)
  WHERE status NOT IN ('CANCELLED','SUPPRESSED_BEFORE_SEND');

COMMIT;
