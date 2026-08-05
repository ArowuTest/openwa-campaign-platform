BEGIN;
CREATE TABLE inbound_replies (
 id uuid PRIMARY KEY,
 event_id text NOT NULL UNIQUE,
 recipient_id uuid NOT NULL REFERENCES campaign_recipients(id),
 contact_id uuid NOT NULL REFERENCES contacts(id),
 campaign_id uuid NOT NULL REFERENCES campaigns(id),
 session_id text NOT NULL,
 provider_message_id text,
 message_text text NOT NULL CHECK (length(message_text) BETWEEN 1 AND 4096),
 message_fingerprint text NOT NULL,
 classification text NOT NULL DEFAULT 'UNREVIEWED' CHECK (classification IN ('UNREVIEWED','OPT_OUT','QUESTION','COMPLAINT','OTHER')),
 escalated boolean NOT NULL DEFAULT false,
 escalation_reason text,
 occurred_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 reviewed_at timestamptz,
 reviewed_by uuid REFERENCES internal_users(id),
 version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
 CHECK (NOT escalated OR escalation_reason IS NOT NULL)
);
CREATE INDEX inbound_replies_campaign_created_idx ON inbound_replies (campaign_id, created_at DESC);
CREATE INDEX inbound_replies_review_queue_idx ON inbound_replies (classification, escalated, created_at) WHERE reviewed_at IS NULL OR escalated;
COMMIT;
