BEGIN;
CREATE TABLE IF NOT EXISTS campaign_commercial_approvals (
 id uuid PRIMARY KEY,
 campaign_id uuid NOT NULL REFERENCES campaigns(id),
 organisation_id uuid NOT NULL REFERENCES organisations(id),
 quotation_reference text NOT NULL,
 invoice_reference text NOT NULL,
 currency varchar(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
 approved_recipients bigint NOT NULL CHECK (approved_recipients > 0),
 unit_price_minor bigint NOT NULL CHECK (unit_price_minor >= 0),
 management_fee_minor bigint NOT NULL CHECK (management_fee_minor >= 0),
 total_amount_minor bigint NOT NULL CHECK (total_amount_minor = approved_recipients * unit_price_minor + management_fee_minor),
 payment_reference text,
 payment_received_at timestamptz,
 status text NOT NULL CHECK (status IN ('DRAFT','PENDING_APPROVAL','APPROVED','REJECTED','REVOKED')),
 version bigint NOT NULL CHECK (version > 0),
 created_by uuid,
 submitted_by uuid,
 approved_by uuid,
 reason text NOT NULL,
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 UNIQUE(campaign_id)
);
CREATE INDEX IF NOT EXISTS campaign_commercial_org_status_idx ON campaign_commercial_approvals(organisation_id,status,created_at DESC);
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS commercial_approval_id uuid REFERENCES campaign_commercial_approvals(id);
INSERT INTO roles(code,name,description,system_role,permissions) VALUES ('FINANCE_USER','Finance user','Records quotations, invoices, payment evidence and commercial approvals.',true,ARRAY['finance.read','finance.write']) ON CONFLICT (code) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,system_role=true,permissions=EXCLUDED.permissions;
INSERT INTO roles(code,name,description,system_role,permissions) VALUES ('FINANCE_APPROVER','Finance approver','Independently approves or revokes campaign commercial evidence.',true,ARRAY['finance.read','finance.approve']) ON CONFLICT (code) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,system_role=true,permissions=EXCLUDED.permissions;
COMMIT;
