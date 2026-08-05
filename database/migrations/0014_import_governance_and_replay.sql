BEGIN;

-- Import identity and interpretation are immutable evidence. Operational counters,
-- status and approval fields remain mutable through guarded application workflows.
ALTER TABLE audience_imports
  DROP CONSTRAINT IF EXISTS audience_imports_status_check;

ALTER TABLE audience_imports
  ADD CONSTRAINT audience_imports_status_check CHECK (status IN (
    'UPLOADED','SCANNING','QUARANTINED','VALIDATING','PREVIEW_READY','APPROVED',
    'IMPORTING','COMPLETED','COMPLETED_WITH_EXCEPTIONS','REJECTED','FAILED','CANCELLED'
  ));

ALTER TABLE audience_imports
  ADD COLUMN IF NOT EXISTS detected_media_type text,
  ADD COLUMN IF NOT EXISTS template_version text,
  ADD COLUMN IF NOT EXISTS update_policy text,
  ADD COLUMN IF NOT EXISTS malware_scan_status text,
  ADD COLUMN IF NOT EXISTS content_signature_valid boolean,
  ADD COLUMN IF NOT EXISTS client_request_id text,
  ADD COLUMN IF NOT EXISTS validation_lease_owner text,
  ADD COLUMN IF NOT EXISTS validation_lease_expires_at timestamptz,
  ADD COLUMN IF NOT EXISTS validation_lease_version bigint NOT NULL DEFAULT 0;

UPDATE audience_imports
SET detected_media_type = coalesce(nullif(detected_media_type,''),'application/octet-stream'),
    template_version = coalesce(nullif(template_version,''),'legacy-v0'),
    update_policy = coalesce(nullif(update_policy,''),'NEWEST_SOURCE'),
    malware_scan_status = coalesce(nullif(malware_scan_status,''),'PENDING'),
    content_signature_valid = coalesce(content_signature_valid,false),
    client_request_id = coalesce(nullif(client_request_id,''),'legacy:' || id::text)
WHERE detected_media_type IS NULL OR detected_media_type='' OR
      template_version IS NULL OR template_version='' OR
      update_policy IS NULL OR update_policy='' OR
      malware_scan_status IS NULL OR malware_scan_status='' OR
      content_signature_valid IS NULL OR client_request_id IS NULL OR client_request_id='';

ALTER TABLE audience_imports
  ALTER COLUMN detected_media_type SET NOT NULL,
  ALTER COLUMN template_version SET NOT NULL,
  ALTER COLUMN update_policy SET NOT NULL,
  ALTER COLUMN malware_scan_status SET NOT NULL,
  ALTER COLUMN content_signature_valid SET NOT NULL,
  ALTER COLUMN client_request_id SET NOT NULL;

ALTER TABLE audience_imports
  DROP CONSTRAINT IF EXISTS audience_imports_detected_media_type_check,
  DROP CONSTRAINT IF EXISTS audience_imports_template_version_check,
  DROP CONSTRAINT IF EXISTS audience_imports_update_policy_check,
  DROP CONSTRAINT IF EXISTS audience_imports_malware_scan_status_check,
  DROP CONSTRAINT IF EXISTS audience_imports_client_request_id_check,
  DROP CONSTRAINT IF EXISTS audience_imports_validation_lease_check;

ALTER TABLE audience_imports
  ADD CONSTRAINT audience_imports_detected_media_type_check
    CHECK (length(detected_media_type) BETWEEN 3 AND 255),
  ADD CONSTRAINT audience_imports_template_version_check
    CHECK (length(template_version) BETWEEN 1 AND 100),
  ADD CONSTRAINT audience_imports_update_policy_check
    CHECK (update_policy IN ('FILL_NULL','NEWEST_SOURCE','TRUSTED_SOURCE','MANUAL_CONFLICT')),
  ADD CONSTRAINT audience_imports_malware_scan_status_check
    CHECK (malware_scan_status IN ('PENDING','CLEAN','INFECTED','FAILED')),
  ADD CONSTRAINT audience_imports_client_request_id_check
    CHECK (length(client_request_id) BETWEEN 16 AND 128 AND client_request_id ~ '^[A-Za-z0-9_.:-]+$'),
  ADD CONSTRAINT audience_imports_validation_lease_check CHECK (
    (validation_lease_owner IS NULL AND validation_lease_expires_at IS NULL) OR
    (validation_lease_owner IS NOT NULL AND validation_lease_expires_at IS NOT NULL)
  );

CREATE UNIQUE INDEX IF NOT EXISTS uq_audience_imports_org_client_request
  ON audience_imports(organisation_id,client_request_id);

-- The existing UNIQUE(organisation_id,file_sha256) enforces exact file replay.
-- These partial indexes serve operational queues without adding write cost to
-- completed imports.
CREATE INDEX IF NOT EXISTS idx_audience_imports_scan_queue
  ON audience_imports(created_at,id)
  WHERE status IN ('UPLOADED','SCANNING') AND malware_scan_status='PENDING';

CREATE INDEX IF NOT EXISTS idx_audience_imports_validation_queue
  ON audience_imports(updated_at,id)
  WHERE status='VALIDATING' AND malware_scan_status='CLEAN' AND content_signature_valid=true;

CREATE INDEX IF NOT EXISTS idx_audience_imports_validation_lease
  ON audience_imports(validation_lease_expires_at,id)
  WHERE status='VALIDATING' AND validation_lease_owner IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_audience_imports_preview_approval
  ON audience_imports(organisation_id,updated_at DESC,id)
  WHERE status='PREVIEW_READY' AND malware_scan_status='CLEAN' AND content_signature_valid=true;

CREATE OR REPLACE FUNCTION prevent_audience_import_identity_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.organisation_id IS DISTINCT FROM OLD.organisation_id OR
     NEW.consent_review_id IS DISTINCT FROM OLD.consent_review_id OR
     NEW.purpose_id IS DISTINCT FROM OLD.purpose_id OR
     NEW.channel IS DISTINCT FROM OLD.channel OR
     NEW.wording_version IS DISTINCT FROM OLD.wording_version OR
     NEW.source_name IS DISTINCT FROM OLD.source_name OR
     NEW.source_system IS DISTINCT FROM OLD.source_system OR
     NEW.default_country_iso2 IS DISTINCT FROM OLD.default_country_iso2 OR
     NEW.object_key IS DISTINCT FROM OLD.object_key OR
     NEW.original_filename IS DISTINCT FROM OLD.original_filename OR
     NEW.detected_media_type IS DISTINCT FROM OLD.detected_media_type OR
     NEW.file_sha256 IS DISTINCT FROM OLD.file_sha256 OR
     NEW.byte_size IS DISTINCT FROM OLD.byte_size OR
     NEW.template_version IS DISTINCT FROM OLD.template_version OR
     NEW.mapping IS DISTINCT FROM OLD.mapping OR
     NEW.update_policy IS DISTINCT FROM OLD.update_policy OR
     NEW.client_request_id IS DISTINCT FROM OLD.client_request_id OR
     NEW.uploaded_by IS DISTINCT FROM OLD.uploaded_by OR
     NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION 'audience import identity and interpretation are immutable'
      USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_audience_import_identity_immutable ON audience_imports;
CREATE TRIGGER trg_audience_import_identity_immutable
BEFORE UPDATE ON audience_imports
FOR EACH ROW EXECUTE FUNCTION prevent_audience_import_identity_mutation();

COMMIT;
