# OpenWA Worker Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the deployed `openwa-gateway -> openwa-upstream` two-service chain with one governed OpenWA-derived worker node while preserving every required transport/session capability.

**Architecture:** `services/openwa-gateway` keeps its existing public/internal contracts and directly owns a worker-local engine registry. A deterministic sync step extracts only the transitive source closure of the pinned upstream `whatsapp-web.js` and Baileys adapters, replacing the upstream TypeORM LID store and full configuration import with narrow worker-local shims.

**Tech Stack:** NestJS 11, TypeScript 5.9, Node 22.16, whatsapp-web.js 1.34.x, Baileys 7.0.0-rc13, Docker Compose.

## Global Constraints

- Go/PostgreSQL remains authoritative for consent, audience, campaigns, approvals, delivery entitlement and long-term records.
- The worker persists only WhatsApp authentication/session files and gateway durability evidence.
- No upstream dashboard or raw upstream application is deployed.
- Upstream MIT licence/provenance remains intact under `third_party/openwa/upstream`.
- No capability may be removed merely to make health checks pass.
- Production exposes only the governed gateway boundary; raw engine internals stay private.

---### Task 1: Structural gate and deterministic retained-source slice

**Files:**
- Modify: `tests/governance/test_stack_startup.py`
- Create: `services/openwa-gateway/scripts/sync-retained-openwa.mjs`
- Create generated-at-build: `services/openwa-gateway/src/retained-openwa/**`
- Modify: `.gitignore`

**Interfaces:**
- Consumes pinned source under `third_party/openwa/upstream/src`.
- Produces the two retained adapter classes and `EngineStatus`/event interfaces inside gateway source before typecheck/build.

- [ ] Add a failing governance test asserting production Compose has no `openwa-upstream` service and the production worker does not build/serve `dashboard/`.
- [ ] Run the test and verify it fails on the current two-service topology.
- [ ] Implement a sync script starting from `engine/adapters/whatsapp-web-js.adapter.ts` and `engine/adapters/baileys.adapter.ts`, recursively copying relative imports only.
- [ ] Rewrite `lid-mapping-store.service` imports to a generated interface-only shim and `config/configuration` use to a generated `resolveNonNegativeIntEnv` shim.
- [ ] Fail sync when the source closure unexpectedly grows or a forbidden app/database import appears.
- [ ] Run structural tests and gateway typecheck to verify the retained slice is deterministic.
### Task 2: Embedded engine registry and provider

**Files:**
- Create: `services/openwa-gateway/src/provider/embedded-openwa-engine.service.ts`
- Modify: `services/openwa-gateway/src/provider/openwa.provider.ts`
- Modify: `services/openwa-gateway/src/app.module.ts`
- Modify: `services/openwa-gateway/package.json`
- Regenerate: `services/openwa-gateway/package-lock.json`
- Test: `scripts/test-gateway-durability.js` plus new provider/session tests.

**Interfaces:**
- Keeps the existing `MessagingProvider` methods unchanged.
- Registry owns `Map<sessionId, engine runtime>`; engine choice is frozen at session creation/start from `ENGINE_TYPE` until the Go control plane later supplies an explicit per-session engine version.

- [ ] Write failing tests for create/start/health/QR/pairing/stop/logout/delete and event mapping without HTTP calls to an upstream service.
- [ ] Add retained runtime dependencies using the versions from the pinned upstream package.
- [ ] Implement an engine registry that instantiates `WhatsAppWebJsAdapter` or `BaileysAdapter`, persists auth state under `/app/session-data`, and guards concurrent lifecycle transitions.
- [ ] Map adapter callbacks directly into the existing durable provider/inbound publishers using stable event IDs where provider IDs exist.
- [ ] Replace `OpenWAProvider` HTTP proxy logic with calls into the engine registry.
- [ ] Run gateway tests, typecheck and production Nest build.
### Task 3: Docker and Compose consolidation

**Files:**
- Modify: `infrastructure/docker/openwa-gateway.Dockerfile`
- Modify: `infrastructure/compose/compose.yaml`
- Modify: `infrastructure/compose/compose.production.yaml`
- Modify: production/Hostinger deployment assets that reference `openwa-upstream`.

**Interfaces:**
- One service named `openwa-gateway` owns Chromium/Baileys runtime and the persistent session volume.
- Campaign/control services continue calling `http://openwa-gateway:2785`; no caller contract changes.

- [ ] Add failing Compose assertions rejecting an `openwa-upstream` deployed service, duplicate OpenWA volume and upstream API-key hop.
- [ ] Extend the gateway image with the browser/native packages and build-time backports required by the retained adapters.
- [ ] Remove `openwa-upstream` from development and production Compose and point the single session volume at the gateway.
- [ ] Keep the worker on private/egress networks only; do not publish its port publicly.
- [ ] Validate both Compose models and build the consolidated image.
### Task 4: Security, runtime proof and release evidence

**Files:**
- Modify: `scripts/verify-node-security.py`
- Modify: `config/node-security-exceptions.json`
- Modify: `docs/releases-0.8.28.md`
- Modify: `docs/BUILD_CHECKPOINT.md`
- Modify/add: Hostinger worker deployment documentation.

**Interfaces:**
- Security audit covers admin web, governed gateway and its production dependencies only.
- Upstream dashboard source remains provenance/reference and is not a production release dependency.

- [ ] Make the security validator fail if a production dashboard exception or deployed dashboard returns.
- [ ] Remove `SEC-EXC-001` from the production exception register and verify first-party/transport npm audits.
- [ ] Recreate the Docker stack from the consolidated image and require every container to become healthy with zero restart loops.
- [ ] Probe public `/healthz`, `/readyz` and admin route, and inspect fresh application/PostgreSQL logs for errors.
- [ ] Run Go tests/vet, changed-package race tests, gateway tests/build/typecheck, OpenAPI, governance, Compose, secret scan and SBOM.
- [ ] Update release/checkpoint docs, commit the consolidation, then create and independently verify source ZIP, Git bundle and SHA-256 manifest.