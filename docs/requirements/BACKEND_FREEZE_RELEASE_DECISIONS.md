# Backend Freeze Release Decisions

**Decision date:** 1 October 2026  
**Release boundary:** first internal OpenWA Campaign Platform production release

This record closes the deliberate release-scope decisions required before backend freeze. It does not delete or supersede the underlying requirements.

## EVT-012 — scheduled reconciliation

**Decision:** keep `PARTIAL`; do not add provider-status polling for every transport before backend freeze.

The backend already runs scheduled reconciliation for stale `SUBMITTING`, `GATEWAY_ACCEPTED`, `SENT` and `UNKNOWN` records, preserves sticky UNKNOWN/no unsafe resend, applies configured final-UNKNOWN aging, and supports governed manual evidence resolution.

For release one, callback/event evidence plus scheduled internal reconciliation and manual operator resolution is the approved operating posture. Provider-specific polling may be added later where a provider exposes a reliable status API and an evidence-backed need is demonstrated.

This is not a backend release blocker.

## EVT-013 — provisional versus final UNKNOWN

**Decision:** keep `PARTIAL`; do not create a second public status taxonomy before backend freeze.

UNKNOWN remains sticky from the first ambiguous outcome. The configured final-UNKNOWN window adds aging/evidence without changing the safe-send semantics or enabling resend.

For the internal operator console, the required release-one distinction is operational evidence/age/reconciliation state, not a second recipient status that could be misread as permission to retry.

This is not a backend release blocker.

## ADM-003 / ADM-012 — retry, reconciliation and UNKNOWN policy governance

**Decision:** keep both `PARTIAL`; move the remaining governance refinement to the production infrastructure/operations policy phase.

Retry, reconciliation and UNKNOWN windows are bounded and validated deployment configuration today. The remaining gap is consolidating them into one approved, effective-dated maker-checker policy record consumed at runtime.

Release one will freeze the approved production values through deployment configuration, secrets/config change control and release evidence. A later governance enhancement can move those values into the platform-policy store without changing the already-proven retry/UNKNOWN semantics.

These are governance-quality refinements, not missing backend behavior.

## CSI-001 .. CSI-014 — campaign-scoped sender profile identity

**Decision:** explicitly defer the CSI tranche from release one while retaining ADR-0006 and the Campaign-Scoped Sender Identity Addendum as accepted future backend authority.

Release one uses genuine governed WhatsApp sender accounts/sessions as inventory and does not expose campaign-specific profile-name/photo/About/username mutation as a launch requirement. Operators select governed sender accounts/pools/sessions; the platform does not promise a campaign-specific WhatsApp profile identity or alphanumeric sender ID.

Therefore release one does not require reservation/configuration/verification/release machinery for mutable campaign profile identity. No CSI requirement is marked implemented by this decision.

If campaign-scoped profile identity is enabled later, implementation must follow ADR-0006 and CSI-001..CSI-014 in full, including exclusive reservation, requested-versus-observed evidence, capability verification, fenced configuration/release, quarantine on ambiguity and disclosure requirements.

## Backend-freeze consequence

The remaining backend release blocker after this decision is a composed internal campaign workflow acceptance over the implemented release-one features. Infrastructure, live-provider, capacity/load, backup/DR, operations and frontend evidence remain later release phases and must not be misclassified as backend implementation gaps.
