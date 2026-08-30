# Backup and restore rehearsal runbook

This defines the recovery exercise contract. It does **not** close RG-006; measured recovery evidence is still blocked on target infrastructure.

## Recovery scopes
- PostgreSQL: encrypted backup plus WAL/PITR to a selected recovery timestamp.
- Object storage: governed message/media, consent evidence, reports and required metadata.
- OpenWA session state: restore without creating dual ownership or bypassing session authority fencing.

## Rehearsal sequence
1. Record source fingerprint, environment, backup identifier and intended RPO/RTO targets before the exercise.
2. Isolate writes or use the approved recovery environment so the exercise cannot corrupt authoritative production state.
3. Restore PostgreSQL, validate migrations/schema, service-role isolation and authoritative ledger integrity.
4. Restore representative object-storage material and verify checksums, access controls and lifecycle policy.
5. Recover an OpenWA node/session with only one active owner; prove stale authority cannot submit.
6. Reconcile campaign/delivery/UNKNOWN evidence before allowing traffic.
7. Measure actual RPO and RTO for every required recovery scope.

## Acceptance evidence
- Named operator/owner and approved environment.
- Evidence reference, timestamps and exact source/config fingerprint.
- Measured non-negative RPO/RTO and comparison against agreed targets.
- Findings and remediation evidence for any failed integrity or authority check.

Recovery records remain `BLOCKED_EXTERNAL` until this exercise is executed on production-equivalent infrastructure and accepted.
