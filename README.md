# Internal Audience & Campaign Platform

Private, internally operated platform for consent-governed audience management, cohort creation, campaign approvals, durable WhatsApp dispatch through isolated OpenWA gateway pools, and truthful campaign reporting.

## Operating model

External organisations do not receive portal access in the initial release. They request campaigns offline and provide the consent basis, approved content and audience data. Internal operators:

1. Create and govern the organisation record.
2. Record and decide the offline consent review.
3. Preview, validate and reconcile audience imports.
4. Build a cohort using governed filters.
5. Freeze an immutable audience snapshot.
6. Approve the message, commercial terms, routing plan and final release.
7. Dispatch through approved gateway and sender pools with durable recipient obligations.
8. Report submitted, sent, delivered, read, failed and unknown outcomes separately.

## Technology boundaries

- **Next.js/TypeScript** provides the internal operations portal.
- **Go** owns organisations, consent, contacts, segmentation, campaigns, approvals, routing, delivery state, metrics and reporting.
- **PostgreSQL** is the authoritative system of record.
- **Redis** is an execution accelerator, never the sole source of recipient or delivery obligations.
- **OpenWA/NestJS** is an isolated, replaceable messaging transport gateway.
- **S3-compatible storage** holds import files, consent evidence, campaign media and generated reports.
- **Hostinger VPS/Docker Compose** is the initial deployment target.

## Current checkpoint

Version `0.8.27` is the platform-governance and operational-control checkpoint based on `0.8.26`. It remains pre-production and must not be represented as deployment-certified.

The repository includes:

- Server-derived identity, MFA/session, RBAC and maker-checker controls.
- Governed organisation, consent, suppression, audience, campaign, commercial, privacy and export lifecycles.
- Immutable audience snapshots, approved message versions and recipient entitlements.
- Durable workers, fenced leases, queue reconstruction and unknown-outcome protection.
- Multi-pool routing across separately governed OpenWA `WHATSAPP_WEB_JS` and `BAILEYS` gateway pools.
- Canonical sender/session lifecycle, owner-node and lease fencing, trusted media/evidence, and signed delivery/inbound callbacks.
- Secure deterministic exports, privacy cases/legal holds, small-cohort report suppression, real XLSX imports, governed mappings/rollback/source retention and filesystem or S3/MinIO object storage.
- Governed gateway-pool maker-checker administration and protected node drain/offline/retirement operations.
- Signed gateway runtime registration with restart-safe nonce replay protection, boot generation, build/configuration/capability drift detection, heartbeat, capacity, queue, CPU and memory evidence.
- Shared platform configuration governance with canonical JSON validation, effective dating, overlap prevention, immutable supersession, retirement and rollback-by-replacement.
- Governed maintenance windows and emergency-stop controls enforced by API admission, campaign start/resume, final dispatch and session lifecycle services.
- Cross-domain retention policies, durable lease-fenced jobs, legal-hold-aware scheduling, supported data actions and fail-closed review for unsupported destruction.
- Governed operational alert policies, threshold evaluation, deduplication, recovery, escalation, portal notifications, automatic incidents and append-only incident timelines.
- A separate `platform-governance-worker` for restart-safe retention scheduling/execution and alert evaluation/escalation.
- PostgreSQL migrations through `0065_platform_governance_runtime_retention_and_alerts.sql`.
- OpenAPI coverage for all 277 implemented `/api/v1` method/path pairs.
- An SRS traceability catalogue containing 398 granular requirement records: 107 implemented/tested, 44 partial and 247 not started.
- Imported OpenWA source under the repository's governed third-party-source model; see `UPSTREAM.md` and `THIRD_PARTY_NOTICES.md`.

## Verified code-level checks

The checkpoint passes:

```bash
make fmt
go test ./...
go vet ./...
make build
make frontend-syntax
python3 scripts/verify_openapi_routes.py
make governance-test
make governance-check
```

Race-sensitive backend packages are also tested with `go test -race` in bounded package groups. The aggregate repository race command can exceed constrained CI/workspace execution windows because the HTTP security tests intentionally perform expensive password hashing.

## Release gates still open

Code-level completion is not the same as production validation. The following remain required:

- Execute the complete migration chain against production-like PostgreSQL and retain locking, upgrade and recovery evidence.
- Validate query plans and endurance at target recipient, campaign and reporting volumes.
- Contract-test and operate genuine OpenWA `WHATSAPP_WEB_JS` and `BAILEYS` sessions, including pairing, media, callbacks, reconnects and uncertain outcomes.
- Complete security assurance, secret/key rotation and penetration testing.
- Prove backup, restore and disaster recovery.
- Validate multi-node Hostinger deployment, observability and operational runbooks.
- Complete the production frontend and the full 398-requirement evidence reconciliation.

`make release-gate` must remain closed until the governed evidence for these gates has been reviewed and accepted.

## Local checks and deployment assets

```bash
make check
```

When all Node dependencies are available:

```bash
npm install
npm run typecheck:web
npm run typecheck:gateway
npm run build:web
npm run build:gateway
```

Docker Compose assets are under `infrastructure/compose/compose.yaml`. Live Compose, PostgreSQL, Redis, OpenWA and Hostinger validation must be performed in an environment that exposes those dependencies.
