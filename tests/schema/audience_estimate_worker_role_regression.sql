-- Operational privilege regression. Run only against an isolated, fully
-- migrated test database as its bootstrap owner:
-- psql -v role_test_database=<isolated-db> -f audience_estimate_worker_role_regression.sql
-- Bootstrap changes service grants in that database; all fixture rows roll back.
\set ON_ERROR_STOP on
\if :{?role_test_database}
\else
\echo 'role_test_database must identify the isolated migrated test database'
\quit 3
\endif
SELECT current_database() = :'role_test_database' AS correct_test_database \gset
\if :correct_test_database
\else
\echo 'Connected database does not match role_test_database'
\quit 3
\endif

-- Both initial application and upgrade reconciliation use the real policy.
\ir ../../database/bootstrap/002_service_roles.sql
GRANT INSERT, DELETE ON audience_cohort_estimates, durable_jobs TO campaign_audience_worker;
GRANT UPDATE (definition) ON audience_cohort_estimates TO campaign_audience_worker;
GRANT UPDATE (payload) ON durable_jobs TO campaign_audience_worker;
GRANT SELECT ON privacy_cases TO campaign_audience_worker;
\ir ../../database/bootstrap/002_service_roles.sql

-- The same conditional grant helper must tolerate relations absent before
-- their migration, including a column privilege on a not-yet-created table.
DO $$
BEGIN
  IF to_regclass('public.audience_worker_regression_absent_table') IS NOT NULL THEN
    RAISE EXCEPTION 'absent-table fixture unexpectedly exists';
  END IF;
END $$;
SELECT pg_temp.grant_existing_tables('campaign_audience_worker',
  'SELECT, UPDATE(not_yet_created_column)',
  ARRAY['audience_worker_regression_absent_table']);

BEGIN;
DO $$
DECLARE
  actor_id uuid := gen_random_uuid();
  org_id uuid := gen_random_uuid();
  purpose_id uuid := gen_random_uuid();
  review_id uuid := gen_random_uuid();
  policy_id uuid := gen_random_uuid();
  job_id uuid := gen_random_uuid();
  fixture_time timestamptz := '2099-04-01 12:00:00+00';
BEGIN
  INSERT INTO internal_users(id,email,display_name,status,mfa_required)
    VALUES(actor_id,'role-estimate-' || actor_id || '@internal.invalid','Estimate Role Fixture','DISABLED',false);
  INSERT INTO organisations(id,legal_name,status)
    VALUES(org_id,'Estimate Role Fixture ' || org_id,'ACTIVE');
  INSERT INTO consent_reviews(
    id,organisation_id,name,channel,consent_source,wording_version,
    privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,
    status,reviewed_by,reviewed_at,expires_at,outcome
  ) VALUES(
    review_id,org_id,'Estimate Role Review','WHATSAPP','DIRECT','v1',
    true,true,true,'APPROVED',actor_id,fixture_time - interval '1 day',
    fixture_time + interval '1 day','APPROVED'
  );
  INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id)
    VALUES(purpose_id,org_id,'ROLE_' || purpose_id,'Estimate Role Purpose','WHATSAPP','v1',review_id);
  INSERT INTO organisation_policy_versions(
    id,organisation_id,allowed_purpose_ids,prohibited_purpose_ids,frequency_caps,
    contact_retention_days,campaign_retention_days,status,effective_from,version,
    created_by,submitted_by,approved_by,reason,created_at,updated_at
  ) VALUES(
    policy_id,org_id,'[]'::jsonb,'[]'::jsonb,'[]'::jsonb,365,365,'ACTIVE',
    fixture_time - interval '2 days',1,actor_id,actor_id,actor_id,
    'Estimate role regression',fixture_time,fixture_time
  );
  INSERT INTO durable_jobs(
    id,job_type,deduplication_key,payload,status,priority,attempt_count,
    max_attempts,available_at,created_at,updated_at
  ) VALUES(
    job_id,'COHORT_ESTIMATE','estimate-role:' || job_id,'{}'::jsonb,
    'PENDING',0,0,5,fixture_time,fixture_time,fixture_time
  );
  INSERT INTO audience_cohort_estimates(
    id,organisation_id,purpose_id,channel,definition,as_of,requested_by,
    client_request_id,request_fingerprint,created_at,updated_at
  ) VALUES(
    job_id,org_id,purpose_id,'WHATSAPP',
    '{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"IN","values":["NG"]}]}'::jsonb,
    fixture_time,actor_id,'estimate-role-' || job_id,repeat('a',64),
    fixture_time,fixture_time
  );
  PERFORM set_config('test.estimate_role_job',job_id::text,true);
  PERFORM set_config('test.estimate_role_review',review_id::text,true);
  PERFORM set_config('test.estimate_role_policy',policy_id::text,true);
