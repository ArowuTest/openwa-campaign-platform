BEGIN;

-- Support deterministic latest-effective-grant resolution used by both release
-- and final dispatch. The ordering columns mirror the fail-closed SQL checks.
CREATE INDEX IF NOT EXISTS consent_grants_latest_effective_idx
  ON consent_grants(contact_id, organisation_id, purpose_id, upper(channel), effective_from DESC, created_at DESC, id DESC);

-- Scope-aware suppression checks are performed for every final release and send.
CREATE INDEX IF NOT EXISTS suppressions_final_eligibility_idx
  ON suppressions(contact_id, active, effective_at, expires_at, scope, organisation_id, purpose_id, upper(channel));

-- Campaign release and dispatch revalidate live organisation and consent-review
-- evidence rather than trusting the earlier approval timestamp.
CREATE INDEX IF NOT EXISTS campaigns_release_guard_idx
  ON campaigns(id, organisation_id, consent_review_id, status);

COMMIT;
