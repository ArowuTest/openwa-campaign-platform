# Build checkpoint

## 18 August 2026 — I3 post-I2-remediation pre-review checkpoint

The I2 blind council was diagnostic rather than freeze authority because all four usable reviewers found reachable production-boundary defects. Independent adjudication CONFIRMED four distinct issues: a Docker-local HTTP media URL crossing Railway→Hostinger; production runtime registration able to advertise Docker-local `openwa-gateway`; governed selected-node dispatch able to fall back to the legacy static router when node URL was absent; and platform-governance S3 startup incorrectly requiring filesystem `OBJECT_STORE_ROOT`. All four were reproduced RED and remediated as one coherent I2→I3 batch without changing the approved three-sibling transport model or the SUBMITTING/UNKNOWN boundary.

I3 now requires explicit HTTPS cross-provider media configuration; production gateway runtime registration requires a governed `GATEWAY_INTERNAL_URL`; selected-node routes fail closed without their own destination and cannot silently use the static compatibility router; and platform-governance object-store validation is driver-aware so S3/MinIO does not require a fake filesystem root. Meta remains direct, Railway remains exactly seven backend/control services, and Hostinger remains OpenWA-gateway-only.

Fresh exact-candidate evidence is GREEN: governance 52/52; production security structure 7 services/25 secrets; split topology; production control-plane Compose render; resolved Hostinger BAILEYS and WHATSAPP_WEB_JS render/preflight; OpenAPI 294/294; candidate secret scan; strict Node security with zero production exceptions/vulnerabilities; real isolated current-source gateway `npm ci --ignore-scripts → tsc --noEmit`; Linux gateway control security 9/9, durable outbox 9/9, session authority 3/3, drain/lifecycle 18/18 plus lifecycle serialization; and `git diff --check`. A direct Windows-only directory-fsync probe reproduces `EPERM`, while the unchanged outbox durability suite passes 9/9 on Linux, so that host-only failure is verifier-environment evidence rather than a source defect. The exact full Go `go test -count=1 ./... → go vet ./... → go build ./...` gate is GREEN with marker `I3_EXACT_GO_GREEN`.

Traceability remains conservatively unchanged at **398 total: 168 IMPLEMENTED_TESTED / 181 PARTIAL / 32 NOT_STARTED / 17 BLOCKED_EXTERNAL**. All eight formal release hard gates remain OPEN/BLOCKED_EXTERNAL. Current source/config fingerprint excluding documentation is tracked `3a8f5ab26faccb6ac6b21e0b2e1daea88e536849400a497607f81bad74bdf14b` / untracked `8e7af7541650f946aae0d1a4165e77786596541672a61ee0967c250ca2f18773` across 189 non-document untracked files. Documentation reconciliation does not alter that fingerprint. **I3 is locally re-gated but is not a new freeze authority until the fresh exact-source four-model blind council is independently adjudicated.**

## 17 August 2026 — I2 node-addressed OpenWA + split-production topology addendum

R19 remains historical backend review evidence, but its local source freeze was deliberately reopened during the first infrastructure council because the approved multi-node Hostinger topology exposed a reachable production defect: frozen authority selected a concrete `GatewayNodeID`, while the OpenWA network client still submitted to one static `OPENWA_GATEWAY_URL`. The platform could therefore authorize node B but send to node A/static routing. The infrastructure I1 review is diagnostic evidence only after the resulting source/config changes; it is not freeze authority.

The remediation preserves the existing authority model rather than introducing a second router. Before SUBMITTING, governed PostgreSQL route validation now carries the current `sender_nodes.internal_url` for the exact fenced node/session; campaign dispatch and controlled test sends propagate that destination into `GatewayRequest`; `HTTPGateway` uses the request-specific node URL with the legacy static URL only as a compatibility fallback. Campaign-worker OpenWA configuration may therefore be command-secret + governed node-addressed routing without a static gateway URL.

Real PostgreSQL proof also exposed and closed three pre-existing `ValidateTestRoute` defects: the route timestamp parameter is explicitly `timestamptz`; the legacy text gateway-pool comparison explicitly casts the UUID parameter to text; and optional `minimum_gateway_version` is checked only when present, matching the other governed provider paths. `TestPostgreSQLGovernedRouteCarriesExactNodeURL` is GREEN on fresh 0001→0089 PostgreSQL 17 and PostgreSQL 18 databases. The affected dispatch/test-message/config/campaign-worker packages are GREEN normally and under `-race`.

