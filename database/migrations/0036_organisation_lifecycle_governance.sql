BEGIN;
ALTER TABLE organisations ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;
ALTER TABLE organisations DROP CONSTRAINT IF EXISTS organisations_status_check;
ALTER TABLE organisations ADD CONSTRAINT organisations_status_check CHECK (status IN ('ACTIVE','SUSPENDED','UNDER_REVIEW','CLOSED'));
CREATE UNIQUE INDEX IF NOT EXISTS uq_organisations_active_legal_country ON organisations(lower(regexp_replace(legal_name,'\s+',' ','g')),country_id) WHERE status <> 'CLOSED';
CREATE TABLE IF NOT EXISTS organisation_events(
 id uuid PRIMARY KEY,
 organisation_id uuid NOT NULL REFERENCES organisations(id),
 version bigint NOT NULL,
 event_type text NOT NULL CHECK(event_type IN('UPDATED','STATUS_CHANGED')),
 actor_id uuid,
 reason text,
 before_status text,
 after_status text,
 occurred_at timestamptz NOT NULL,
 UNIQUE(organisation_id,version)
);
CREATE INDEX IF NOT EXISTS idx_organisation_events_org ON organisation_events(organisation_id,version);
COMMIT;