END $$;

SET LOCAL ROLE campaign_audience_worker;
DO $$
DECLARE
  job_id uuid := current_setting('test.estimate_role_job')::uuid;
  review_id uuid := current_setting('test.estimate_role_review')::uuid;
  policy_id uuid := current_setting('test.estimate_role_policy')::uuid;
  fixture_time timestamptz := '2099-04-01 12:00:00+00';
  leased_job record;
  estimate_record record;
  stored_id uuid;
  affected integer;
  failures text[] := ARRAY[]::text[];
BEGIN
  IF current_user <> 'campaign_audience_worker' OR
     EXISTS (SELECT 1 FROM pg_roles WHERE rolname=current_user AND (rolsuper OR rolbypassrls OR rolcanlogin)) THEN
    RAISE EXCEPTION 'regression is not executing as the least-privilege service role';
  END IF;

  -- Exact FilterDefinitionStore.List startup read. Its source is
  -- attribute_definitions, not a table named filter_definitions.
  PERFORM code,display_name,coalesce(description,''),data_type,
    to_jsonb(allowed_operators)::text,core,filterable,reportable,sensitive,
    coalesce(required_permission,''),storage,coalesce(queryable_field,''),
    coalesce(attribute_value_column,''),index_strategy,allowed_values::text,
    display_order,active,version,created_at,updated_at
  FROM attribute_definitions ORDER BY display_order,code;

  BEGIN
    -- The production one-type Claim statement, with only an extra fixture-id
    -- predicate to avoid touching other test runs' pending queue rows.
    WITH candidates AS (
      SELECT id FROM durable_jobs
      WHERE id=job_id AND job_type='COHORT_ESTIMATE' AND available_at <= fixture_time
        AND (status='PENDING' OR (status='PROCESSING' AND lease_expires_at <= fixture_time))
        AND (
          job_type <> 'DISPATCH_CAMPAIGN_RECIPIENT'
          OR EXISTS (
            SELECT 1 FROM campaign_recipients cr
            JOIN campaigns c ON c.id=cr.campaign_id
            WHERE cr.id::text=durable_jobs.payload->>'campaignRecipientId'
              AND c.status='DISPATCHING'
              AND cr.status IN ('AUTHORISED','QUEUED','FAILED_RETRYABLE')
          )
        )
      ORDER BY priority DESC,available_at,created_at
      FOR UPDATE SKIP LOCKED LIMIT 1
    )
    UPDATE durable_jobs j
    SET status='PROCESSING',lease_owner='estimate-role-worker',
        lease_expires_at=fixture_time + interval '5 minutes',
        attempt_count=j.attempt_count+1,lease_version=j.lease_version+1,
        updated_at=fixture_time
    FROM candidates c WHERE j.id=c.id
    RETURNING j.* INTO leased_job;
    IF NOT FOUND OR leased_job.id <> job_id THEN
      RAISE EXCEPTION 'Claim did not lease the fixture';
    END IF;
  EXCEPTION WHEN insufficient_privilege THEN
    failures := array_append(failures,'Claim: ' || SQLERRM);
  END;

  BEGIN
    -- Worker Get loads estimate evidence and its corresponding durable job.
    SELECT e.* INTO estimate_record FROM audience_cohort_estimates e WHERE e.id=job_id;
    IF NOT FOUND THEN RAISE EXCEPTION 'Get did not read estimate evidence'; END IF;
    SELECT j.* INTO leased_job FROM durable_jobs j WHERE j.id=job_id;
    IF NOT FOUND THEN RAISE EXCEPTION 'Get did not read its durable job'; END IF;
  EXCEPTION WHEN insufficient_privilege THEN
    failures := array_append(failures,'Get: ' || SQLERRM);
  END;

  BEGIN
    -- Same fields and live-lease fence as PostgreSQLEstimateRepository.StoreResult.
    UPDATE audience_cohort_estimates e SET
      eligible_count=7,
      breakdown='{"matchedProfiles":9,"consentEligible":8,"consentExcluded":1,"unsuppressed":7,"suppressionExcluded":1,"eligible":7,"frequencyCapExcluded":0}'::jsonb,
      calculated_at=fixture_time,consent_review_id=review_id,
      consent_review_version=1,consent_wording_version='v1',
      organisation_policy_id=policy_id,organisation_policy_version=1,
      updated_at=fixture_time
    FROM durable_jobs j WHERE e.id=job_id AND j.id=e.id AND j.status='PROCESSING'
      AND j.lease_owner='estimate-role-worker' AND j.lease_version=1
      AND j.lease_expires_at>fixture_time
    RETURNING e.id INTO stored_id;
    IF stored_id IS DISTINCT FROM job_id THEN
      RAISE EXCEPTION 'StoreResult did not persist its fenced result';
    END IF;
  EXCEPTION WHEN insufficient_privilege THEN
    failures := array_append(failures,'StoreResult: ' || SQLERRM);
  END;
  IF cardinality(failures)>0 THEN
    RAISE EXCEPTION 'Estimate worker privilege failures: %',array_to_string(failures,'; ');
  END IF;

  -- Exercise the remaining real queue lifecycle update columns.
  UPDATE durable_jobs SET lease_expires_at=fixture_time + interval '6 minutes',updated_at=fixture_time
    WHERE id=job_id AND status='PROCESSING' AND lease_owner='estimate-role-worker'
      AND lease_version=1 AND lease_expires_at>fixture_time;
  GET DIAGNOSTICS affected=ROW_COUNT;
  IF affected<>1 THEN RAISE EXCEPTION 'Renew did not extend the live lease'; END IF;
  UPDATE durable_jobs SET status='PENDING',available_at=fixture_time,
    last_error_code='ROLE_RETRY',last_error_detail='role regression retry',
    updated_at=fixture_time,lease_owner=NULL,lease_expires_at=NULL
    WHERE id=job_id AND status='PROCESSING' AND lease_owner='estimate-role-worker'
      AND lease_version=1 AND lease_expires_at>fixture_time;
  GET DIAGNOSTICS affected=ROW_COUNT;
  IF affected<>1 THEN RAISE EXCEPTION 'Fail did not persist its retry'; END IF;
  UPDATE durable_jobs SET status='PROCESSING',lease_owner='estimate-role-worker',
    lease_expires_at=fixture_time + interval '5 minutes',attempt_count=attempt_count+1,
    lease_version=lease_version+1,updated_at=fixture_time
    WHERE id=job_id AND status='PENDING' AND available_at<=fixture_time;
  GET DIAGNOSTICS affected=ROW_COUNT;
  IF affected<>1 THEN RAISE EXCEPTION 'Reclaim did not obtain a fresh lease'; END IF;
  UPDATE durable_jobs SET status='COMPLETED',completed_at=fixture_time,
    updated_at=fixture_time,lease_owner=NULL,lease_expires_at=NULL
    WHERE id=job_id AND status='PROCESSING' AND lease_owner='estimate-role-worker'
      AND lease_version=2 AND lease_expires_at>fixture_time;
  GET DIAGNOSTICS affected=ROW_COUNT;
  IF affected<>1 THEN RAISE EXCEPTION 'Complete did not finish the fresh lease'; END IF;
  SELECT j.* INTO leased_job FROM durable_jobs j WHERE j.id=job_id;
  SELECT e.* INTO estimate_record FROM audience_cohort_estimates e WHERE e.id=job_id;
  IF leased_job.status<>'COMPLETED' OR leased_job.attempt_count<>2 OR
     estimate_record.eligible_count<>7 OR estimate_record.consent_review_version<>1 THEN
    RAISE EXCEPTION 'Worker lifecycle did not retain its expected result and state';
  END IF;

  -- Negative runtime checks ensure reconciliation removed both broad grants
  -- and independent column grants seeded between the two policy applications.
  BEGIN
    UPDATE audience_cohort_estimates SET definition='{}'::jsonb WHERE id=job_id;
    RAISE EXCEPTION 'Worker can mutate the immutable estimate definition';
  EXCEPTION WHEN insufficient_privilege THEN NULL;
  END;
  BEGIN
    UPDATE durable_jobs SET payload='{}'::jsonb WHERE id=job_id;
    RAISE EXCEPTION 'Worker can mutate the durable job payload';
  EXCEPTION WHEN insufficient_privilege THEN NULL;
  END;
  BEGIN
    DELETE FROM audience_cohort_estimates WHERE id=job_id;
    RAISE EXCEPTION 'Worker can delete estimate evidence';
  EXCEPTION WHEN insufficient_privilege THEN NULL;
  END;
  BEGIN
    DELETE FROM durable_jobs WHERE id=job_id;
    RAISE EXCEPTION 'Worker can delete durable jobs';
  EXCEPTION WHEN insufficient_privilege THEN NULL;
  END;
  BEGIN
    PERFORM count(*) FROM privacy_cases;
    RAISE EXCEPTION 'Worker can read unrelated privacy cases';
  EXCEPTION WHEN insufficient_privilege THEN NULL;
  END;
  IF has_any_column_privilege(current_user,'audience_cohort_estimates','INSERT') OR
     has_any_column_privilege(current_user,'durable_jobs','INSERT') THEN
    RAISE EXCEPTION 'Worker has unnecessary request-scheduling INSERT privilege';
  END IF;
END $$;
ROLLBACK;
\echo 'PASS: estimate worker startup read, Claim/Get/StoreResult/lease lifecycle, and least-privilege reconciliation'