The production deployment reference is now consistent with the approved split: `infrastructure/compose/compose.production.yaml` contains exactly the seven Railway backend/control services and no `openwa-gateway` or static `OPENWA_GATEWAY_URL`; Hostinger OpenWA remains isolated in `compose.hostinger-openwa-gateway.yaml`; direct Meta remains a sibling path. Public control-api reference configuration requires `ALLOWED_NETWORK_CIDRS`, `TRUSTED_PROXY_CIDRS` and ClamAV. The hardened production verifier reads the canonical deployment topology and rejects a reintroduced gateway.

Fresh I2 local evidence is GREEN: 50/50 governance tests; production Compose validation at 7 services / 25 secrets; split-topology verification; Docker Compose rendering of the Railway control-plane reference; BAILEYS and WHATSAPP_WEB_JS Hostinger render + resolved preflight; OpenAPI 294/294; committed-secret scan; Node security with no production exceptions; gateway TypeScript syntax/durability/control-plane/outbox/session-authority/drain/lifecycle suites; `git diff --check`; and exact read-only current-source `go test -count=1 ./... → go vet ./... → go build ./...` with marker `I2_EXACT_GO_GREEN`. All eight release hard gates remain visibly open/blocked where appropriate. No Railway/Hostinger resource, live WhatsApp session, provider credential, DNS/firewall, commit, push, deployment or frontend was mutated.

Pre-review source/config fingerprint (documentation excluded): tracked diff `bdaebba1e8d92ac858e642ca4ea9b07410d01d39a20f3bfe9bdaeb8879d63fa1`; untracked content `3cfdd21cb240c82a0eff0b663977731d0513495b9cdda82af98b7bf71caa8d8f` across 189 non-document untracked files. **I2 is fully locally re-gated but is not yet a new freeze authority. Fresh blind review of the exact post-reconciliation candidate is mandatory.**

## 17 August 2026 — superseding local backend candidate addendum

The 0.8.28 text below is retained as historical checkpoint evidence. The current candidate remains on `work/backend-production-engineering` at committed HEAD `786451d5616f5e179903fd9b61c5e26f6de76f77` with substantial intentional uncommitted backend/Meta work. Do not reset, clean or stash it.

Current schema chain is **0001→0089**. Fresh PostgreSQL 17 and PostgreSQL 18 databases both replayed all 89 migrations from zero, and the R17 PostgreSQL regressions passed on both. Migration 0089 fails closed against DELETE of live HELD/ACTIVE capacity reservations while retaining RELEASED tombstone evidence.

Current generated traceability remains **398 total: 168 IMPLEMENTED_TESTED, 181 PARTIAL, 32 NOT_STARTED, 17 BLOCKED_EXTERNAL**. The original catalogue predates first-class Meta; the 11-Aug Meta design and 15-Aug FREE_FORM conversation-window plan are later approved requirement authority where they supersede the original provider assumptions.

R18 produced two confirmed routing-capacity defects; both were reproduced RED, fixed minimally and are GREEN on PostgreSQL 17 and 18. Fresh blind R19 then reviewed the exact post-R18-remediation candidate (tracked diff `77e51bf5e15e41ee7cc398cd02d6e9f0b749c0c844e61743528495baed0ecad8`, untracked content `7bbf7b68d621a57fffd637a8507fc906e29cc1ed0283f7e369cec0ee6080385b`). Surfaces A, C and D each received independent Grok 4.6 + Qwen PASS evidence. Surface B received Qwen + Gemini PASS evidence while Grok alleged that daily buckets could follow a non-UTC PostgreSQL session. Independent adjudication rejected that claim: production control-api/campaign-worker connections use the platform libpq driver, every physical connection executes `SET TIME ZONE 'UTC'`, and the repository contains no later timezone mutation, so the claimed America/New_York sequence is unreachable. No R19 source defect survives adjudication. Provider content-filter/length failures are recorded as unavailable reviewer evidence, not programme blockers. The local backend source freeze is therefore established at R19.

