# Release 0.8.28 — backend production engineering

## Status

This document describes an interim runtime-validated recovery checkpoint on branch `work/backend-production-engineering`. It is based on the completed `0.8.27` commit `d002177c6ea52500053134398282f5410d8e6e2a`.

The checkpoint is committed and packaged to preserve the substantial `0.8.28` work. It is not the final release candidate and is not deployment-certified.

## Production-engineering scope

- Native `libpq` PostgreSQL runtime integration with TLS/SCRAM, cancellation, arrays, domain values, typed-null handling and SQLSTATE-preserving errors.
- Clean PostgreSQL migration chain through migration `0066` and live validation on isolated Neon PostgreSQL 18.4.
- Exact service-role grants, post-migration reconciliation and runtime rejection of unsafe database identities.
- Prometheus-compatible metrics, W3C trace context, sanitised structured logging and protected profiling.
- Durable gateway replay protection, restart-safe idempotency and signed runtime registration.
- Partition-readiness governance, supporting indexes, maintenance functions and adversarial validation assets.
- Production Compose hardening, reduced secret exposure, SBOM generation and committed-secret scanning.
- Retry-safe retention object deletion and live multi-session PostgreSQL concurrency evidence.

## Runtime defects corrected

The real Docker build and startup gate found defects that syntax-only and mocked tests did not expose. The checkpoint preserves their fixes and regression coverage:

- missing gateway gauge and summary metric methods;
- incomplete control-plane, campaign and export-worker keyring wiring;
- missing export-worker readiness configuration;
- invalid campaign, inbound rotation, capacity and identity SQL contracts;
- typed-nil `libpq` parameter panic;
- governance policy persistence that discarded approval actors or encoded JSONB as bytea;
- stale schema-readiness relation names;
- admin web listener bound to a container hostname rather than all interfaces;
- absent edge health and readiness routes.

The implementation was corrected without deleting features, disabling workers or weakening readiness checks.

## Full-stack Docker evidence

The authoritative desktop repository built and ran as Docker Compose project `openwa0828smoke`. All 14 containers became healthy with zero restart counts:

- PostgreSQL, Redis and ClamAV;
- OpenWA upstream and the isolated gateway;
- control API and admin web behind nginx;
- audience, campaign, export, inbound-governance, metrics and platform-governance workers.

The public UI, `/healthz` and `/readyz` returned HTTP 200. Control API readiness confirmed the database and canonical schema. The final observation window contained no fresh application or PostgreSQL errors.

## Dependency and build-chain hardening

- Admin web upgraded to Next.js `16.3.0`; its npm audit is zero.
- OpenWA gateway upgraded to current compatible NestJS 11 releases; its npm audit is zero.
- OpenWA server lockfile received compatible transitive fixes; its npm audit is zero.
- Admin and gateway now use committed package-local lockfiles and `npm ci` in Docker.
- The root `.dockerignore` excludes local dependency caches, Git history and generated output; first-party build context fell from hundreds of megabytes to about 80 KB.

The OpenWA dashboard is pinned to React Router DOM `7.18.2`. npm currently reports one underlying high-severity advisory, `GHSA-qwww-vcr4-c8h2`, through two package entries. The advisory concerns RSC action processing. This dashboard is a Vite-built client-only SPA using `BrowserRouter` and contains no React Server Components, server actions or server request handler.

`SEC-EXC-001` records this as a pending, expiring exception candidate. Automated validation rejects additional advisories, RSC adoption, version drift and expiry. Strict release security remains blocked until an independent security reviewer approves the exception.

## Verification evidence

- full Go tests and `go vet ./...` passed;
- race detection passed for every Go package changed by the runtime corrections;
- gateway durability and TypeScript syntax gates passed;
- admin typecheck and production build passed;
- OpenWA dashboard typecheck, all 273 tests and whole-upstream Docker build passed;
- OpenAPI covers 277 implemented `/api/v1` method/path pairs;
- production Compose, committed-secret scan, SBOM and governance structure checks passed;
- CycloneDX SBOM contains 24 components.

## Live PostgreSQL evidence

Migrations `0001` through `0066` were executed in order on an isolated clean Neon PostgreSQL 18.4 branch. The final validated schema contained 128 base tables, 411 indexes, 61 user triggers, one view and five governed partition policies.

Exact service-role grants and six independent concurrency/failure-boundary scenarios were also verified. See `docs/operations/NEON_POSTGRES_VALIDATION_0.8.28.md`.

## Remaining before final 0.8.28

- Complete real continuation pagination for remaining growing operational histories.
- Reconcile the full 398-row requirements catalogue against current evidence.
- Complete the adversarial Backend Release Readiness Review.
- Refresh final handover and traceability documentation.
- Obtain independent security disposition for `SEC-EXC-001` or upgrade once a fully patched compatible React Router release exists.
- Produce and independently verify the final release ZIP, Git bundle and checksum manifest after the remaining work.

External validation remains required for authenticated OpenWA engine sessions, production-scale endurance, Hostinger deployment, backup/restore and disaster recovery, penetration testing, operational approval and the production frontend.
