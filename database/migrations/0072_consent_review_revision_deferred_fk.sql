BEGIN;

-- Consent-review revision is one atomic transaction: the approved review is
-- linked to its replacement and the replacement row is created before commit.
-- Enforce the self-reference at commit so the transaction may establish both
-- sides without weakening referential integrity.
ALTER TABLE consent_reviews
  ALTER CONSTRAINT consent_reviews_superseded_by_id_fkey
  DEFERRABLE INITIALLY DEFERRED;

COMMIT;
