# OpenWA Worker Consolidation Design

## Purpose

Align the deployed WhatsApp transport architecture with the approved platform design: each Worker VPS runs one internally managed OpenWA-derived NestJS gateway node, not a governed gateway plus a second full upstream OpenWA application.

## Source-of-truth architecture

The approved platform flow remains:

```text
Internal Admin Portal
  -> Go Control API / workers
  -> transactional delivery ledger and durable queue
  -> Worker VPS A/B/N
  -> one OpenWA node per worker VPS
  -> WhatsApp sessions
  -> signed event ingestion
  -> metrics/reporting
```

The consolidation changes only what is inside the `OpenWA node` box; it does not change the business architecture.
## Deployed worker node

A deployed worker node will contain:

- the existing governed `services/openwa-gateway` API and security boundary;
- retained OpenWA transport/session components required for pairing, persistence and engine operation;
- `whatsapp-web.js` and Baileys provider adapters;
- text, image, video and document submission;
- QR and pairing-code workflows;
- session lifecycle, health, ownership, fencing and drain controls;
- durable idempotency/replay protection;
- signed delivery/read/failure/inbound event publishing;
- one persistent session-state volume owned by that worker node.

It must not contain campaign eligibility, consent, audience selection, billing, reporting or long-term delivery state. Those remain in Go/PostgreSQL.

## Upstream source relationship

`third_party/openwa/upstream` remains a pinned source/provenance reference with its MIT licence, notices and exact upstream history. It is not deployed as a second application.

Upstream changes are reviewed and selectively ported into the governed transport node; they are never automatically merged into production.
## Production exclusions

The production worker image must not build, copy or serve the upstream OpenWA dashboard. It must not expose raw upstream administration routes through nginx or a public host port.

The dashboard React/Vite tree may remain in the pinned provenance source, but it is outside the production dependency and vulnerability gate. `SEC-EXC-001` is therefore removed from the production release path rather than approved as a waiver.

## Hostinger topology

Pilot:

- control-plane VPS for portal/API/scheduler/reporting;
- data-plane VPS for PostgreSQL/Redis/backups where appropriate;
- separate OpenWA worker VPSs with stable IPs;
- initially 1-3 sessions per worker, subject to measured resource and reliability evidence.

Production expands to redundant application/API nodes, PostgreSQL primary/replica plus PgBouncer, Redis/queue, monitoring/logging, object storage/backups, and multiple isolated OpenWA worker groups.

Only 80/443 are public at the application edge. Worker transport APIs, PostgreSQL, Redis and storage stay on private/firewalled paths.
## Migration approach

The consolidation is incremental and test-first. First add structural tests that fail while two deployed Node services exist. Then move only the transport/session functionality required by the governed gateway, switch its provider from HTTP proxying to an in-process adapter, remove the `openwa-upstream` Compose service and duplicate volume, and rebuild the whole stack.

No business feature is removed to obtain a green build. Required session, pairing, send, media, health and event behaviours must have executable contract coverage before the second service is removed.

## Acceptance criteria

- production Compose contains one OpenWA-derived deployed Node service per worker node;
- `services/openwa-gateway` owns the retained WhatsApp engines directly;
- no production image builds or serves the upstream dashboard;
- no production security exception exists for the dashboard React Router advisory;
- session create/start/stop/logout/delete, QR/pairing, text/media send, health and inbound/delivery events remain covered;
- gateway durability, ownership/fencing and signed-event tests remain green;
- Docker image builds and the complete local Compose stack becomes healthy with zero restart loops;
- Go tests/vet, Node tests/builds, OpenAPI, Compose security, secret scan and SBOM gates pass;
- Hostinger deployment documentation describes the same single-node worker topology.