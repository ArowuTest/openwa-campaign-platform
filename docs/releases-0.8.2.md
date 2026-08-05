# Release 0.8.2 — Programme Governance and Release Gates

This checkpoint adds the formal programme controls required to move from code development toward an evidence-based production candidate.

## Added

- Master implementation roadmap with workstreams, milestones and exit criteria.
- Production-readiness checklist covering product, security, data, messaging, infrastructure, resilience and go-live.
- Risk and technical-debt register with explicit release blockers.
- ADR register and decision triggers.
- Independent engineering audit plan.
- Machine-readable hard release gates.
- Automated governance verifier and tests.
- Make targets for governance checking and production-candidate gating.

## Important status clarification

The repository remains pre-production. A production-candidate gate intentionally fails while live PostgreSQL, genuine OpenWA, capacity, security, disaster-recovery, operational and frontend evidence remain outstanding.
