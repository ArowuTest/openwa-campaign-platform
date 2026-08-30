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
- **Railway** hosts the seven Go control-plane/API/worker services, **Hostinger VPS** hosts isolated OpenWA gateways, and **Meta Cloud** remains a direct sibling transport rather than passing through OpenWA.

## Current development checkpoint

Version `0.8.28` is the current backend production-engineering checkpoint on branch `work/backend-production-engineering`.

Accepted committed authority is HEAD `887e577565de22642cf4041a223835d122e6ff15`. The intentional dirty I4 candidate is being completed as one substantial production-hardening tranche on the engineering branch. Fresh19 passed its complete local matrix but was rejected after a valid eight-lane council; the reproduced findings are being remediated together as Fresh20. Nothing in the candidate is staged, accepted, merged, pushed, or deployed. The detailed live authority is [`docs/handover/AGENT_PICKUP.md`](docs/handover/AGENT_PICKUP.md).

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

The `openwa0828active` 13-container Docker snapshot below is retained as historical 0.8.28 development evidence. It is not the current production-topology authority.

The latest complete exact frozen evidence is Fresh19: governance 141/141; seven Railway services / 25 secret classes; OpenAPI 294/294; full Go test/race/vet/build; fresh PostgreSQL 17.10 with three migration histories through 0090 and the 40-DSN repository matrix; gateway security 32/32 plus 38 retained durability/lifecycle tests; and a 30-component / eight-container SBOM. Its council then confirmed further defects, so those green results are historical evidence rather than acceptance. Fresh20 focused regressions are green, but its full exact-source matrix, double freeze, council, and adjudication remain mandatory.

At the post-fix runtime snapshot:

- all 13 containers were healthy;
- the recreated final gateway had zero restarts;
- the gateway ran as non-root and exposed no direct host port;
- the gateway image contained Chromium and compiled transport code but no upstream dashboard/server application;
- the public admin UI returned HTTP 200;
- public `/healthz` and `/readyz` both returned HTTP 200;
- no fresh application or PostgreSQL errors were present in the observation window.

Historical 0.8.28 source verification included:

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

Fresh19 PostgreSQL 17.10 evidence covers current, pre-0085, and pre-metadata histories through migration `0090` plus the complete 40-DSN native-libpq repository matrix. Fresh20 changes runtime rejection transaction behavior and adds a PostgreSQL version-conflict DRAINING regression; it is not accepted until the fresh migration histories and complete PostgreSQL matrix are repeated on the exact Fresh20 executable source.

## Release gates still open

The original granular requirements catalogue contains 398 rows: 168 `IMPLEMENTED_TESTED`, 181 `PARTIAL`, 32 `NOT_STARTED`, and 17 `BLOCKED_EXTERNAL`. Later approved Meta, FREE_FORM and campaign-scoped sender-identity addenda are tracked separately until their implementation tranches update generated traceability. These totals include frontend, repository-governance, deployment, operational and external-validation requirements and are not a backend completion percentage.

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

Production topology assets are `infrastructure/compose/compose.production.yaml`, `infrastructure/compose/compose.hostinger-openwa-gateway.yaml`, `infrastructure/railway/service-contracts.json`, and `config/deployment-topology.json`. Live authenticated WhatsApp, target-host, capacity and disaster-recovery validation must be performed only in authorised environments that expose those dependencies.

## 24 August 2026 — I4 Fresh18 sizeable remediation and pre-council boundary

Fresh16 and Fresh17 were sizeable exact-source council boundaries, not file-by-file reviews. Fresh16 confirmed four defects that Fresh17 remediated: atomic nonce/rejection persistence, durable governed-pool rejection anchoring, gateway/control hostname separation, healthcheck-scoped Compose verification, and observed private-bind evidence. Fresh17 then confirmed three remaining defects: wall-clock runtime causality allowed a retired boot to reclaim service and fenced healthy clock corrections; invalid production capacity could be swallowed by deferred registration; and production gate-closure metadata was not bound to the actual candidate evidence artifact.

Fresh18 remediates those findings as one engineering chunk. Signed runtime reports now carry a positive per-boot `runtimeSequence`; memory and PostgreSQL accept only a newer same-boot sequence, retain accepted boot history, reject every state from a retired boot, keep same-boot DRAINING terminal, and treat `observedAt` as freshness evidence rather than a causal clock. Production gateway startup validates capacity before deferred publication. Production-candidate release verification now requires safe local evidence paths, SHA-256 byte binding, schema-versioned JSON manifests bound to the exact gate and candidate fingerprint, non-empty observed evidence, and independent owner/approver identity. Development-mode structural checks and honestly open gates remain unaffected.

Fresh18 exact-source evidence is GREEN: **2,384 files / 0 byte mismatches**; governance **137/137**; Go ordinary, full race, vet and build; fresh PostgreSQL 17 with three histories through migration 0090 and the full **40-DSN** suite; gateway TypeScript/build, control-plane security **30/30**, and **38** retained durability/lifecycle tests; OpenAPI **294/294**; production Compose **7 Railway services / 25 secrets**; resolved Hostinger boundary for both BAILEYS and WHATSAPP_WEB_JS; committed-secret scan; Node audits **0/0 vulnerabilities**; strict CycloneDX **30 components / 8 images** with SHA-256 `84257faddf0d708bf0a9e0b0b95d897fdc5ef40a922b1ff4225aee8fcf1be7c3`. Production-candidate validation correctly exits 2 because all eight release gates remain OPEN or BLOCKED_EXTERNAL.

The campaign-scoped sender-name authority remains ADR-0006 plus `CAMPAIGN_SCOPED_SENDER_IDENTITY_ADDENDUM.md`: reusable account identity is distinct from exclusive campaign reservation and requested-versus-observed sender profile identity. No live deployment, provider traffic, production credential/network mutation, capacity proof, DR exercise, penetration test, operational exercise, or frontend completion is claimed. Fresh18 must now be frozen twice and reviewed once as this complete 54-path chunk; no staging or local acceptance commit is permitted before council adjudication.
