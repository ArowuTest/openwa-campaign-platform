# Release 0.8.28 — backend production engineering

## Status

Version `0.8.28` is the current backend production-engineering checkpoint on branch `work/backend-production-engineering`.

The OpenWA worker-consolidation design is recorded in commit `6b668dc`. The verified implementation and runtime hardening are recorded in commit `c70956a` (`feat: consolidate OpenWA worker and harden runtime`).

This checkpoint is code- and runtime-validated to the extent possible on the authorised development workstation. It is **not** a production deployment certificate: live authenticated WhatsApp sessions, target-volume endurance, target-host deployment, backup/restore, disaster recovery, penetration testing, operational approval and frontend completion remain separate gates.

## Architecture position

The platform remains an internal managed-service campaign control plane.

- Go and PostgreSQL remain authoritative for consent, audience eligibility, campaign obligations, sender/session governance, canonical delivery state, privacy, configuration, retention, reporting, incidents and audit.
- Redis remains reconstructable execution infrastructure.
- OpenWA remains a replaceable WhatsApp transport boundary.
- The production topology now uses **one isolated `openwa-gateway` worker**, not a separate upstream OpenWA server plus adapter.
- The gateway synchronises an audited transport subset from `third_party/openwa/upstream/src` into generated `src/retained-openwa` source at build/typecheck time and runs the retained engines in-process.
- The upstream dashboard/server application is not built or shipped in the production gateway image.

The consolidation changes the implementation boundary, not the platform capability surface. A direct before/after provider comparison confirms the same ten provider operations remain: send, health, get/create/start/stop/logout/delete session, QR and pairing code. The gateway service also retains drain/resume, and the embedded engine retains text, image, video and document sends.

## Material 0.8.28 corrections

The production-engineering work includes:

- native `lib/pq` PostgreSQL integration and production startup retry/backoff for transient database availability;
- clean PostgreSQL migration and runtime validation evidence already captured for migrations `0001` through `0066`;
- exact service-role, runtime-identity and configuration controls;
- Prometheus-compatible metrics, W3C trace propagation, structured logging and protected profiling;
- durable gateway replay protection, restart-safe idempotency and signed runtime registration;
- production Compose hardening, secret-file support, SBOM/security checks and committed-secret scanning;
- partition-readiness, retention and failure-boundary controls;
- OpenWA worker consolidation into one isolated runtime with retained upstream engine/adaptor behaviour;
- active session liveness supervision for silently wedged READY engines;
- narrow retained-OpenWA SSRF allowlisting for the signed internal `control-api` media endpoint while other private hosts remain blocked;
- Docker/Desktop-compatible secret-file validation that still rejects unsafe ordinary files and accepts only a genuine read-only `/run/secrets/*` runtime mount when translated mode bits are misleading;
- removal of the obsolete separate-upstream secret entrypoint and deployment dependency, without removing the transport capabilities it previously served.

## OpenWA preservation and hardening

The consolidation deliberately preserves functionality instead of compiling by subtraction.

- `WHATSAPP_WEB_JS` and `BAILEYS` retained engine paths remain available.
- Session create/start/stop/logout/delete, health, QR and pairing-code flows remain available.
- Text, image, video and document send paths remain available.
- inbound message, delivery/read event and durable callback/outbox handling remain wired.
- session authority, signed command validation, idempotency and node/pool identity checks remain outside the retained upstream code and continue to gate submissions.
- the upstream liveness-watchdog semantics were restored into the embedded runtime: READY engines are actively probed, repeated failures trigger fenced teardown/recovery, ACTION_REQUIRED remains observe-only, and stale probe results cannot act on a superseded engine.
- retained media fetching keeps SSRF protection enabled. Only the exact internal hostname `control-api` is allowlisted for the platform's signed media URL; a second private service (`postgres`) was explicitly verified as still blocked.

## Current Docker runtime evidence

The active Docker Compose project `openwa0828active` runs **13 containers**:

- PostgreSQL, Redis and ClamAV;
- the single consolidated OpenWA gateway;
- control API and admin web behind nginx;
- audience, campaign, export, inbound-governance, metrics and platform-governance workers.

At the post-fix observation point all 13 containers were healthy. The recreated final gateway image reported zero restarts, ran as non-root `node`, exposed no host port directly, contained Chromium and the compiled gateway, and contained no upstream dashboard/server application directory.

The public UI, `/healthz` and `/readyz` on the development edge returned HTTP 200. A fresh multi-service log scan found no application or PostgreSQL errors in the observation window.

## Verification evidence

Fresh verification for the consolidated checkpoint includes:

- gateway TypeScript `tsc --noEmit`: passed in a clean ephemeral workspace;
- final OpenWA gateway Docker image build: passed; production dependency audit reported zero vulnerabilities;
- embedded OpenWA lifecycle/event/watchdog regression bundle: passed;
- gateway durability tests: passed;
- Node secret-file tests: passed;
- stack-governance and release-readiness unit tests: 12 passed;
- strict Node security validator: passed with **no production exceptions**;
- production Compose structural validator: passed (8 services, 24 secret definitions);
- OpenAPI parity: 277 implemented `/api/v1` method/path pairs covered;
- committed-secret scan: passed;
- `go vet ./...`: passed before the final documentation phase;
- changed Go packages (`internal/persistence/database`, `internal/shared/envfile`) passed under the race detector;
- deterministic explicit Go verification passed all 55 tracked root Go package directories plus the nested supplied OpenWA Go SDK.

The normal recursive `go test ./...`/`go list ./...` orchestration was also investigated because it could stall on the mixed Go/Node Windows bind-mounted working tree. Rather than treating an opaque coordinator stall as a pass, verification was rerun package-by-package with visible package names and explicit timeouts; all tracked Go package directories passed.

## PostgreSQL evidence

The isolated Neon validation branch `openwa-0828-validation` (`br-green-pine-aykv09eo`) remains present and intact. Earlier 0.8.28 validation executed migrations `0001` through `0066` on a clean Neon PostgreSQL 18.4 branch and captured role/concurrency evidence in `docs/operations/NEON_POSTGRES_VALIDATION_0.8.28.md`.

Commit `c70956a` does not change migrations or SQL schema contracts, so that schema evidence was not destructively rerun merely to exercise transport/runtime changes.

## Requirements and release gates

The granular requirements catalogue remains:

```text
Total requirements:       398
Implemented and tested:   107
Partial:                    44
Not started:               247
```

Those counts include frontend, repository-governance, deployment, operational and external-validation requirements and must not be presented as a backend completion percentage.

The release-readiness gate file still reports eight hard gates as open or externally blocked: authoritative requirements traceability, production-like PostgreSQL validation, live OpenWA transport validation, capacity/performance evidence, independent security assurance, backup/DR proof, operational readiness and frontend workflow completion.

## Remaining work

The next work should close evidence and external gates without removing or weakening product functionality:

1. reconcile the 398-row requirements catalogue against the now-current 0.8.28 implementation/evidence;
2. complete the adversarial Backend Release Readiness Review and address any genuine code findings;
3. run authenticated WhatsApp session validation (QR/pairing, sends, inbound, acknowledgements, reconnect/recovery) on the consolidated worker;
4. produce target-volume performance/endurance evidence;
5. complete independent security review/penetration testing and operational key-management approval;
6. complete target-host deployment, backup/restore and disaster-recovery proof;
7. complete operational runbook/owner approval;
8. begin/complete the production frontend only after the backend closure review confirms there is no further code-side backend work that can reasonably be completed first.
