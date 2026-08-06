BEGIN;

-- High-growth relations remain ordinary tables until a production-like migration
-- rehearsal proves that keys, foreign keys, query plans and retention behaviour
-- remain correct. This catalogue and its procedures make that conversion governed
-- and repeatable rather than allowing ad-hoc partition DDL in production.
CREATE TABLE high_volume_partition_policies (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  parent_table regclass NOT NULL UNIQUE,
  partition_key_column name NOT NULL,
  strategy text NOT NULL CHECK (strategy IN ('RANGE_DAILY','RANGE_MONTHLY','HASH')),
  precreate_windows integer NOT NULL DEFAULT 3 CHECK (precreate_windows BETWEEN 1 AND 36),
  retain_attached_windows integer NOT NULL DEFAULT 12 CHECK (retain_attached_windows BETWEEN 1 AND 1200),
  archive_required boolean NOT NULL DEFAULT true,
  status text NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT','PENDING_APPROVAL','ACTIVE','PAUSED','RETIRED')),
  created_by text NOT NULL,
  approved_by text,
  reason text NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (status <> 'ACTIVE' OR approved_by IS NOT NULL)
);

CREATE TABLE high_volume_partition_windows (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  policy_id uuid NOT NULL REFERENCES high_volume_partition_policies(id),
  partition_name name NOT NULL,
  window_from timestamptz NOT NULL,
  window_to timestamptz NOT NULL,
  status text NOT NULL CHECK (status IN ('PLANNED','CREATED','SEALED','DETACHED','ARCHIVED','DROPPED','FAILED')),
  row_count bigint CHECK (row_count IS NULL OR row_count >= 0),
  archive_reference text,
  evidence_sha256 text CHECK (evidence_sha256 IS NULL OR length(evidence_sha256)=64),
  last_error_reference text,
  created_by text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(policy_id,window_from),
  UNIQUE(policy_id,partition_name),
  CHECK (window_to > window_from),
  CHECK (status NOT IN ('ARCHIVED','DROPPED') OR row_count IS NOT NULL),
  CHECK (status <> 'ARCHIVED' OR (archive_reference IS NOT NULL AND evidence_sha256 IS NOT NULL))
);

CREATE TABLE high_volume_partition_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  policy_id uuid NOT NULL REFERENCES high_volume_partition_policies(id),
  partition_window_id uuid REFERENCES high_volume_partition_windows(id),
  event_type text NOT NULL CHECK (event_type IN ('POLICY_CREATED','POLICY_APPROVED','POLICY_PAUSED','WINDOW_CREATED','WINDOW_SEALED','WINDOW_DETACHED','WINDOW_ARCHIVED','WINDOW_DROPPED','OPERATION_REJECTED')),
  actor_id text NOT NULL,
  reason text NOT NULL,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(evidence)='object'),
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX high_volume_partition_events_policy_time_idx
  ON high_volume_partition_events(policy_id,occurred_at DESC,id DESC);
CREATE INDEX high_volume_partition_windows_status_time_idx
  ON high_volume_partition_windows(status,window_from,policy_id);

CREATE FUNCTION prevent_high_volume_partition_event_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'high-volume partition events are append-only';
END;
$$;
CREATE TRIGGER high_volume_partition_events_immutable
BEFORE UPDATE OR DELETE ON high_volume_partition_events
FOR EACH ROW EXECUTE FUNCTION prevent_high_volume_partition_event_mutation();

CREATE FUNCTION governed_partition_name(p_parent regclass, p_from timestamptz)
RETURNS name LANGUAGE plpgsql STABLE AS $$
DECLARE
  parent_name text;
BEGIN
  SELECT c.relname INTO parent_name FROM pg_class c WHERE c.oid=p_parent;
  IF parent_name IS NULL THEN
    RAISE EXCEPTION 'partition parent does not exist';
  END IF;
  RETURN left(parent_name || '_p' || to_char(p_from AT TIME ZONE 'UTC','YYYYMMDDHH24MI'), 63)::name;
END;
$$;

CREATE FUNCTION create_governed_range_partition(
  p_policy_id uuid,
  p_window_from timestamptz,
  p_window_to timestamptz,
  p_actor_id text,
  p_reason text
) RETURNS name
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path=public,pg_temp
AS $$
DECLARE
  policy high_volume_partition_policies%ROWTYPE;
  parent_schema name;
  parent_name name;
  child_name name;
  child_qualified text;
  parent_qualified text;
  window_id uuid;
