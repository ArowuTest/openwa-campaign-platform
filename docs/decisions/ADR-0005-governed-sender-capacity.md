# ADR-0005: Govern sender inventory and capacity in the control plane

## Decision
Sender pools, worker nodes, genuine WhatsApp sessions, lifecycle state, measured limits and capacity reservations are authoritative control-plane records. Gateway workers report heartbeats and measured capacity but cannot enlarge their own approved pool or daily entitlement.

## Rationale
This preserves SND-001 through SND-018 principles: real sender identity, deterministic ownership, measured capacity, explicit reservations, bounded in-flight work and audited changes. It also prevents a transport adapter from becoming the source of truth for campaign admission.

## Consequences
All administrative mutations use optimistic versions and immutable governance events. Capacity calculations exclude stale or unhealthy sessions and subtract reserved capacity. Actual OpenWA pairing and transport remain a separate provider-runtime deliverable.
