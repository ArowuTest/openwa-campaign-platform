# Architecture Decision Record Register

| ADR | Decision | Status | Scope |
|---|---|---|---|
| [ADR-0001](ADR-0001-service-boundaries.md) | Service boundaries and authoritative data ownership | Accepted | Go control plane, PostgreSQL, Redis and OpenWA isolation |
| [ADR-0002](ADR-0002-extensible-filter-registry.md) | Hybrid governed audience-filter registry | Accepted | High-scale typed filters and configurable attributes |
| [ADR-0003](ADR-0003-designer-html-as-reference.md) | Designer HTML is reference material, not production code | Accepted | Next.js implementation, accessibility and safeguards |
| [ADR-0004](ADR-0004-bounded-import-preview.md) | Large import preview is bounded and streaming | Accepted | Memory safety and import UX |
| [ADR-0005](ADR-0005-governed-sender-capacity.md) | Sender capacity is governed and evidence-based | Accepted | Admission, forecasting and session safety |
| [ADR-0006](ADR-0006-campaign-scoped-sender-profile-identity.md) | Campaign-scoped WhatsApp profile identity is governed separately from the reusable account | Accepted | Reservation, profile verification, capability evidence and safe account reuse |

## Required ADR triggers

Create a new ADR before implementing a material change to:

- authoritative data ownership;
- provider or engine routing;
- consent or suppression precedence;
- encryption, key management or identity scope;
- queue durability or idempotency;
- session ownership and recovery;
- large-table partitioning or retention;
- infrastructure topology or disaster recovery;
- external-user scope;
- security trust boundaries.

## ADR lifecycle

`Proposed → Accepted → Superseded` or `Rejected`.

An accepted ADR is not silently edited to reverse the decision. A new ADR supersedes it and records the migration impact.
