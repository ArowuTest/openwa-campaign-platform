# Current status

## Current repository baseline

Version `0.8.28` on branch `work/backend-production-engineering` is the current backend production-engineering baseline.

Key recent commits:

- `6b668dc` — OpenWA worker-consolidation design;
- `c70956a` — verified implementation: consolidated OpenWA worker plus runtime hardening.

The platform remains an internal managed-service campaign control plane. Go and PostgreSQL own consent, audience eligibility, campaign approval, recipient obligations, sender/session governance, canonical delivery state, privacy, configuration, retention, reporting, incidents and audit. Redis remains reconstructable execution infrastructure. OpenWA remains an isolated, replaceable transport boundary.

## Current OpenWA topology

Production Compose no longer requires a separate upstream OpenWA server/dashboard container plus adapter. The transport runtime is consolidated into one isolated `openwa-gateway` worker.

The supplied OpenWA source remains preserved under `third_party/openwa/upstream/`. At gateway build/typecheck time an explicit audited transport subset is synchronised into generated `services/openwa-gateway/src/retained-openwa/` source and compiled into the gateway. The upstream dashboard/server application is not shipped.

This consolidation does **not** intentionally remove transport functionality. A direct before/after provider-method comparison is identical: send, health, get/create/start/stop/logout/delete session, QR and pairing code remain. Drain/resume and text/image/video/document send behaviour also remain.

## Latest hardening

- PostgreSQL startup uses bounded retry/backoff for transient dependency availability while retaining fail-closed behaviour for permanent configuration/schema errors.
- Docker/Desktop `/run/secrets/*` mounts are validated using actual write denial when translated POSIX mode bits are misleading; ordinary deployed secret files still require restrictive permissions.
- Retained OpenWA media loading keeps SSRF protection enabled and narrowly allowlists only `control-api` for the platform's signed internal media URL.
- An active liveness watchdog now detects silently wedged READY engines after repeated failed probes and performs fenced teardown/recovery; ACTION_REQUIRED remains observe-only and stale probe results cannot act on replacement engines.
- First-party Node security now passes strict validation with no production exceptions.
- The final consolidated gateway image builds successfully, audits with zero known production dependency vulnerabilities, runs non-root and does not contain the upstream dashboard/server application.
- The active development stack contains 13 healthy containers; the UI, `/healthz` and `/readyz` return HTTP 200.

## Verification position

Fresh 0.8.28 evidence includes:

- gateway TypeScript typecheck;
- final gateway Docker build;
- embedded OpenWA lifecycle/event/watchdog tests;
- gateway durability tests;
- Node secret-file tests;
- stack/release governance tests;
- strict Node security validation;
- production Compose validation;
- OpenAPI parity for 277 implemented `/api/v1` method/path pairs;
- committed-secret scan;
- `go vet ./...`;
- race detection for the changed Go packages;
- deterministic package-by-package Go testing across all 55 tracked root package directories and the nested supplied OpenWA Go SDK.

The isolated Neon branch `openwa-0828-validation` remains present. Existing 0.8.28 migration/concurrency evidence through migration `0066` remains applicable because the latest consolidation does not modify migrations or schema contracts.

## Traceability position

The generated requirements catalogue currently reports:

```text
Total requirements:       398
Implemented and tested:   107
Partial:                    44
Not started:               247
```

The catalogue deliberately includes frontend, repository-governance, deployment, operational and external-validation requirements. Those counts are evidence classifications, not a backend completion percentage and have not been artificially increased because the runtime is green.

## Honest readiness position

The code/runtime baseline is materially stronger and the identified consolidation defects have been corrected without deleting required features merely to compile.

The release is still not production-certified. Hard gates remain around full requirements/evidence reconciliation, live authenticated WhatsApp sessions, target-volume performance/endurance, independent security assurance, backup/restore and DR, target-host deployment, operational approval and frontend completion.

The next step is to finish the backend closure/evidence review and address any genuine remaining code findings before moving into frontend implementation.
