# Independent Engineering Audit Plan

## Objective

Perform a repository-wide, evidence-based assessment before production deployment. The audit must identify defects and gaps; it is not a confirmation exercise.

## Audit domains

### Architecture

- Validate service boundaries and dependency direction.
- Confirm PostgreSQL remains authoritative for obligations and canonical state.
- Identify duplicated or browser-only business rules.
- Review failure domains and coupling between control plane and OpenWA.

### Security

- Threat-model identities, admin access, imports, exports, gateway commands and callbacks.
- Review cryptography, key rotation, secret handling and PII exposure.
- Test RBAC coverage and maker-checker bypass resistance.
- Review dependency, container and upstream OpenWA supply-chain controls.

### Data and database

- Review schema integrity, constraints, indexes, partition strategy and migrations.
- Confirm consent, withdrawal and suppression precedence.
- Inspect retention, legal hold, deletion and audit immutability.
- Review large-query plans with production-scale synthetic data.

### Messaging and reconciliation

- Review idempotency before provider submission.
- Exercise crash-before-send, crash-after-send and unknown-outcome paths.
- Confirm no automatic cross-session/provider resend after ambiguity.
- Validate event deduplication and monotonic canonical state transitions.

### Reliability and operations

- Test queue reconstruction, worker fencing, lease expiry and graceful drain.
- Review readiness/health semantics and maintenance behaviour.
- Execute backup, restore, failover and disaster-recovery drills.
- Validate monitoring and alert coverage against runbooks.

### Frontend and accessibility

- Map UI/UX identifiers to implementation routes and tests.
- Verify masking, permission-aware actions and high-risk confirmations.
- Test loading, empty, degraded, unknown and recovery states.
- Assess WCAG 2.2 AA keyboard and screen-reader journeys.

## Finding format

Every finding records:

- unique ID;
- domain;
- affected requirement(s);
- severity and exploit/impact rationale;
- reproducible evidence;
- recommended remediation;
- accountable owner;
- target milestone;
- verification evidence;
- closure approval.

## Severity

- Critical: likely severe data, consent, duplicate-send, security or availability impact; immediate release blocker.
- High: material control failure or likely production outage; release blocker.
- Medium: important weakness with compensating controls possible.
- Low: maintainability, clarity or minor operational improvement.

## Exit criteria

- No open Critical findings.
- No open High findings without formally approved, time-bounded risk acceptance.
- Release-gate evidence is complete.
- Traceability accurately reflects the audited implementation.
