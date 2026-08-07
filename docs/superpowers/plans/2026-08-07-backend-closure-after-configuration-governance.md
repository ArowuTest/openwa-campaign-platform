# Backend Closure After Configuration Governance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish all backend code-side work that can reasonably be completed before frontend development, without deleting capabilities or weakening controls.

**Architecture:** Preserve Go/PostgreSQL as the authoritative control plane and the consolidated NestJS OpenWA worker as transport only. Operational settings become governed PostgreSQL configuration where they can be applied safely; deployment values remain bootstrap/fallback; secrets remain secret-file/keyring driven.

**Tech Stack:** Go, PostgreSQL/Neon, NestJS/TypeScript, Docker Compose, OpenAPI, retained OpenWA 0.13.0 transport subset.

## Global Constraints

- Never delete required functionality merely to compile or pass a release gate.
- Never weaken idempotency, ownership/fencing, consent, audit, SSRF, secret, or UNKNOWN-outcome controls.
- No reusable production/development secrets may be hard-coded in source.
- Governed runtime values override deployment/bootstrap values within code-owned safety bounds.
- Frontend remains deferred until backend closure confirms no further reasonable code-side work remains.
- Evidence counts are classifications, not a backend completion percentage.

---
### Task 1: Finish governed sender proxy and one-active-campaign default

**Files:**
- Modify: `internal/sender/proxy*.go`, `internal/sender/session_lifecycle.go`, `internal/sender/session_gateway_http.go`
- Modify: `services/openwa-gateway/src/provider/*.ts`, `services/openwa-gateway/src/session.controller.ts`
- Modify: `cmd/control-api/runtime.go`, `internal/platform/httpserver/sender_governance.go`, `contracts/openapi/control-api.yaml`
- Modify: `infrastructure/compose/compose*.yaml`, `.env.example`
- Test: `internal/sender/*_test.go`, `scripts/test-embedded-openwa.js`, `tests/governance/test_stack_startup.py`

**Produces:** encrypted audited per-session proxy configuration, signed start propagation, in-memory-only gateway credential handling, and `DefaultMaxActiveCampaigns = 1` with governed override.

- [ ] Run the targeted Go tests and confirm the proxy/default-policy behaviours fail if the new implementation is reverted.
- [ ] Finish OpenAPI and deployment wiring without embedding secret values.
- [ ] Run gateway typecheck and embedded OpenWA tests proving both engine shapes receive proxy configuration and registry persistence excludes it.
- [ ] Run PostgreSQL-backed tests against an isolated schema/branch for configure, resolve, clear, version conflict and inactive-session enforcement.
- [ ] Rebuild/recreate `control-api` and `openwa-gateway`, then verify health/readiness and no regression to the single-worker topology.
- [ ] Commit the complete tranche as one reviewable engineering change.

---
### Task 2: Complete sender health and account-operational metadata

**Files:**
- Modify: `internal/sender/governance.go`, `internal/sender/postgres_governance.go`, `internal/sender/memory_governance.go`
- Modify: `internal/platform/httpserver/sender_governance.go`
- Modify: `database/migrations/<next>.sql` only if the current schema has no safe governed metadata location
- Modify: `contracts/openapi/control-api.yaml`
- Test: `internal/sender/governance_test.go` and PostgreSQL integration tests

**Produces:** health assessment that uses real sender success recency plus configured health thresholds, and a complete non-secret operational sender record for ownership/registration/profile/recovery-reference fields that remain in scope after reconciliation.

- [ ] Write failing tests proving stale/missing `LastSuccessAt` contributes to health and that health thresholds are not embedded magic values.
- [ ] Determine the narrowest existing governed policy source for heartbeat/success/capacity thresholds; add a focused governed configuration type only if no existing pacing/configuration contract fits safely.
- [ ] Ensure successful provider/delivery evidence updates sender `last_success_at` through an authoritative, idempotent path.
- [ ] Add only the SND-003 metadata fields still required by the evolved specification, with no recovery secrets stored in ordinary sender records.
- [ ] Verify memory and PostgreSQL implementations, API masking, audit attribution and optimistic concurrency.
- [ ] Commit after targeted tests, race checks for changed Go packages and OpenAPI parity pass.

---
### Task 3: Move operator-managed transport tuning into governed runtime configuration

**Files:**
- Modify: `internal/platformpolicy/*` only where existing generic configuration validation/resolution is insufficient
- Modify: `cmd/control-api/runtime.go` and relevant sender/runtime services
- Modify: `services/openwa-gateway/src/provider/embedded-openwa-engine.service.ts`
- Modify: gateway runtime-registration/command contracts only when an effective value must reach the worker
- Modify: `.env.example`, Compose and OpenAPI/admin contracts
- Test: platform-policy resolution tests, sender/runtime tests and embedded gateway tests

**Produces:** deterministic runtime resolution for reconnect limits, watchdog/probe intervals, sender concurrency and transport timeouts where safe, with environment values retained as bootstrap/fallback and code-owned safety ceilings retained as invariants.

