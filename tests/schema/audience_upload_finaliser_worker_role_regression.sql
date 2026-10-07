-- Run against a dedicated migrated test database as its bootstrap owner, then
-- execute TestPostgreSQLUploadFinaliserAudienceWorker* with the dedicated DSN.
-- Existing broad grants must converge to the minimal finaliser authority.
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

\ir ../../database/bootstrap/002_service_roles.sql
GRANT ALL ON audience_import_upload_sessions,audience_import_upload_parts TO campaign_audience_worker;
\ir ../../database/bootstrap/002_service_roles.sql
\ir ../../database/bootstrap/002_service_roles.sql

DO $$
DECLARE
  relation_name text;
  column_name text;
  actual_update boolean;
  expected_update boolean;
BEGIN
  FOREACH relation_name IN ARRAY ARRAY['audience_import_upload_sessions','audience_import_upload_parts'] LOOP
    IF NOT has_table_privilege('campaign_audience_worker',relation_name,'SELECT')
      OR has_table_privilege('campaign_audience_worker',relation_name,'UPDATE')
      OR has_table_privilege('campaign_audience_worker',relation_name,'INSERT')
      OR has_table_privilege('campaign_audience_worker',relation_name,'DELETE')
      OR has_table_privilege('campaign_audience_worker',relation_name,'TRUNCATE') THEN
      RAISE EXCEPTION 'upload finaliser table grants are not least privilege: %', relation_name;
    END IF;
    FOR column_name IN
      SELECT attname FROM pg_attribute WHERE attrelid=to_regclass(relation_name)
        AND attnum>0 AND NOT attisdropped
    LOOP
      actual_update := has_column_privilege('campaign_audience_worker',relation_name,column_name,'UPDATE');
      expected_update := CASE WHEN relation_name='audience_import_upload_sessions'
        THEN column_name=ANY(ARRAY['state','finaliser_lease_owner','finaliser_lease_version',
          'finaliser_lease_expires_at','version','updated_at','final_sha256','detected_media_type',
          'linked_import_id','failure_reason'])
        ELSE column_name='state' END;
      IF actual_update IS DISTINCT FROM expected_update THEN
        RAISE EXCEPTION 'unexpected upload finaliser column grant: %.% update=% expected=%',
          relation_name,column_name,actual_update,expected_update;
      END IF;
    END LOOP;
  END LOOP;
END $$;
