# ADR-0001: Go control plane with isolated NestJS/OpenWA gateway

- Status: Accepted
- Date: 2026-08-04

## Decision

Use Go for the authoritative platform backend and asynchronous workers. Retain OpenWA in a separate NestJS/Node.js gateway because its WhatsApp engines are Node ecosystem dependencies. Use Next.js only for the internal frontend and minimal presentation-layer server functions.

## Consequences

- OpenWA can be replaced without rebuilding the campaign platform.
- Business rules exist once, in Go.
- Gateway instability cannot directly corrupt consent or campaign state.
- Cross-service contracts must be explicitly versioned and idempotent.
