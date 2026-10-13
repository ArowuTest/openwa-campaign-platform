BEGIN;

-- Upgrade-only migration runners must converge to the same worker authority
-- as the fresh-install role bootstrap. PostgreSQL table-level revocation also
-- removes the corresponding column grants, including any earlier extra grants.
-- The role guard permits owner-only schema replay before service provisioning.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'campaign_export_worker') THEN
    REVOKE ALL PRIVILEGES ON TABLE public.export_requests FROM campaign_export_worker;
    -- Older releases allowed a live redacted authentication projection.
    -- Snapshot rendering needs no source access; remove table/column grants.
    IF pg_catalog.to_regclass('public.authentication_events') IS NOT NULL THEN
      REVOKE ALL PRIVILEGES ON TABLE public.authentication_events FROM campaign_export_worker;
    END IF;
    GRANT SELECT ON TABLE public.export_requests TO campaign_export_worker;
    -- UPDATE on these columns is sufficient for the worker's row-lock reads.
    GRANT UPDATE (status,lease_owner,lease_expires_at,attempt_count,updated_at,version,object_key,content_type,sha256,size_bytes,generated_at,expires_at,failure_code,failure_detail)
      ON TABLE public.export_requests TO campaign_export_worker;
  END IF;
END $$;

-- The worker must update status to claim/finalise jobs, but that column
-- privilege must never let its credentials approve or publish a pending job.
-- This invoker function checks both the stable NOLOGIN role and rotatable,
-- non-superuser LOGIN roles that inherit it. Owners/control API keep their
-- governance authority; a superuser's implicit membership is not worker use.
CREATE OR REPLACE FUNCTION guard_export_worker_status_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  is_export_worker boolean := false;
  actor_is_superuser boolean := false;
BEGIN
  IF current_user = 'campaign_export_worker' THEN
    is_export_worker := true;
  ELSE
    SELECT rolsuper INTO actor_is_superuser FROM pg_catalog.pg_roles WHERE rolname = current_user;
    IF NOT coalesce(actor_is_superuser, false) THEN
      IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'campaign_export_worker') THEN
        is_export_worker := pg_catalog.pg_has_role(current_user, 'campaign_export_worker', 'MEMBER');
      END IF;
    END IF;
  END IF;
  IF NOT is_export_worker THEN
    RETURN NEW;
  END IF;

  -- Approval writes a distinct checker in the control API. Fail closed on
  -- legacy/incomplete approved rows instead of granting publication authority.
  -- Existing READY rows may still move through expiry for safe cleanup.
  IF NEW.status IN ('PROCESSING', 'READY')
    AND (OLD.approved_by IS NULL OR OLD.approved_by = OLD.requested_by) THEN
    RAISE EXCEPTION 'export worker requires independent approval evidence'
      USING ERRCODE = '42501';
  END IF;
  -- Reclaims and failure metadata may update the current lifecycle state.
  IF NEW.status = OLD.status THEN
    RETURN NEW;
  END IF;
  IF (OLD.status = 'APPROVED' AND NEW.status = 'PROCESSING')
    OR (OLD.status = 'PROCESSING' AND NEW.status IN ('READY', 'FAILED'))
    OR (OLD.status = 'READY' AND NEW.status = 'EXPIRING')
    OR (OLD.status = 'EXPIRING' AND NEW.status IN ('READY', 'EXPIRED')) THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'export worker cannot perform governance transition % -> %', OLD.status, NEW.status
    USING ERRCODE = '42501';
END $$;

DROP TRIGGER IF EXISTS export_requests_worker_status_transition_guard ON export_requests;
CREATE TRIGGER export_requests_worker_status_transition_guard
BEFORE UPDATE OF status ON export_requests
FOR EACH ROW EXECUTE FUNCTION guard_export_worker_status_transition();

COMMIT;