BEGIN
  IF p_window_to <= p_window_from THEN
    RAISE EXCEPTION 'partition window end must be after start';
  END IF;
  IF length(trim(p_actor_id)) < 3 OR length(trim(p_reason)) < 8 THEN
    RAISE EXCEPTION 'partition operation requires actor and reason evidence';
  END IF;

  SELECT * INTO policy FROM high_volume_partition_policies WHERE id=p_policy_id FOR UPDATE;
  IF NOT FOUND OR policy.status <> 'ACTIVE' OR policy.strategy NOT IN ('RANGE_DAILY','RANGE_MONTHLY') THEN
    RAISE EXCEPTION 'active range partition policy is required';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_partitioned_table WHERE partrelid=policy.parent_table) THEN
    -- The operation fails in the caller transaction. Do not insert a rejection
    -- event here because the subsequent exception would roll it back and create
    -- a false impression that the rejection is durably evidenced. Callers must
    -- record the failed operation in their own durable governance transaction.
    RAISE EXCEPTION 'parent relation is not declaratively partitioned';
  END IF;

  SELECT n.nspname,c.relname INTO parent_schema,parent_name
  FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
  WHERE c.oid=policy.parent_table;
  child_name := governed_partition_name(policy.parent_table,p_window_from);
  parent_qualified := format('%I.%I',parent_schema,parent_name);
  child_qualified := format('%I.%I',parent_schema,child_name);

  PERFORM pg_advisory_xact_lock(hashtextextended(policy.parent_table::text,0));
  EXECUTE format('CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM (%L) TO (%L)',
    child_qualified,parent_qualified,p_window_from,p_window_to);

  INSERT INTO high_volume_partition_windows(policy_id,partition_name,window_from,window_to,status,created_by)
  VALUES(policy.id,child_name,p_window_from,p_window_to,'CREATED',p_actor_id)
  ON CONFLICT(policy_id,window_from) DO UPDATE
    SET partition_name=EXCLUDED.partition_name,status='CREATED',updated_at=now(),last_error_reference=NULL
  RETURNING id INTO window_id;

  INSERT INTO high_volume_partition_events(policy_id,partition_window_id,event_type,actor_id,reason,evidence)
  VALUES(policy.id,window_id,'WINDOW_CREATED',p_actor_id,p_reason,
    jsonb_build_object('parent',parent_qualified,'partition',child_qualified,'from',p_window_from,'to',p_window_to));
  RETURN child_name;
END;
$$;

CREATE FUNCTION detach_governed_partition(
  p_policy_id uuid,
  p_partition_name name,
  p_actor_id text,
  p_reason text
) RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path=public,pg_temp
AS $$
DECLARE
  policy high_volume_partition_policies%ROWTYPE;
  window_record high_volume_partition_windows%ROWTYPE;
  parent_schema name;
  parent_name name;
  child_oid regclass;
BEGIN
  IF length(trim(p_actor_id)) < 3 OR length(trim(p_reason)) < 8 THEN
    RAISE EXCEPTION 'partition operation requires actor and reason evidence';
  END IF;
  SELECT * INTO policy FROM high_volume_partition_policies WHERE id=p_policy_id FOR UPDATE;
  IF NOT FOUND OR policy.status NOT IN ('ACTIVE','PAUSED') THEN
    RAISE EXCEPTION 'active or paused partition policy is required';
  END IF;
  SELECT * INTO window_record FROM high_volume_partition_windows
    WHERE policy_id=p_policy_id AND partition_name=p_partition_name FOR UPDATE;
  IF NOT FOUND OR window_record.status NOT IN ('CREATED','SEALED') THEN
    RAISE EXCEPTION 'created or sealed partition window is required';
  END IF;

  SELECT n.nspname,c.relname INTO parent_schema,parent_name
  FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.oid=policy.parent_table;
  child_oid := to_regclass(format('%I.%I',parent_schema,p_partition_name));
  IF child_oid IS NULL OR NOT EXISTS (
    SELECT 1 FROM pg_inherits WHERE inhparent=policy.parent_table AND inhrelid=child_oid
  ) THEN
    RAISE EXCEPTION 'named relation is not an attached partition of the governed parent';
  END IF;

  PERFORM pg_advisory_xact_lock(hashtextextended(policy.parent_table::text,0));
  EXECUTE format('ALTER TABLE %I.%I DETACH PARTITION %I.%I',parent_schema,parent_name,parent_schema,p_partition_name);
  UPDATE high_volume_partition_windows SET status='DETACHED',updated_at=now() WHERE id=window_record.id;
  INSERT INTO high_volume_partition_events(policy_id,partition_window_id,event_type,actor_id,reason,evidence)
  VALUES(policy.id,window_record.id,'WINDOW_DETACHED',p_actor_id,p_reason,
    jsonb_build_object('parent',policy.parent_table::text,'partition',p_partition_name::text));
END;
$$;

