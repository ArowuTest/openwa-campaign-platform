# ADR-0007: Railway is the authoritative production PostgreSQL control/data plane

**Status:** Accepted  
**Date:** 2026-10-02  
**Scope:** OpenWA Campaign Platform V1 production infrastructure

## Context

The release-one backend is a governed internal WhatsApp campaign operations platform. Its business/control plane is the Go backend and PostgreSQL. PostgreSQL is authoritative for campaigns, recipient ledger, audience snapshots, consent/suppression, sender configuration, routing/reservations, delivery state, UNKNOWN outcomes, audit, governance and operations.

The documented production split is:

- **Railway:** authoritative control/data plane — Go APIs/workers, PostgreSQL, Redis, private application networking and object storage.
- **Hostinger VPS:** WhatsApp execution/gateway fleet — Baileys, whatsapp-web.js, Chromium, persistent session state, sender/session proxies and gateway-node capacity.
- **Meta Cloud API:** direct sibling transport from the control plane, never downstream of the OpenWA gateway.

A verification PostgreSQL instance already exists in the separate Railway verification project. The new `openwa-prod` project currently has only empty backend service slots; it does not yet have a production database.

## Decision

The authoritative V1 production PostgreSQL database **must be Railway managed PostgreSQL inside the `openwa-prod` production environment**.

Hostinger must not host the authoritative campaign-platform PostgreSQL database. Hostinger remains the stateful OpenWA gateway/session/browser runtime boundary.

Supabase is not part of the V1 production architecture. It may be reconsidered later only through a new ADR and migration plan.

## Required production posture

The production PostgreSQL service must provide or be configured with:

1. private Railway connectivity to the backend/control-plane services;
2. no public database exposure unless a documented break-glass exception is approved;
3. SSL-required client connections;
4. service-specific login roles mapped to the stable repository-managed NOLOGIN privilege roles;
5. no owner/migration role supplied to application containers;
6. PITR/WAL retention enabled before production acceptance;
7. backup and restore evidence, including at least one restore rehearsal;
8. migration validation against a clean production-equivalent database;
9. upgrade-path validation from the accepted baseline to the current production candidate;
10. operational monitoring and alerting for database health, storage, backup, WAL/PITR, connection saturation and migration failures.

The stable privilege roles are:

- `campaign_control_api`
- `campaign_audience_worker`
- `campaign_campaign_worker`
- `campaign_export_worker`
- `campaign_inbound_governance_worker`
- `campaign_metrics_worker`
- `campaign_platform_governance_worker`

Each Railway service receives only its own rotatable login credential and `DATABASE_EXPECTED_ROLE` for the corresponding stable privilege role.

## Non-decisions and explicit exclusions

- This ADR does not provision the database.
- This ADR does not close RG-002, RG-006 or any other release gate.
- This ADR does not authorize committing credentials or connection strings.
- This ADR does not deploy code to production.
- This ADR does not change the Hostinger gateway boundary.
- This ADR does not introduce Supabase into V1.

## Consequences

The next infrastructure step is to provision a Railway managed PostgreSQL instance in `openwa-prod`, then bind service-specific database credentials to the seven backend services without writing secrets to source.

Release-gate closure remains blocked until production-equivalent migration, backup/restore, PITR and operational evidence exists.
