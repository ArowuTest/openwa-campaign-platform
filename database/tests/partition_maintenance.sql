BEGIN;
CREATE SCHEMA partition_validation;
CREATE TABLE partition_validation.events (
  id bigint NOT NULL,
  occurred_at timestamptz NOT NULL,
  payload jsonb NOT NULL,
  PRIMARY KEY(occurred_at,id)
) PARTITION BY RANGE(occurred_at);
INSERT INTO high_volume_partition_policies(parent_table,partition_key_column,strategy,status,created_by,approved_by,reason)
VALUES('partition_validation.events'::regclass,'occurred_at','RANGE_MONTHLY','ACTIVE','validation','validation','Cross-partition validation policy')
RETURNING id \gset
SELECT create_governed_range_partition(:'id','2026-01-01T00:00:00Z','2026-02-01T00:00:00Z','validation','Create January validation partition');
SELECT create_governed_range_partition(:'id','2026-02-01T00:00:00Z','2026-03-01T00:00:00Z','validation','Create February validation partition');
INSERT INTO partition_validation.events VALUES
  (1,'2026-01-15T00:00:00Z','{}'),
  (2,'2026-02-15T00:00:00Z','{}');
DO $$ BEGIN
  IF (SELECT count(*) FROM partition_validation.events) <> 2 THEN
    RAISE EXCEPTION 'cross-partition query failed';
  END IF;
END $$;
SELECT detach_governed_partition(:'id','events_p202601010000','validation','Detach January validation partition');
DO $$ BEGIN
  IF (SELECT count(*) FROM partition_validation.events) <> 1 THEN
    RAISE EXCEPTION 'detached partition remained visible through parent';
  END IF;
END $$;
ROLLBACK;