- [ ] Inventory each current transport literal/environment setting and classify it as invariant, governed runtime setting, deployment-only setting or secret.
- [ ] Add failing tests for governed override precedence and invalid-value rejection before modifying runtime resolution.
- [ ] Implement the minimum effective-configuration contract needed by the Go control plane and OpenWA worker; do not introduce a second business-authority store in Node.
- [ ] Prove active-session changes are rejected or safely transitioned when hot application would be misleading.
- [ ] Verify configuration source/scope/version can be observed without exposing secrets.
- [ ] Commit this migration independently from unrelated backend findings.

---
### Task 4: Close remaining API scalability and gateway telemetry code gaps

**Files:**
- Review/modify: `internal/shared/httpx/pagination*.go`, operational history handlers/repositories, `contracts/openapi/control-api.yaml`
- Review/modify: gateway runtime registration/observability models and handlers
- Test: pagination continuation tests, runtime telemetry tests, OpenAPI route parity

**Produces:** no unbounded growing-history API left without a real continuation mechanism, and complete safe runtime health evidence where code can supply it.

- [ ] Enumerate every list/history endpoint over a growing table and classify it as keyset/cursor paginated, bounded reference data, or genuine gap.
- [ ] Write a failing continuation test for each genuine growing-history gap before adding pagination.
- [ ] Reconcile `GWY-005` runtime-health evidence: CPU/memory already exist; add disk/inode/process/network evidence only where the worker can measure it truthfully and safely.
- [ ] Keep host/VPS inventory reconciliation as deployment evidence rather than fabricating it inside application code.
- [ ] Run OpenAPI parity and pagination regression tests; commit only evidence-backed changes.

---

### Task 5: Reconcile all 398 requirements against current source and tests

**Files:**
- Modify: `docs/requirements/traceability.json`, generated `docs/requirements/TRACEABILITY.md`
- Review: current source, tests, migrations, OpenAPI, release evidence and later approved scope decisions

**Produces:** an authoritative catalogue that separates implemented/tested, implemented-not-fully-tested, partial, externally blocked, deferred frontend and superseded/not-applicable requirements without converting counts into a completion percentage.

- [ ] Review every requirement row; do not promote from filename/text search alone.
- [ ] For each promotion, attach concrete source/test/migration/evidence references and update stale notes.
- [ ] Mark frontend-only requirements as deferred frontend rather than backend defects where the catalogue schema supports that distinction; otherwise document the distinction explicitly in notes without falsifying status semantics.
- [ ] Record requirements changed/superseded by OpenWA source alignment or later approved discussions.
- [ ] Regenerate the rendered matrix and validate total row count remains 398 unless an explicitly approved supersession model adds metadata rather than deleting rows.
- [ ] Commit the evidence reconciliation separately from implementation changes.

---
### Task 6: Run adversarial backend release-readiness review and fix genuine findings

**Files:**
- Review: all first-party Go services/workers, PostgreSQL repositories/migrations, OpenWA gateway, Compose, OpenAPI, security/configuration and operations code
- Create/update: backend adversarial findings register and release-readiness evidence

**Produces:** a severity-ranked, evidence-backed review of the consolidated backend; Critical/High code findings are fixed through focused TDD changes, not waived by deleting functionality.

- [ ] Review concurrency, idempotency/replay, fencing, UNKNOWN outcomes, transaction boundaries, SQL typing/index use, secret handling, audit attribution, failure recovery, cancellation and retention paths.
- [ ] Re-run full first-party Node typecheck/tests/security validation and deterministic Go package verification.
- [ ] Re-run production Compose validation, committed-secret scan, OpenAPI parity and schema/migration checks.
- [ ] For each Critical/High code finding, write a focused failing regression test and fix it before backend closure.
- [ ] Separate external/deployment findings from code findings; do not claim external gates closed from code-side evidence.

---

### Task 7: Final backend checkpoint before frontend

**Files:**
- Update: `docs/handover/CURRENT_STATUS.md`, `docs/handover/NEXT_ACTIONS.md`, release-readiness evidence and release notes
- Generate: tracked-source ZIP, full Git bundle and SHA-256 manifest from clean committed HEAD

**Produces:** a reproducible backend checkpoint with an honest boundary between code-side closure and remaining live/external validation.

- [ ] Run the complete verification matrix fresh from the final HEAD.
- [ ] Verify Docker development stack health and current consolidated topology.
- [ ] Verify applicable Neon migration/concurrency evidence and rerun only database tests affected by later schema/SQL changes.
- [ ] Record remaining external gates: authenticated WhatsApp, target-volume/endurance, independent security, target-host deployment, backup/DR and operational approval.
- [ ] Create and independently verify source ZIP, Git bundle and SHA-256 manifest.
- [ ] Only after this checkpoint confirms no further reasonable backend code work remains, transition to the deferred production frontend programme.
