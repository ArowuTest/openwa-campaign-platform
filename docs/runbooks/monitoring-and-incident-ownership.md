# Monitoring and incident ownership runbook

This is a local operations procedure. Monitoring exercise evidence remains external until deployed dashboards, alerts and ownership are exercised.

## Required monitoring domains
- Railway control plane and workers: readiness, error rate, queue/lease health and saturation.
- Hostinger OpenWA gateway fleet: runtime heartbeat, capacity, session state, queue depth and resource health.
- PostgreSQL: availability, connections, replication/PITR health, latency and storage growth.
- Redis: availability, memory/eviction and reconstructability alarms.
- Object storage: availability, failed writes/reads, lifecycle and restore signals.
- Direct Meta Cloud API: submission/webhook health, credential/template/sender authority and error rates.

## Ownership and escalation
1. Every accepted monitoring record must name an accountable owner and evidence reference.
2. Critical alerts must identify the affected service, environment, correlation evidence and safe containment action.
3. Campaign pause is preferred over speculative resend when provider outcome is ambiguous.
4. Gateway/session authority or cross-provider-boundary alarms require traffic isolation before remediation.
5. Incident resolution records must preserve timestamps, source fingerprint and supporting evidence.

## Exercise
- Simulate service-unavailable, stale-node, queue-growth and provider-boundary incidents.
- Verify alert delivery, acknowledgement, assignment, escalation and recovery evidence.
- Confirm no monitoring surface exposes raw MSISDNs, message bodies or secret values.

Exercise status remains `PENDING_EXTERNAL` until the deployed monitoring stack and incident process are exercised and accepted.