Programme sequence is now: **R19 local backend source freeze → infrastructure/platform engineering and production-like proof → separately authorised Railway + Hostinger deployment / real-provider validation → production Next.js frontend → UAT/accessibility/release**. “Code exists” remains distinct from live production acceptance, and no live infrastructure mutation is implied by the local freeze.


Current version: **0.8.28**

Branch: `work/backend-production-engineering`

Implementation commit: `c70956a` (`feat: consolidate OpenWA worker and harden runtime`)

Checkpoint type: **backend production-engineering / consolidated OpenWA runtime checkpoint**

## What this checkpoint proves

This checkpoint preserves the 0.8.28 backend production-engineering work and the OpenWA worker consolidation. It is not a production deployment certificate.

The active development Docker project `openwa0828active` runs 13 containers: PostgreSQL, Redis, ClamAV, one consolidated OpenWA gateway, control API, admin web, nginx, and the six Go worker services (audience, campaign, export, inbound-governance, metrics and platform-governance).

At the post-fix observation point all 13 were healthy. The recreated final gateway image was healthy with zero restarts, ran non-root, exposed no direct host port, included Chromium and compiled gateway code, and did not contain the upstream OpenWA dashboard/server application.

The public UI, `/healthz` and `/readyz` returned HTTP 200 and a fresh application-log scan found no errors in the observation window.

## Capability-preservation rule

The 0.8.28 implementation was corrected without deleting required platform features merely to compile.

- The pre- and post-consolidation `OpenWAProvider` expose the same ten provider operations.
- Session create/start/stop/logout/delete, read/health, QR and pairing-code flows remain.
- Gateway drain/resume remains.
- Text, image, video and document sends remain.
- Inbound messages and delivery/read events remain wired to durable platform callbacks/outboxes.
- Both retained `WHATSAPP_WEB_JS` and `BAILEYS` engine paths remain available.
- The liveness watchdog was restored into the embedded runtime rather than dropped.
- SSRF protection remains enabled; only the exact `control-api` hostname is allowlisted for signed internal media retrieval.

The only tracked file deleted by the consolidation implementation is the obsolete `infrastructure/compose/openwa-upstream-secret-entrypoint.sh`, which served the removed separate upstream container and has no remaining references. Its transport capability is now served inside the consolidated gateway.

## Material corrections

- PostgreSQL startup now retries bounded transient availability failures rather than failing immediately during valid dependency warm-up.
- Docker/Desktop read-only runtime secrets are recognised through actual write-denial semantics while unsafe ordinary deployed files remain rejected.
- OpenWA media loading accepts only the governed `control-api` internal hostname while other private hosts remain blocked.
- Silent READY-engine failure is actively detected through bounded liveness probes and fenced recovery.
- The separate upstream OpenWA server/dashboard is removed from the production runtime topology; retained audited transport source is built into the isolated gateway instead.
- Node security verification now passes strict mode with no production exceptions.
- First-party gateway production dependencies audit with zero known vulnerabilities.

## Verification

Fresh evidence includes:

- clean gateway TypeScript typecheck;
- final gateway Docker image build;
- embedded OpenWA lifecycle/event/watchdog tests;
- gateway durability tests;
- Node secret-file tests;
- 12 stack-governance/release-readiness tests;
- strict Node security validation with no exceptions;
- production Compose validation (8 services / 24 secret definitions);
- OpenAPI parity for 277 implemented `/api/v1` method/path pairs;
- committed-secret scan;
- `go vet ./...`;
- race tests for the changed database/envfile packages;
- explicit deterministic Go test pass across all 55 tracked root Go package directories plus the nested supplied OpenWA Go SDK.

## Database evidence

The Neon validation branch `openwa-0828-validation` (`br-green-pine-aykv09eo`) remains present. The earlier 0.8.28 migration/concurrency evidence through migration `0066` remains applicable because the consolidation commit does not change migrations or schema contracts.

## Not yet production-certified

The requirements catalogue still contains 398 granular rows (107 implemented/tested, 44 partial, 247 not started), including frontend, repository, deployment and external-validation requirements.

Remaining hard gates include requirements/evidence reconciliation, live authenticated OpenWA validation, target-volume capacity/endurance, independent security assurance, target-host deployment, backup/restore and DR proof, operational readiness/approval and frontend completion.
