BEGIN;

ALTER TABLE sender_sessions
  ADD COLUMN IF NOT EXISTS owner_reference text,
  ADD COLUMN IF NOT EXISTS registration_country_iso2 text,
  ADD COLUMN IF NOT EXISTS profile_display_name text,
  ADD COLUMN IF NOT EXISTS recovery_reference text;

ALTER TABLE sender_sessions DROP CONSTRAINT IF EXISTS sender_sessions_owner_reference_check;
ALTER TABLE sender_sessions ADD CONSTRAINT sender_sessions_owner_reference_check
  CHECK (owner_reference IS NULL OR length(trim(owner_reference)) BETWEEN 2 AND 200);

ALTER TABLE sender_sessions DROP CONSTRAINT IF EXISTS sender_sessions_registration_country_check;
ALTER TABLE sender_sessions ADD CONSTRAINT sender_sessions_registration_country_check
  CHECK (registration_country_iso2 IS NULL OR registration_country_iso2 ~ '^[A-Z]{2}$');
ALTER TABLE sender_sessions DROP CONSTRAINT IF EXISTS sender_sessions_profile_display_name_check;
ALTER TABLE sender_sessions ADD CONSTRAINT sender_sessions_profile_display_name_check
  CHECK (profile_display_name IS NULL OR length(trim(profile_display_name)) BETWEEN 1 AND 160);

ALTER TABLE sender_sessions DROP CONSTRAINT IF EXISTS sender_sessions_recovery_reference_check;
ALTER TABLE sender_sessions ADD CONSTRAINT sender_sessions_recovery_reference_check
  CHECK (recovery_reference IS NULL OR length(trim(recovery_reference)) BETWEEN 3 AND 500);

COMMENT ON COLUMN sender_sessions.recovery_reference IS
  'Non-secret recovery-information reference only. Never expose through ordinary sender APIs or logs.';

-- Sender health thresholds are configuration records and may require a
-- per-session override. Maintenance-window scope remains deliberately narrower.
ALTER TABLE platform_configurations DROP CONSTRAINT IF EXISTS platform_configurations_scope_type_check;
ALTER TABLE platform_configurations ADD CONSTRAINT platform_configurations_scope_type_check
  CHECK (scope_type IN ('PLATFORM','ENVIRONMENT','ORGANISATION','CAMPAIGN','PROVIDER','GATEWAY_POOL','SENDER_POOL','SENDER_SESSION'));

-- A non-login principal gives automatic health-protection transitions an
-- attributable foreign-key identity without impersonating a human operator.
-- No credentials or role membership are provisioned for this record.
INSERT INTO internal_users(id,email,display_name,status,mfa_required)
VALUES('00000000-0000-4000-8000-000000000003','service.sender-health@internal.invalid','Sender Health Automation','DISABLED',false)
ON CONFLICT(id) DO UPDATE SET display_name=excluded.display_name,status='DISABLED',mfa_required=false,updated_at=now();

COMMIT;
