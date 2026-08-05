# Release 0.6.0 — Production Messaging Engine (non-live release candidate)

## Scope completed

This release completes the code-level production messaging engine boundary between the Go control plane and the retained OpenWA 0.13.0 NestJS/Node.js gateway.

- Explicit immutable OpenWA provider, engine, gateway-pool and sender-route selection.
- Governed engine-specific gateway pools and capability-aware campaign pre-flight.
- Actual supplied OpenWA source imported under `third_party/openwa/upstream` with MIT attribution and an unverified upstream commit recorded honestly.
- Restricted real OpenWA adapter for text, image, video and document submission.
- Session create, start, stop, drain, resume, logout, delete, QR and pairing-code operations.
- HMAC-signed control-plane commands with method/path/timestamp/nonce/body binding and replay protection.
- Durable gateway idempotency records. A submission left uncertain is never automatically repeated.
- One ordered bounded per-session pipeline with adaptive rate reduction and controlled recovery.
- Gateway-pool identity and capability enforcement on every send.
- OpenWA webhook signature verification and canonical sent/delivered/read/failure mapping.
- Durable gateway-local provider-event outbox with exact event-ID replay.
- Provider-message-ID reconciliation when client correlation is absent.
- Short-lived HMAC-signed internal media URLs and fail-closed media retrieval.
- Compose topology for the Go control plane, campaign worker, restricted gateway wrapper and retained OpenWA upstream service.

## Release boundary

The code and contracts are complete for controlled deployment, but this is not evidence of live WhatsApp readiness. The following gates require a Docker/VPS environment and genuine WhatsApp accounts:

1. install and dependency-resolved NestJS/OpenWA builds;
2. start the supplied OpenWA upstream container;
3. pair genuine `whatsapp-web.js` and Baileys sessions;
4. prove text and common-media sends;
5. prove signed sent/delivered/read/inbound events end to end;
6. execute migrations against live PostgreSQL;
7. test worker loss, queue recovery and session-state recovery;
8. measure sustainable per-session capacity and establish admission thresholds;
9. complete Hostinger network, backup, monitoring and restore validation.

No claim of five-million-message transport capacity or WhatsApp acceptance is made by this release.
