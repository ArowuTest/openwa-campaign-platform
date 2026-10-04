BEGIN;

-- Network admission denials are security-relevant authentication events and
-- must remain auditable when the public boundary rejects a request.
ALTER TABLE authentication_events
  DROP CONSTRAINT IF EXISTS authentication_events_event_type_check;

ALTER TABLE authentication_events
  ADD CONSTRAINT authentication_events_event_type_check
  CHECK (event_type IN (
    'LOGIN_SUCCEEDED',
    'LOGIN_FAILED',
    'MFA_SUCCEEDED',
    'MFA_FAILED',
    'ACCOUNT_LOCKED',
    'SESSION_REVOKED',
    'ALL_SESSIONS_REVOKED',
    'PERMISSION_DENIED',
    'STEP_UP_REQUIRED',
    'NETWORK_ACCESS_DENIED'
  ));

COMMIT;
