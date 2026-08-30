# Campaign-Scoped Sender Profile Identity Addendum

Approved conversation authority: 24 August 2026. These requirements extend the original 398-row SRS catalogue and must be added to generated traceability when the backend tranche begins. Until then they are explicitly not implemented.

| ID | Priority | Requirement | Acceptance boundary |
|---|---|---|---|
| CSI-001 | Must | Treat the genuine WhatsApp account as reusable inventory and campaign profile identity as a separate governed object. | Account metadata cannot silently satisfy or overwrite campaign identity fields. |
| CSI-002 | Must | Permit at most one non-terminal campaign identity reservation per underlying sender account. | Concurrent reservation attempts produce one winner and one conflict under PostgreSQL concurrency. |
| CSI-003 | Must | Enforce the complete profile lifecycle from `AVAILABLE` through reservation, configuration, verification, sending, draining, release and back to `AVAILABLE`. | Invalid transitions fail before provider side effects and every accepted transition is immutable evidence. |
| CSI-004 | Must | Persist requested and provider-observed display name, photo reference/hash, About text and future username separately. | A read model exposes both snapshots, comparison result, observation time and provider correlation without leaking secrets. |
| CSI-005 | Must | Require verified requested-versus-observed equivalence before `READY`. | Missing, stale, mismatched or ambiguous observation blocks readiness and campaign admission. |
| CSI-006 | Must | Version capabilities by provider, engine, adapter and evidence version for profile read/write, recipient visibility and identity switching. | Runtime self-report cannot promote a capability to `SUPPORTED_AND_VERIFIED`; activation requires governed evidence. |
| CSI-007 | Must | Rely only on `SUPPORTED_AND_VERIFIED` capabilities for hard guarantees. | Unsupported, unknown and unverified operations fail closed with an actionable reason. |
| CSI-008 | Must | Keep username operations disabled until genuine-account validation proves support, propagation, cooldown, rendering and safe switching. | A default installation cannot request or guarantee a username operation. |
| CSI-009 | Must | Prevent profile mutation while sending or draining and prevent cross-campaign identity bleed on reuse. | Provider-operation tests prove no mutation during active send; ambiguous release quarantines the account. |
| CSI-010 | Must | Make configuration and release idempotent, resumable and fenced. | Worker crash/retry cannot duplicate a mutation, skip verification or let a stale worker alter current identity. |
| CSI-011 | Must | Preserve immutable actor, reason, campaign/account binding, request fingerprint, observed fingerprint, capability version and provider evidence for every operation. | Evidence survives retries and cannot be updated or deleted through application roles. |
| CSI-012 | Must | Verify release before returning the underlying account to `AVAILABLE`. | The observed outgoing campaign identity is absent or the governed neutral/next identity is verified; otherwise the account is quarantined. |
| CSI-013 | Should | Model provider cooldowns and propagation windows as governed, effective-dated policy. | Scheduling and admission wait until configured and observed safety windows have elapsed. |
| CSI-014 | Must | Disclose that profile identity is not an alphanumeric WhatsApp sender ID and may render differently by client/provider. | API/help/reporting copy is covered by backend contract and later frontend acceptance tests. |

## Required backend seams

- Campaign identity reservation and lifecycle repository/service.
- Requested and observed profile snapshots with normalized fingerprints.
- Versioned provider/engine/adapter capability registry.
- Fenced, idempotent configuration/observation/release jobs.
- Campaign admission integration and sender allocation exclusion.
- Immutable event history and operator-facing mismatch/quarantine state.

The tranche follows durable queue reconstruction, pause/resume/cancellation, retry/reconciliation and production sender-lease wiring unless a later approved roadmap explicitly changes that dependency order.
