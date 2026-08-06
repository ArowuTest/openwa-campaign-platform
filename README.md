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

Version `0.8.28` is an **interim runtime-validated backend-production-engineering checkpoint** on branch `work/backend-production-engineering`, based on the completed `0.8.27` commit `d002177c6ea52500053134398282f5410d8e6e2a`.

The repository includes the earlier `0.8.28` PostgreSQL, service-identity, observability, replay-safety, partition-readiness and supply-chain work, plus corrections found only by building and running the complete Docker stack:

- native `libpq` handling for PostgreSQL domains, typed nil parameters and SQL-state-preserving errors;
- valid campaign lease, controlled-test-message, inbound rotation, identity bootstrap, capacity and governance-policy SQL;
- control-plane secret and keyring wiring, export-worker readiness and explicit admin runtime binding;
- canonical schema-readiness checks and public edge `/healthz` and `/readyz` routes;
- gateway gauge and summary metrics with executable durability coverage;
- deterministic package-local lockfiles and `npm ci` for first-party Node images;
- Next.js `16.3.0` and NestJS dependency upgrades, with zero npm findings for the admin portal and gateway;
- zero npm findings for the OpenWA server dependency tree;
- an explicit, expiring and strict-release-blocking exception candidate for the sole remaining OpenWA-dashboard React Router advisory, which is limited to RSC mode that the Vite/BrowserRouter dashboard does not expose;
- a root `.dockerignore` that excludes Git history, local dependency caches and generated output from build contexts.

This checkpoint is intentionally recoverable and fully runnable in local Docker, but it is **not the final or deployment-certified `0.8.28` release**.

## Current verification evidence

The authoritative desktop repository was built and started through Docker Compose as project `openwa0828smoke`. The verified runtime contained 14 containers: PostgreSQL, Redis, ClamAV, OpenWA upstream, OpenWA gateway, control API, admin web, nginx and all seven Go workers.

At the final runtime snapshot:

- all 14 containers were healthy;
- all restart counts were zero;
- the public admin UI returned HTTP 200;
- public `/healthz` and `/readyz` both returned HTTP 200;
- control API readiness reported PostgreSQL and schema checks as healthy;
- no fresh application-level errors were present;
- no fresh PostgreSQL errors or failed statements were present.

The source has also passed:

```bash
go test -count=1 ./...
go vet ./...
go test -race <all changed Go packages>
node scripts/test-gateway-durability.js
python3 -m unittest tests.governance.test_release_readiness tests.governance.test_stack_startup
python3 scripts/verify-node-security.py
python3 scripts/verify_openapi_routes.py
python3 scripts/verify-production-compose.py
python3 scripts/scan-committed-secrets.py
python3 scripts/generate-sbom.py
python3 scripts/verify-release-readiness.py
```

The CycloneDX SBOM contains 24 components. OpenAPI verification covers 277 implemented `/api/v1` method/path pairs. Live PostgreSQL evidence remains recorded in `docs/operations/NEON_POSTGRES_VALIDATION_0.8.28.md`, including clean migrations `0001` through `0066`, exact service-role grants and six independent multi-session adversarial scenarios.

## Release gates still open

This interim checkpoint must not be represented as the final `0.8.28` release. Remaining code and evidence work includes:

- genuine continuation pagination for remaining growing operational histories;
- reconciliation of all 398 requirement records against code, migrations, tests, frontend scope and external evidence;
- the adversarial Backend Release Readiness Review and final handover documentation;
- independent review of the pending, time-bounded React Router RSC advisory exception; strict security verification fails until that approval is recorded;
- final release packaging and independent clone/extraction verification after the remaining code work.

External or environment-dependent gates remain separate: authenticated real WhatsApp sessions, target-volume endurance, Hostinger deployment, backup/restore and disaster recovery, penetration testing, operational approval and the production frontend.

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

Docker Compose assets are under `infrastructure/compose/compose.yaml`. Live Compose, PostgreSQL, Redis, OpenWA and Hostinger validation must be performed in an environment that exposes those dependencies.
