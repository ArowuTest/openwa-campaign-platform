BEGIN;
CREATE TABLE IF NOT EXISTS organisation_policy_versions(
 id uuid PRIMARY KEY,
 organisation_id uuid NOT NULL REFERENCES organisations(id),
 allowed_purpose_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(allowed_purpose_ids)='array'),
 prohibited_purpose_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(prohibited_purpose_ids)='array'),
 contact_retention_days integer NOT NULL CHECK(contact_retention_days BETWEEN 1 AND 3650),
 campaign_retention_days integer NOT NULL CHECK(campaign_retention_days BETWEEN 30 AND 3650),
 report_brand_name text,
 report_footer text,
 status text NOT NULL CHECK(status IN('DRAFT','PENDING_APPROVAL','ACTIVE','REJECTED','RETIRED')),
 effective_from timestamptz NOT NULL,
 effective_to timestamptz,
 version bigint NOT NULL,
 created_by uuid,
 submitted_by uuid,
 approved_by uuid,
 reason text NOT NULL,
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 CHECK(effective_to IS NULL OR effective_to>effective_from)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_org_policy_active ON organisation_policy_versions(organisation_id) WHERE status='ACTIVE';
CREATE INDEX IF NOT EXISTS idx_org_policy_lookup ON organisation_policy_versions(organisation_id,status,effective_from DESC);
COMMIT;
