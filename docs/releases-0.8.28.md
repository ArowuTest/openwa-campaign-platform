# Release 0.8.28 — backend production engineering (interim checkpoint)

## Status

This document describes an interim recovery checkpoint on branch `work/backend-production-engineering`. It is based on the completed `0.8.27` commit `d002177c6ea52500053134398282f5410d8e6e2a`.

The checkpoint is intentionally committed and packaged to prevent loss of the substantial `0.8.28` work. It is **not the final release candidate and is not deployment-certified**.

## Implemented or materially progressed

- Native `libpq` PostgreSQL driver integration with fail-closed non-CGO behaviour.
- Secret-file loading, secret separation and verification-only previous-key rotation windows.
- Service-specific PostgreSQL roles and exact post-migration privilege reconciliation.
- Prometheus-compatible metrics, W3C trace context, sanitised structured logging and protected profiling.
- Durable gateway replay protection and idempotency retention across restarts.
- Concurrent identical gateway submissions share one in-flight result.
- Signed gateway runtime registration and heartbeat evidence.
- Standard cursor envelopes for principal cursor-based API endpoints.
- Partition-readiness governance, maintenance functions, supporting indexes and validation assets.
- Production Compose hardening, immutable-image requirements, reduced secret exposure, SBOM generation and secret scanning.
- Retry-safe object deletion for retention workflows.
- Clean PostgreSQL migration chain through migration `0066`.
- Opt-in PostgreSQL adversarial integration tests.

## Live PostgreSQL evidence

Migrations `0001` through `0066` were executed in order on an isolated clean Neon PostgreSQL 18.4 branch. Live execution found and corrected stale schema references that static validation had missed.

The final validated schema contained 128 base tables, 411 indexes, 61 user triggers, one view and five governed partition policies. Exact service-role grants and six independent concurrency/failure-boundary scenarios were also verified.

See `docs/operations/NEON_POSTGRES_VALIDATION_0.8.28.md`.

## Current code gates

The working tree has passed ordinary Go tests, vet, all seven Go executable builds, TypeScript syntax validation, OpenAPI reconciliation, gateway durability tests, production Compose verification, committed-secret scanning and SBOM generation. Race-sensitive packages have been run in bounded groups, with all HTTP tests completed individually under the race detector.

## Remaining before final 0.8.28

- Complete real continuation pagination for remaining growing operational histories.
- Reconcile the full 398-row requirements catalogue against current evidence.
- Complete the adversarial Backend Release Readiness Review.
- Refresh final release, handover and traceability documentation.
- Rerun the complete final gate.
- Produce and independently verify the final source ZIP, Git bundle and checksum manifest.

External validation remains required for real OpenWA engine sessions, production-scale endurance, Hostinger deployment, backup/restore and disaster recovery, penetration testing, operational monitoring delivery and the production frontend.
