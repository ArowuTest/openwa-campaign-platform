# ADR-0002: Hybrid typed and configurable audience filters

- Status: Accepted
- Date: 2026-08-04

## Decision

Use indexed typed columns for high-volume core dimensions such as country, state, LGA, reported age and gender. Use a governed attribute-definition registry and typed value table for future fields. Promote popular configurable fields into dedicated columns or materialised projections after measured query analysis.

## Consequences

- New filter types can be introduced without redesigning the user interface.
- Core 10-million-profile queries remain efficient.
- Administrators cannot create arbitrary unsafe SQL; only approved types and operators are permitted.
- Every filter is permissioned, versioned and auditable.
