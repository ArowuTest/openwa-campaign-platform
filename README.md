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

## Current development checkpoint

Version `0.8.28` is an **interim backend-production-engineering checkpoint** on branch `work/backend-production-engineering`, based on the completed `0.8.27` commit `d002177c6ea52500053134398282f5410d8e6e2a`.

This checkpoint is intentionally preserved before the final release-readiness review. It is substantially beyond `0.8.27`, but it is **not yet the final or deployment-certified `0.8.28` release**.

The repository now includes the `0.8.27` functional and governance platform plus the following `0.8.28` production-engineering work:

- A real PostgreSQL runtime driver backed by native `libpq`, including TLS/SCRAM support, prepared statements, cancellation, SQLSTATE-preserving errors, PostgreSQL array handling and a fail-closed non-CGO build path.
- Clean PostgreSQL migrations through `0066_partition_readiness_and_maintenance.sql`.
- Live clean-install validation of migrations `0001` through `0066` on an isolated Neon PostgreSQL 18.4 branch.
- Corrections for stale migration references to obsolete role, user and segment table names, with regression tests preventing recurrence.
- Service-specific PostgreSQL privilege roles, post-migration grant reconciliation, explicit table-level worker grants and runtime rejection of superusers or cross-service role membership.
- Prometheus-compatible metrics, W3C trace-context propagation, sanitised structured logs and protected profiling controls across Go services and the OpenWA gateway.
- Durable gateway command replay protection, restart-safe idempotency retention, concurrent identical-request sharing, signed runtime registration and heartbeat evidence.
- Secret-file loading, previous-key verification windows and separated command, callback and runtime-registration signing credentials.
- Standard cursor envelopes on the principal cursor-based APIs, with a wider operational-history pagination audit still open.
- Partition-readiness policies, maintenance functions, high-volume time-key indexes and rollback-based partition validation assets.
- Production Compose hardening, immutable-image requirements, reduced secret exposure, optional S3 session-token overlay, SBOM generation and committed-secret scanning.
- Retry-safe object deletion for retention workflows after partial external success.
- An opt-in PostgreSQL adversarial integration suite covering concurrent release, `SKIP LOCKED` claims, duplicate provider events, stale fencing, transaction atomicity and overlapping capacity reservations.

## Current verification evidence

The current working tree has passed the following code-side gates during `0.8.28` development:

```bash
go test ./...
go vet ./...
make build
make frontend-syntax
python3 scripts/verify_openapi_routes.py
python3 scripts/verify-production-compose.py
python3 scripts/scan-committed-secrets.py
python3 scripts/generate-sbom.py
node scripts/test-gateway-durability.js
```

Race-sensitive Go packages have been completed in bounded groups; the 26 HTTP-server tests were also run individually under the race detector because a single aggregate HTTP package run exceeds constrained workspace execution limits.

Live PostgreSQL evidence is recorded in `docs/operations/NEON_POSTGRES_VALIDATION_0.8.28.md`. The clean Neon validation branch reached:

- 128 base tables;
- 411 indexes;
- 61 user triggers;
- 1 view;
- 5 governed partition policies.

The same environment was used to validate exact service-role grants and six independent multi-session adversarial scenarios. No database credential is stored in this repository.

## Release gates still open

This interim checkpoint must not be represented as the final `0.8.28` release. The remaining work includes:

- Complete genuine continuation pagination for the remaining growing operational histories; bounded limits alone are not being misrepresented as pagination.
- Reconcile all 398 requirement records against the final backend code, migrations, tests, frontend scope and external evidence. The inherited `107 implemented / 44 partial / 247 not started` catalogue is stale and is not a backend-completion percentage.
- Complete the adversarial Backend Release Readiness Review across APIs, workers, migrations, gateway contracts, deployment assets, monitoring and operational controls.
- Refresh final release notes, handover material and traceability evidence.
- Run the final complete verification gate after all remaining corrections.
- Commit and independently verify the final source archive, complete Git bundle and checksum manifest.

External or environment-dependent gates will remain separate after code completion, including genuine `whatsapp-web.js` and Baileys sessions, target-volume endurance, Hostinger deployment, backup/restore and disaster recovery, penetration testing, and the production frontend.

`make release-gate` must remain closed until governed evidence for the applicable release gates has been reviewed and accepted.

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
