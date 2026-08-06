# Current status

## Current repository baseline

Version `0.8.26` on branch `work/backend-data-privacy-exports` is the current backend checkpoint.

The platform remains an internal managed-service campaign control plane. Go and PostgreSQL own consent, audience eligibility, campaign approval, recipient obligations, canonical delivery state, privacy decisions, reporting and audit. OpenWA remains an isolated replaceable transport.

## Latest hardening

- Export requests freeze criteria, template version, report as-of time and deterministic source evidence before rendering.
- Export retrieval uses short-lived, actor-bound, single-use grants with revocation, expiry, integrity verification and complete success/failure/interruption audit.
- Audit search is cursor-based and filterable by actor, action, object, organisation, request/correlation identifiers, source IP, time, outcome and sensitivity.
- CSV and XLSX cells neutralise dangerous spreadsheet formula prefixes; PDF output is multi-page rather than silently truncated.
- XLSX imports are parsed through a bounded OOXML path and reject ambiguous/hidden sheets, formulas, macros, external relationships and suspicious archive expansion.
- Reusable import mappings are governed and effective-dated; templates are deterministic; validation issues are exportable with masked identifiers.
- Import rollback restores captured pre-import state and fails closed after downstream consumption or later independent changes.
- Import source deletion is lease-fenced, retryable and records append-only success/failure evidence.
- Privacy cases support access, portability, correction, objection, restriction and erasure/anonymisation with deadlines, assignment, maker-checker decisions, a separate executor and legal holds.
- Access/portability packages are encrypted under a dedicated privacy key and ordinary APIs never expose the package payload.
- Campaign and organisation reports suppress small demographic/geographic cells server-side and record the exact reporting-privacy policy ID/version.
- Contact lifecycle operations are MFA-protected, optimistically locked and append-only; anonymised/deleted contacts cannot be reactivated outside privacy governance.
- Object storage can use the local filesystem or an S3/MinIO-compatible backend with signed requests, checksum-backed idempotency and verified reads.
- Migrations `0062`–`0064` add the corresponding database authority and evidence controls.

## Traceability position

The generated catalogue currently reports:

```text
Total requirements:       398
Implemented and tested:    96
Partial:                    38
Not started:               264
```

The matrix is granular and contains frontend, repository-governance, deployment and external-validation requirements. These counts are evidence classifications, not a direct backend completion percentage.

## Honest readiness position

The next code-side package must complete common platform configuration governance, gateway/node administration and signed runtime discovery, maintenance/emergency controls, cross-domain retention/legal-hold automation, alert escalation and incident timelines. Production engineering hardening and live infrastructure/frontend gates remain separately open.
