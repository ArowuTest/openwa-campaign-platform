BEGIN;
CREATE TABLE operations_incidents(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), campaign_id uuid REFERENCES campaigns(id), sender_session_id uuid REFERENCES sender_sessions(id),
 category text NOT NULL, severity text NOT NULL CHECK(severity IN('INFO','WARNING','CRITICAL')), status text NOT NULL CHECK(status IN('OPEN','ACKNOWLEDGED','RESOLVED')),
 summary text NOT NULL, detail text, owner_id uuid REFERENCES internal_users(id), resolution text, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), resolved_at timestamptz, version bigint NOT NULL DEFAULT 1,
 CHECK(status<>'RESOLVED' OR (resolved_at IS NOT NULL AND resolution IS NOT NULL))
);
CREATE INDEX idx_operations_incidents_active ON operations_incidents(status,severity,created_at DESC) WHERE status<>'RESOLVED';
CREATE INDEX idx_operations_incidents_campaign ON operations_incidents(campaign_id,created_at DESC) WHERE campaign_id IS NOT NULL;
CREATE TABLE export_requests(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), kind text NOT NULL CHECK(kind IN('CAMPAIGN_REPORT','AUDIT_LOG')), object_id uuid, format text NOT NULL CHECK(format IN('CSV','JSON','PDF','XLSX')),
 status text NOT NULL CHECK(status IN('DRAFT','PENDING_APPROVAL','APPROVED','REJECTED','READY','EXPIRED')), requested_by uuid NOT NULL REFERENCES internal_users(id), approved_by uuid REFERENCES internal_users(id), reason text NOT NULL, rejection_reason text,
 storage_object_key text, content_hash text, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz, version bigint NOT NULL DEFAULT 1,
 CHECK(approved_by IS NULL OR approved_by<>requested_by), CHECK(status<>'REJECTED' OR rejection_reason IS NOT NULL)
);
CREATE INDEX idx_export_requests_status ON export_requests(status,created_at DESC);
CREATE INDEX idx_export_requests_expiry ON export_requests(expires_at) WHERE status='READY';
UPDATE roles SET permissions=(SELECT ARRAY(SELECT DISTINCT unnest(permissions || ARRAY['operations.read','operations.write','report.read','export.request','export.approve']))) WHERE code='SUPER_ADMIN';
UPDATE roles SET permissions=(SELECT ARRAY(SELECT DISTINCT unnest(permissions || ARRAY['operations.read','operations.write','report.read','export.request']))) WHERE code IN('CAMPAIGN_OPERATOR','TECHNICAL_ADMIN');
UPDATE roles SET permissions=(SELECT ARRAY(SELECT DISTINCT unnest(permissions || ARRAY['operations.read','report.read','export.request']))) WHERE code='ANALYST';
UPDATE roles SET permissions=(SELECT ARRAY(SELECT DISTINCT unnest(permissions || ARRAY['export.approve']))) WHERE code='COMPLIANCE_REVIEWER';
COMMIT;
