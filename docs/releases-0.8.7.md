# Release 0.8.7 — Governed Campaign Commercial Controls

This remediation release implements the internal managed-service commercial workflow required before a campaign may progress to commercial approval.

## Delivered

- Campaign-specific quotation, invoice, pricing, payment and recipient-entitlement records.
- Draft, pending approval, approved, rejected and revoked lifecycle.
- Maker-checker separation and MFA-protected approval/revocation APIs.
- Exact arithmetic validation in minor currency units.
- Campaign transition gate that requires active commercial evidence and sufficient approved recipient volume.
- PostgreSQL migration `0039_campaign_commercial_governance.sql`.
- Finance user and finance approver roles.
- OpenAPI coverage for all 104 implemented API routes.

## Validation boundary

The Go domain, HTTP contract, migration shape and repository compilation are tested. Live PostgreSQL execution remains blocked until a reproducibly pinned SQL driver is linked and an integration database is available.
