# Reporting and operations architecture

Version 0.8.0 introduces an authoritative operational read model without making dashboards or exports a second source of truth.

## Principles

- Campaign, recipient, sender and incident counters are derived from PostgreSQL authoritative state.
- Delivery acceptance, sent, delivered, read, failure and unknown remain separate.
- Delivery-exception views expose internal recipient identifiers and operational metadata, never plaintext MSISDNs.
- Incidents are governed records with severity, ownership, optimistic versioning, resolution evidence and tamper-evident audit events.
- Audit search pages through the existing chained audit ledger; it does not create a parallel mutable log.
- Campaign reports are generated as point-in-time summaries from campaign and metric records.
- Exports are purpose-bound, require recent MFA, use maker-checker approval, expire after 24 hours and are audited.
- The export request is not itself evidence that a file was generated. File rendering and object-store delivery remain a subsequent controlled worker responsibility.

## Operational surfaces

1. Dashboard: campaign states, recipient states, queue depth, sender states, unknown outcomes, stale nodes and active incidents.
2. Incident queue: create, acknowledge, assign and resolve operational incidents.
3. Delivery exception queue: retryable failures, permanent failures and unknown outcomes for reconciliation.
4. Audit search: sequence-based paging over tamper-evident audit evidence.
5. Campaign report: audience, delivery, engagement and exception summary.
6. Controlled exports: request, independent approval/rejection and time-limited availability.

## Remaining live validation

The read models and controls require migration 0034 on PostgreSQL and operational load testing. PDF/XLSX rendering, object-store publication, download watermarking and expiry cleanup are deliberately not represented as complete until their worker implementation and live storage validation are delivered.
