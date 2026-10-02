BEGIN;

CREATE TABLE campaign_opt_out_metric_events (
    suppression_id uuid PRIMARY KEY REFERENCES suppressions(id) ON DELETE RESTRICT,
    campaign_id uuid NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    recorded_at timestamptz NOT NULL
);

CREATE INDEX idx_campaign_opt_out_metric_events_campaign
ON campaign_opt_out_metric_events(campaign_id, recorded_at);

COMMIT;
