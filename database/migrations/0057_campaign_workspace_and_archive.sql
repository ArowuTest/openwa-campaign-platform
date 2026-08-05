BEGIN;
CREATE TABLE campaign_workspaces (
  campaign_id uuid PRIMARY KEY REFERENCES campaigns(id) ON DELETE CASCADE,
  tags jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(tags)='array'),
  archived boolean NOT NULL DEFAULT false,
  archived_by uuid REFERENCES internal_users(id),
  archived_at timestamptz,
  archive_reason text,
  version bigint NOT NULL DEFAULT 1 CHECK(version>0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((archived AND archived_by IS NOT NULL AND archived_at IS NOT NULL AND length(trim(archive_reason))>=3) OR (NOT archived AND archived_by IS NULL AND archived_at IS NULL))
);
CREATE TABLE campaign_internal_notes (
  id uuid PRIMARY KEY,
  campaign_id uuid NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  category text NOT NULL CHECK(category IN ('GENERAL','COMPLIANCE','FINANCE','TECHNICAL')),
  body text NOT NULL CHECK(length(trim(body)) BETWEEN 3 AND 4000),
  created_by uuid NOT NULL REFERENCES internal_users(id),
  created_at timestamptz NOT NULL
);
CREATE INDEX campaign_internal_notes_campaign_created_idx ON campaign_internal_notes(campaign_id,created_at DESC);
CREATE TABLE campaign_workspace_events (
  id bigserial PRIMARY KEY,
  campaign_id uuid NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  event_type text NOT NULL CHECK(event_type IN ('TAGS_UPDATED','ARCHIVED','RESTORED')),
  actor_id uuid NOT NULL REFERENCES internal_users(id),
  reason text NOT NULL CHECK(length(trim(reason))>=3),
  created_at timestamptz NOT NULL
);
CREATE INDEX campaign_workspace_events_campaign_idx ON campaign_workspace_events(campaign_id,created_at DESC);
CREATE FUNCTION prevent_campaign_workspace_evidence_mutation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'campaign workspace evidence is append-only'; END $$;
CREATE TRIGGER campaign_internal_notes_append_only BEFORE UPDATE OR DELETE ON campaign_internal_notes FOR EACH ROW EXECUTE FUNCTION prevent_campaign_workspace_evidence_mutation();
CREATE TRIGGER campaign_workspace_events_append_only BEFORE UPDATE OR DELETE ON campaign_workspace_events FOR EACH ROW EXECUTE FUNCTION prevent_campaign_workspace_evidence_mutation();
COMMIT;
