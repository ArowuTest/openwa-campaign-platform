# Release 0.8.19 — Runtime Pacing and Multi-Pool Capacity Enforcement

## Scope

This release turns the 0.8.18 pacing and multi-pool models into persisted, API-managed and campaign-worker-enforced operational controls.

## Sender pacing administration

- Effective-dated pacing policies are persisted in PostgreSQL.
- Draft, submit, independent approve/reject and effective-policy resolution APIs are available.
- Policy scope supports platform, provider, OpenWA engine, gateway pool, sender pool, session and campaign levels.
- Policy events are append-only.
- Provider and engine scope identifiers are stored as text so values such as `OPENWA`, `WHATSAPP_WEB_JS` and `BAILEYS` are valid.

## Runtime enforcement

Before provider submission, the campaign worker now enforces:

- minimum and maximum delay with deterministic bounded jitter;
- message-type delay overrides;
- per-session hourly allowance;
- per-session daily allowance;
- maximum simultaneous campaign assignments per session;
- durable next-send time and jitter sequence;
- exact pacing policy ID and version used by the session.

Pacing occurs before the canonical `SUBMITTING` transition, so an interrupted wait does not leave a recipient falsely marked as submitted.

## Multi-pool routing and reservations

- Campaign routing plans are persisted with all approved OpenWA pools and engine identities.
- Capacity is atomically reserved for the campaign window.
- Serializable transactions and sender-pool row locking prevent concurrent overbooking.
- Routing-plan, reservation and release APIs are available.
- Campaign admission now aggregates the latest multi-pool plan when present and retains the single-pool path for older campaigns.
- Reservations move from `HELD` to `ACTIVE` at dispatch start and are released on cancellation or terminal completion.

## Migrations

- `0053_pacing_scope_and_policy_audit.sql`
- `0054_sender_pacing_runtime_enforcement.sql`

## Validation

- `go test ./...`
- `go vet ./...`
- `go build ./...`
- OpenAPI reconciliation for 146 implemented API routes
- migration/schema validation
- pacing and routing-plan unit tests

## External validation still required

Live PostgreSQL execution, Hostinger multi-node deployment, genuine `whatsapp-web.js` and Baileys sessions, and measured pacing/capacity calibration remain release gates.
