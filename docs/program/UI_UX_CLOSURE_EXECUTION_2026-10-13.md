# OpenWA — Day-1 operator UI closure plan
**Reconciled:** 13 October 2026 | **Baseline:** GitHub main `25638554` (inbound PR #3 merged; CI-gated Railway rollout separately verified) | **Current UI candidate:** Alert Centre

## Product boundary

The goal is to complete the internal operator product, not expand the backend. The reference specification defines **67 screen/workflow IDs**, not 67 separate Next.js URLs. Several screens are intentionally sections or states inside a single campaign, organisation, consent or operations workspace.

The matrix `UI_UX_IMPLEMENTATION_MATRIX.md` was reconciled with the current frontend and deployed source. Historical "missing" labels for user administration, consent maker/checker, audit, sender pairing and organisation status were wrong. The verified state is:

| Evidence label | Screen IDs | Meaning |
|---|---:|---|
| E — bounded source accepted | 11 | Shipped source slice and acceptance tests exist; full operational UAT is still independent |
| P — partly implemented | 37 | Working source-backed UI exists; specified states/flows or acceptance evidence remain open |
| M — missing | 17 | No comparable Day-1 operator screen/workflow yet |
| C — isolated candidate | 2 | Scoped suppression-management work and Alert Centre candidate; neither implies full UAT |

These categories **do not** establish a product completion percentage, WCAG certification, provider UAT or performance certification. They track source-backed coverage only; refresh them after every accepted `main` release.

## Workstream order — business journeys before cosmetic polishing

### 0. Release already built work
- [x] PR #3 inbound review/opt-outs merged to main after source-bound privacy hardening, full feature CI and secure manual suppression.
- [x] Confirm exact `25638554` on eight Railway application services, both main CI jobs green, public health and anonymous 401; **permitted-operator authenticated smoke remains open**.
- [ ] Complete candidate `UI-OPS-06` Alert Centre from existing `operations/alerts` APIs: inventory, severity/status filters, history, reasoned versioned acknowledgement. **No new backend**; assignment/resolution remain explicitly outside this candidate.

### 1. Unblock daily campaign operations — first priority
- **UI-DASH-02 + UI-CAMP-07**: one permission-aware *My Work / Approval Queue* fed by existing governed campaign, commercial and consent APIs. Route users to authoritative approval detail; no synthetic "approved" state, self-checker shortcut or newly invented claim/reassign operation.
- **UI-CAMP-12**: completed-campaign reconciliation workspace using known execution/UNKNOWN/metrics/audit data. No claiming successful closure while pending deliveries or reconciliation remain.
- **UI-CAMP-14**: safely clone an existing campaign into a fresh DRAFT. Never copy permissions, approved consent state, payment approval, immutable materialised snapshot or valid release authority without server revalidation.
- **UI-ORG-03**: consolidate existing organisation GET/events/policies, consent and campaign relations into a usable single detail workspace, including suspension context. Expand `UI-ORG-05` only where explicit server authority already exists.

### 2. Finish lawful contact/consent operations
- **UI-AUD-01/02**: masked contact register and governed detail; confirm existing API provides safe search/paging/lineage before adding components. If API absent, record the exact narrow blocker rather than invent another platform domain.
- **UI-CONS-04/05/06/07**: evidence access, controlled purpose/wording versions, renewals, remediation and rejection. Enhance the *existing* consent maker/checker form and queue, do not rebuild.
- **UI-SEG-04 + UI-AUD-08**: safe masked sample and quality aggregates within bounded permissions; no raw MSISDN export to ordinary UI.
- Complete independent **UI-AUD-09** scoped suppression management; inbound workbench is only a partial substitute.

### 3. Operator self-service and configuration completion
- **UI-AUTH-03**: controlled access recovery consistent with MFA/security, not a new public anonymous reset path.
- **UI-DASH-03**: permission-scoped global search and controlled exact-contact identity lookup; avoid implementing a frontend-only search over arbitrary sensitive fields.
- **UI-ADMIN-03 + UI-ADMIN-06**: govern configuration and retention policies using existing versioned maker/checker APIs.

## How each slice is accepted

A complete Day-1 workflow must show: discover/list → select/detail → reasoned action → server result → immutable evidence/history → recover from denial, expiry, duplicate, conflict and network uncertainty. Permissions must be enforced by the API and reflected in navigation; keyboard/screen-reader and constrained responsive layouts must work. Test the changed slice locally, then require **full Linux CI/race/PostgreSQL/Next gates at PR and main release**, followed by exact-SHA Railway health and approved authenticated UAT.

**Backend freeze rule:** Change a Go/API/database contract only after a test proves the existing endpoint prevents an agreed UI outcome or introduces a security/correctness blocker. Scope such changes narrowly and record them as exceptions.

## Product completion criteria

No new screen is counted as accepted merely because the component renders. The acceptance register must cover all 67 source IDs and eight mandatory prototype journeys, with a final operator walk-through: login/MFA → organisation → high-volume audience → cohort/snapshot → campaign/content → sender/pacing → commercial and consent checker → schedule/release → monitoring/UNKNOWN → inbound STOP/suppression → reconciled reports/audit/privacy.

**Operational gaps remaining regardless of source completion:** a separately authorised production QA fixture, genuine independent maker/checker sessions, delivery-provider opt-out exclusion, large-upload reconnection/throughput proof, runtime alert/worker recovery, and complete accessibility/responsiveness acceptance.
