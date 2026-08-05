# Release 0.8.15 — Sender quarantine and health-aware allocation controls

This operational-core checkpoint adds governed sender-session quarantine, MFA-protected reinstatement, persistent quarantine evidence, heartbeat protection against accidental re-entry, capacity exclusion, and an evidence-based health assessment endpoint.

## Controls

- Dedicated quarantine and reinstatement operations with optimistic versions and meaningful reasons.
- Quarantined, restricted, and retired sessions cannot be returned to allocation by a runtime heartbeat.
- Quarantine timestamps, reasons, and reinstatement timestamps are retained.
- Quarantined sessions contribute no throughput or daily capacity.
- Health assessments score heartbeat freshness, lifecycle status, and daily-capacity consumption and return explicit allocation recommendations.
- Migration `0047_sender_session_quarantine.sql` adds database evidence and review indexes.

## Validation boundary

The assessment uses currently available platform telemetry. Provider failure ratios, latency distributions, reconnect frequency, complaints, and block evidence remain future inputs once live OpenWA telemetry is available.

## Delivery exception reconciliation

- MFA-protected operator resolution for unknown and contradictory outcomes.
- Evidence-backed confirmation of sent, delivered, read, permanent failure, or confirmed non-submission.
- Confirmed non-submission is rejected whenever a provider message identifier or acknowledgement exists.
- Canonical delivery events remain monotonic and idempotent.
- Append-only resolution evidence is stored by migration `0048_delivery_exception_resolution.sql`.
- Resolved items are removed from the operational exception queue only after canonical state and resolution evidence are persisted.
