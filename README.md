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

Version `0.8.28` is the current backend production-engineering checkpoint on branch `work/backend-production-engineering`.

Recent implementation lineage:

- `6b668dc` — OpenWA worker-consolidation design;
- `c70956a` — consolidated OpenWA worker and runtime hardening.

The repository includes the 0.8.28 PostgreSQL, service-identity, observability, replay-safety, partition-readiness and supply-chain work, plus runtime corrections found through real Docker startup and consolidation testing:

- native `lib/pq` PostgreSQL integration and bounded startup retry/backoff;
- valid campaign, inbound, identity, capacity and governance-policy SQL contracts;
- control-plane keyring/secret wiring, worker readiness and admin runtime binding;
- canonical schema-readiness checks and public edge `/healthz` and `/readyz` routes;
- durable gateway replay/idempotency and signed runtime registration;
- deterministic package-local lockfiles and `npm ci` for first-party Node images;
- audit-clean compatible first-party Node dependency trees;
- one isolated OpenWA gateway worker instead of a separate upstream server plus adapter;
- retained OpenWA liveness supervision, narrow media SSRF allowlisting and durable event/inbound handling;
- Docker/Desktop-compatible read-only secret-file validation without weakening ordinary deployed file checks;
- a root `.dockerignore` that excludes Git history, local dependency caches and generated output from build contexts.

The consolidation preserves the platform transport surface rather than obtaining a green build by removing features. The pre- and post-consolidation provider method sets are identical, and session lifecycle, QR/pairing, drain/resume and text/image/video/document send paths remain.

This checkpoint is intentionally recoverable and runnable in local Docker, but it is **not production deployment-certified**.

## Current verification evidence

The active development Docker Compose project is `openwa0828active`. The verified runtime contains **13 containers**: PostgreSQL, Redis, ClamAV, one consolidated OpenWA gateway, control API, admin web, nginx, and the six Go workers (audience, campaign, export, inbound-governance, metrics and platform-governance).

At the post-fix runtime snapshot:

- all 13 containers were healthy;
- the recreated final gateway had zero restarts;
- the gateway ran as non-root and exposed no direct host port;
- the gateway image contained Chromium and compiled transport code but no upstream dashboard/server application;
- the public admin UI returned HTTP 200;
- public `/healthz` and `/readyz` both returned HTTP 200;
- no fresh application or PostgreSQL errors were present in the observation window.

Fresh source verification includes:

```text
Gateway TypeScript typecheck: passed
Final gateway Docker build: passed
Embedded OpenWA lifecycle/event/watchdog tests: passed
Gateway durability tests: passed
Node secret-file tests: passed
Stack/release governance tests: 12 passed
Strict Node security validation: passed with no production exceptions
Production Compose validation: passed (8 services / 24 secret definitions)
OpenAPI parity: 277 implemented /api/v1 method/path pairs covered
Committed-secret scan: passed
go vet ./...: passed
Race tests for changed Go packages: passed
Tracked root Go package directories: 55/55 passed
Nested supplied OpenWA Go SDK: passed
```

The normal all-at-once recursive Go orchestration was investigated when it could stall on the mixed Go/Node Windows bind-mounted working tree. Verification was therefore rerun deterministically package-by-package with visible package names and explicit timeouts; all tracked Go package directories passed.

Live PostgreSQL evidence remains recorded in `docs/operations/NEON_POSTGRES_VALIDATION_0.8.28.md`, including clean migrations `0001` through `0066`, service-role checks and adversarial concurrency/failure-boundary scenarios. The isolated Neon validation branch `openwa-0828-validation` remains present, and the latest consolidation does not change migrations or schema contracts.

## Release gates still open

The granular requirements catalogue currently contains 398 rows: 107 `IMPLEMENTED_TESTED`, 44 `PARTIAL`, and 247 `NOT_STARTED`. These include frontend, repository-governance, deployment, operational and external-validation requirements and are not a backend completion percentage.

The remaining release work includes:

- full requirements/evidence reconciliation;
- the adversarial Backend Release Readiness Review;
- authenticated real WhatsApp session validation on the consolidated worker;
- target-volume performance/endurance evidence;
- independent security assurance and penetration testing;
- Hostinger/target-host deployment and network validation;
- backup, restore and disaster-recovery proof;
- operational-owner approval;
- production frontend completion after backend closure.

`make release-gate` must remain closed until the applicable governed evidence has been reviewed and accepted.

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

Docker Compose assets are under `infrastructure/compose/compose.yaml`. Live authenticated WhatsApp, target-host, capacity and disaster-recovery validation must be performed in authorised environments that expose those dependencies.
