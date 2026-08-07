# Build checkpoint

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
