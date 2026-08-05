# Current status

## Current repository baseline

Version `0.8.24` on branch `work/backend-hardening-scalability` is the current backend checkpoint.

The platform is an advanced, governed campaign control plane with durable audience, campaign, messaging, delivery, operational and reporting foundations. It is not a basic OpenWA dashboard and OpenWA is not the source of campaign truth.

## Latest hardening

- Exact provider capability definition IDs and versions are frozen into campaigns and controlled test sends.
- Campaign release, resume, runtime dispatch and test-message execution fail closed on provider, engine, gateway, adapter, minimum-version or capability drift.
- Overlapping active provider definitions are prevented by transaction-level route locking and database effective-period exclusion.
- Campaign-recipient and outbox diagnostic reads are bounded and keyset-paginated; other high-risk administrative histories have explicit hard bounds.
- Multi-pool admission evaluates minute, hourly and daily capacity over the real campaign window and enforces overlapping hourly reservations.
- Operations dashboard primary totals come from canonical aggregate evidence.
- Invalid campaign PostgreSQL insert SQL and silent persistence error handling discovered during adversarial review have been corrected.
- Migration `0059_backend_hardening_and_provider_binding.sql` is included and required by readiness checks.

## Honest readiness position

The repository is backend-advanced but not deployment-certified. Remaining hard gates include:

- complete 398-requirement evidence reconciliation;
- live PostgreSQL migration, concurrency and query-plan evidence;
- genuine OpenWA `WHATSAPP_WEB_JS` and `BAILEYS` operation;
- production-like endurance, multi-node and recovery tests;
- security assessment and secret/key operations;
- backup, restore and disaster-recovery proof;
- Hostinger operational readiness; and
- production frontend completion.

`make release-gate` must remain closed until reviewed evidence satisfies those gates.
