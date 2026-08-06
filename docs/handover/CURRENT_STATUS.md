# Current status

## Current repository baseline

Version `0.8.27` on branch `work/backend-platform-governance` is the current completed backend checkpoint.

The platform remains an internal managed-service campaign control plane. Go and PostgreSQL own consent, audience eligibility, campaign approval, recipient obligations, canonical delivery state, privacy, configuration, retention, reporting, incidents and audit. Redis remains reconstructable execution infrastructure. OpenWA remains an isolated replaceable transport.

## Latest hardening

- Gateway pools now have draft, submission, independent approval, activation, pause/rejection and guarded retirement evidence.
- Gateway nodes can be drained, marked offline or retired through MFA-protected administration, and pools cannot retire while nodes, sessions, campaigns or capacity reservations remain active.
- Gateway runtime reports are exact-body HMAC authenticated and carry durable nonce, boot, build, configuration, capability, health and capacity evidence.
- Runtime reports fail closed on provider, engine, pool, adapter, node-version or capability drift.
- Platform configuration now uses one governed, effective-dated catalogue with canonical JSON checksums, overlap prevention, immutable supersession and rollback-by-replacement.
- Maintenance windows support scoped draining, read-only/admission freeze and emergency stop and are enforced in API writes, campaign start/resume, dispatch submission and session changes.
- Retention policy precedence prevents platform defaults from duplicating organisation-specific work.
- Retention workers use fenced leases, explicit retry availability, legal-hold checks and review holds for unsupported or destructive actions.
- Operational alert policies are effective-dated and maker-checker governed; alerts deduplicate, recover, escalate and create persisted portal notifications.
- Portal notifications are recorded as delivered when they enter the authoritative portal inbox rather than remaining indefinitely pending.
- Incident assignment and lifecycle changes produce immutable timeline events in the same PostgreSQL transaction as the incident state change; resolved/closed incidents cannot be silently reopened.
- A separate platform-governance worker runs retention scheduling/execution and alert evaluation/escalation with health endpoints and graceful shutdown.

## Traceability position

The generated catalogue currently reports:

```text
Total requirements:       398
Implemented and tested:   107
Partial:                    44
Not started:               247
```

The matrix is granular and contains frontend, repository-governance, deployment and external-validation requirements. These counts are evidence classifications, not a direct backend completion percentage.

## Honest readiness position

The next code-side package remains production engineering hardening: service observability, service-specific identities/secrets, durable transport replay/unknown reconciliation, pagination consistency, partition-ready high-growth schemas and PostgreSQL-backed failure/concurrency tests. Live infrastructure, independent assurance and frontend gates remain separately open.
