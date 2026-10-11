BEGIN;

CREATE TABLE audience_cohort_estimates (
  id uuid PRIMARY KEY REFERENCES durable_jobs(id) ON DELETE RESTRICT,
  organisation_id uuid NOT NULL REFERENCES organisations(id) ON DELETE RESTRICT,
  purpose_id uuid NOT NULL REFERENCES consent_purposes(id) ON DELETE RESTRICT,
  channel text NOT NULL CHECK (channel='WHATSAPP'),
  definition jsonb NOT NULL,
  as_of timestamptz NOT NULL,
  requested_by uuid NOT NULL REFERENCES internal_users(id) ON DELETE RESTRICT,
  client_request_id text NOT NULL,
  request_fingerprint text NOT NULL CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
  eligible_count bigint NULL CHECK (eligible_count IS NULL OR eligible_count >= 0),
  breakdown jsonb NULL,
  calculated_at timestamptz NULL,
  consent_review_id uuid NULL REFERENCES consent_reviews(id) ON DELETE RESTRICT,
  consent_review_version bigint NULL CHECK (consent_review_version IS NULL OR consent_review_version > 0),
  consent_wording_version text NULL,
  organisation_policy_id uuid NULL REFERENCES organisation_policy_versions(id) ON DELETE RESTRICT,
  organisation_policy_version bigint NULL CHECK (organisation_policy_version IS NULL OR organisation_policy_version > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  UNIQUE (organisation_id,client_request_id),
  CHECK (
    (calculated_at IS NULL AND eligible_count IS NULL AND breakdown IS NULL
      AND consent_review_id IS NULL AND consent_review_version IS NULL AND consent_wording_version IS NULL
      AND organisation_policy_id IS NULL AND organisation_policy_version IS NULL)
    OR
    (calculated_at IS NOT NULL AND eligible_count IS NOT NULL AND breakdown IS NOT NULL
      AND consent_review_id IS NOT NULL AND consent_review_version IS NOT NULL AND length(trim(consent_wording_version)) > 0
      AND organisation_policy_id IS NOT NULL AND organisation_policy_version IS NOT NULL)
  )
);

CREATE INDEX audience_cohort_estimates_org_created_idx
  ON audience_cohort_estimates(organisation_id,created_at DESC,id DESC);

CREATE INDEX audience_cohort_estimates_pending_result_idx
  ON audience_cohort_estimates(created_at,id)
  WHERE calculated_at IS NULL;

COMMIT;
