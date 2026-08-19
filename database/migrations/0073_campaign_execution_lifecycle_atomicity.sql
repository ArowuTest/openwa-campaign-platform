BEGIN;

ALTER TABLE campaign_execution_events
    ADD COLUMN campaign_version bigint NULL,
    ADD CONSTRAINT chk_campaign_execution_event_version
        CHECK (campaign_version IS NULL OR campaign_version > 0);

CREATE UNIQUE INDEX uq_campaign_execution_event_lifecycle_version
    ON campaign_execution_events(campaign_id, campaign_version)
    WHERE campaign_version IS NOT NULL;

COMMIT;
