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