CREATE FUNCTION record_governed_partition_archive(
  p_policy_id uuid,
  p_partition_name name,
  p_row_count bigint,
  p_archive_reference text,
  p_evidence_sha256 text,
  p_actor_id text,
  p_reason text
) RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path=public,pg_temp
AS $$
DECLARE window_id uuid;
BEGIN
  IF length(trim(p_actor_id)) < 3 OR length(trim(p_reason)) < 8 THEN
    RAISE EXCEPTION 'partition archive operation requires actor and reason evidence';
  END IF;
  IF p_row_count < 0 OR length(trim(p_archive_reference)) < 3 OR p_evidence_sha256 !~ '^[0-9A-Fa-f]{64}$' THEN
    RAISE EXCEPTION 'complete archive evidence is required';
  END IF;
  UPDATE high_volume_partition_windows
     SET status='ARCHIVED',row_count=p_row_count,archive_reference=p_archive_reference,
         evidence_sha256=lower(p_evidence_sha256),updated_at=now()
   WHERE policy_id=p_policy_id AND partition_name=p_partition_name AND status='DETACHED'
   RETURNING id INTO window_id;
  IF window_id IS NULL THEN
    RAISE EXCEPTION 'detached governed partition window is required';
  END IF;
  INSERT INTO high_volume_partition_events(policy_id,partition_window_id,event_type,actor_id,reason,evidence)
  VALUES(p_policy_id,window_id,'WINDOW_ARCHIVED',p_actor_id,p_reason,
    jsonb_build_object('rowCount',p_row_count,'archiveReference',p_archive_reference,'sha256',lower(p_evidence_sha256)));
END;
$$;

-- Ordered time-key indexes support bounded retention/archive scans before native
-- partition conversion and become the required query shape after conversion.
CREATE INDEX IF NOT EXISTS campaign_recipients_partition_time_idx
  ON campaign_recipients(authorised_at,id);
CREATE INDEX IF NOT EXISTS delivery_events_partition_time_idx
  ON delivery_events(received_at,id);
CREATE INDEX IF NOT EXISTS audit_events_partition_time_idx
  ON audit_events(created_at,sequence,id);
CREATE INDEX IF NOT EXISTS audience_import_staging_partition_time_idx
  ON audience_import_staging(staged_at,audience_import_id,row_number);
CREATE INDEX IF NOT EXISTS audience_import_issues_partition_time_idx
  ON audience_import_issues(created_at,id);

INSERT INTO high_volume_partition_policies(parent_table,partition_key_column,strategy,created_by,reason,status)
SELECT relation,key_column,'RANGE_MONTHLY','migration-0066','Initial partition-readiness registration','DRAFT'
FROM (VALUES
  (to_regclass('public.campaign_recipients'),'authorised_at'::name),
  (to_regclass('public.delivery_events'),'received_at'::name),
  (to_regclass('public.audit_events'),'created_at'::name),
  (to_regclass('public.audience_import_staging'),'staged_at'::name),
  (to_regclass('public.audience_import_issues'),'created_at'::name)
) AS candidate(relation,key_column)
WHERE relation IS NOT NULL
ON CONFLICT(parent_table) DO NOTHING;

CREATE VIEW high_volume_partition_readiness AS
SELECT p.id,p.parent_table::text AS parent_table,p.partition_key_column,p.strategy,p.status,
       EXISTS(SELECT 1 FROM pg_partitioned_table pt WHERE pt.partrelid=p.parent_table) AS native_partitioned,
       coalesce(s.n_live_tup,0)::bigint AS estimated_live_rows,
       pg_total_relation_size(p.parent_table) AS total_bytes,
       (SELECT count(*) FROM pg_inherits i WHERE i.inhparent=p.parent_table) AS attached_partitions
FROM high_volume_partition_policies p
LEFT JOIN pg_stat_user_tables s ON s.relid=p.parent_table;

REVOKE ALL ON FUNCTION create_governed_range_partition(uuid,timestamptz,timestamptz,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION detach_governed_partition(uuid,name,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION record_governed_partition_archive(uuid,name,bigint,text,text,text,text) FROM PUBLIC;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='campaign_platform_governance_worker') THEN
    GRANT SELECT,INSERT,UPDATE ON high_volume_partition_policies,high_volume_partition_windows TO campaign_platform_governance_worker;
    GRANT SELECT,INSERT ON high_volume_partition_events TO campaign_platform_governance_worker;
    GRANT SELECT ON high_volume_partition_readiness TO campaign_platform_governance_worker;
    GRANT EXECUTE ON FUNCTION create_governed_range_partition(uuid,timestamptz,timestamptz,text,text) TO campaign_platform_governance_worker;
    GRANT EXECUTE ON FUNCTION detach_governed_partition(uuid,name,text,text) TO campaign_platform_governance_worker;
    GRANT EXECUTE ON FUNCTION record_governed_partition_archive(uuid,name,bigint,text,text,text,text) TO campaign_platform_governance_worker;
  END IF;
END $$;

COMMIT;
