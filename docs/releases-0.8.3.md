# Release 0.8.3 — Organisation Lifecycle Governance and Audit Remediation

This checkpoint closes the first repository-audit findings with a coherent organisation-governance slice.

## Added

- Persistent organisation retrieval, profile update and lifecycle status APIs.
- Optimistic concurrency for every organisation mutation.
- Governed `ACTIVE`, `SUSPENDED`, `UNDER_REVIEW` and `CLOSED` states.
- Mandatory reasons for status changes.
- Immutable organisation lifecycle events with actor and version evidence.
- Normalised duplicate prevention for non-closed organisations.
- PostgreSQL migration `0036_organisation_lifecycle_governance.sql`.
- Exhaustive release builds for every `cmd/*` executable.
- Initial independent audit findings and traceability reconciliation.

## Known boundary

Campaign and import boundaries do not yet universally reject suspended organisations. This remains a high-severity release blocker and is explicitly recorded rather than inferred as complete.
