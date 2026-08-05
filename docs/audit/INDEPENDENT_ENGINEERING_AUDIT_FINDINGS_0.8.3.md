# Independent Engineering Audit — Initial Findings and Remediation

## Scope

This first audit pass reviewed repository recoverability, build coverage, organisation governance, persistence boundaries, HTTP contracts and the generated SRS traceability evidence.

## Findings

### AUD-001 — Build target omitted worker executables

- Severity: Medium
- Status: Closed in 0.8.3
- Evidence: `Makefile`, `cmd/export-worker`, `cmd/inbound-governance-worker`
- Finding: the release build target compiled only four executables and could therefore produce an incomplete binary set even when package tests passed.
- Remediation: the build target now discovers and compiles every first-level `cmd/*` executable deterministically.

### AUD-002 — Organisation lifecycle was create/list only

- Severity: High
- Status: Closed in 0.8.3
- Finding: the domain had statuses and a version field, but there was no persistent update, suspend, review, close or change-history workflow. This prevented proper enforcement and evidence for organisation governance requirements.
- Remediation: optimistic updates, governed status transitions, immutable lifecycle events, PostgreSQL constraints, protected APIs and workflow tests were added.

### AUD-003 — Traceability understated implemented organisation requirements

- Severity: Medium
- Status: Closed in 0.8.3
- Finding: `ORG-001`, `ORG-008` and `ORG-009` remained `NOT_STARTED` despite code evidence after remediation.
- Remediation: the evidence catalogue and generated matrix were reconciled without changing unrelated requirements.

### AUD-004 — Suspended-organisation release enforcement incomplete

- Severity: High
- Status: Closed in 0.8.4
- Finding: organisations can now be governed as suspended, under review or closed, but campaign creation/release and final eligibility do not yet all fail closed on that status.
- Remediation: organisation status is rechecked during campaign creation and progression, consent review, audience import approval and final release. Safety actions remain available after suspension.

### AUD-005 — Organisation restrictions remain unstructured

- Severity: Medium
- Status: Closed in 0.8.6
- Finding: free-text internal notes do not fully satisfy organisation-specific permitted/prohibited purposes, retention and report-branding requirements.
- Remediation: effective-dated maker-checker organisation policy versions now govern allowed/prohibited purposes, retention periods and report-branding metadata; campaign creation enforces the active purpose policy.

### AUD-006 — Consent precedence and live review revalidation were inconsistent

- Severity: High
- Status: Closed in 0.8.8
- Finding: final eligibility treated any historical withdrawal as permanently blocking, even after a later valid re-consent, and campaign progression did not consistently revalidate consent-review expiry and channel scope.
- Remediation: latest-effective-grant precedence is now deterministic; suppression remains dominant; campaign approval, release and final dispatch revalidate live organisation and consent-review evidence with precise exclusion reasons.

### AUD-007 — Material campaign changes did not invalidate approvals

- Severity: High
- Status: Closed in 0.8.9
- Finding: approved campaigns had no governed amendment workflow for replacement audience, message, entitlement, transport or schedule evidence. Repository updates also did not persist all material schedule and entitlement fields, so a change could not be evidenced or forced through reapproval consistently.
- Remediation: explicit MFA-protected material amendments now validate authoritative immutable evidence, use optimistic concurrency, regress the campaign to the correct approval stage, clear downstream approvals, persist all changed fields transactionally and record append-only change events.

### AUD-008 — Required frequency caps were not configurable or enforced

**Severity:** High
**Status:** Closed in 0.8.10

The SRS requires organisation/purpose/channel frequency caps in both cohort and final eligibility. The prior implementation had no governed cap definition and did not exclude recently contacted recipients. Version 0.8.10 extends effective-dated organisation policy with bounded rolling-window caps and enforces them during cohort compilation, recipient release and final dispatch with the explicit `FREQUENCY_CAPPED` reason.

### AUD-009 — Sender quarantine and delivery-exception resolution lacked governed operational controls

**Severity:** High
**Status:** Closed in 0.8.15

The scheduler excluded only basic unhealthy states, while runtime heartbeats could restore a manually isolated session. Unknown and contradictory delivery outcomes were surfaced but lacked a complete evidence-backed resolution workflow. Version 0.8.15 adds governed quarantine/reinstatement, heartbeat protection, zero-capacity exclusion, health assessments, MFA-protected delivery-exception resolution and append-only resolution evidence.
